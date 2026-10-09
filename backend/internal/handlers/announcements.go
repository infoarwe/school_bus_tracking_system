package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

var announcementCategories = []string{"school_announcement", "route_announcement", "bus_breakdown", "traffic_delay",
	"pickup_change", "emergency_message", "holiday", "other"}

// CreateAnnouncement: send now, or at scheduled_at. Entire school: Super Admin and
// School Admin only ("School Notification"); one route: also Transport Manager
// ("Route Notification").
func (a *API) CreateAnnouncement(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req struct {
		Category      string     `json:"category"`
		Target        string     `json:"target"`
		RouteID       *string    `json:"route_id"`
		Title         string     `json:"title"`
		Message       string     `json:"message"`
		AttachmentURL string     `json:"attachment_url"`
		ScheduledAt   *time.Time `json:"scheduled_at"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Title, req.Message, req.AttachmentURL = strings.TrimSpace(req.Title), strings.TrimSpace(req.Message), strings.TrimSpace(req.AttachmentURL)
	ctx := r.Context()
	p := auth.FromContext(ctx)

	v := validate.New()
	v.OneOf("category", req.Category, announcementCategories...)
	v.OneOf("target", req.Target, "school", "route")
	v.Required("title", req.Title)
	v.MaxLen("title", req.Title, 100)
	v.Required("message", req.Message)
	v.MaxLen("message", req.Message, 1000)
	if req.AttachmentURL != "" {
		u, err := url.Parse(req.AttachmentURL)
		v.Check(err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "", "attachment_url", "must be a web link (https://…)")
	}
	if req.ScheduledAt != nil {
		v.Check(req.ScheduledAt.After(time.Now().Add(-time.Minute)), "scheduled_at", "must be in the future")
		v.Check(req.ScheduledAt.Before(time.Now().AddDate(0, 0, 60)), "scheduled_at", "can be at most 60 days ahead")
	}
	if req.Target == "route" {
		ok := req.RouteID != nil
		if ok {
			_, err := uuid.Parse(*req.RouteID)
			ok = err == nil
		}
		if ok {
			_, err := store.GetRoute(ctx, a.Store.Pool, schoolID, *req.RouteID)
			ok = err == nil
		}
		v.Check(ok, "route_id", "choose a route of this school")
	} else {
		req.RouteID = nil
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	if req.Target == "school" && p.Role == models.RoleTransportManager {
		httpx.Error(w, http.StatusForbidden, "forbidden", "Transport Managers can send route announcements only.")
		return
	}

	var id string
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		var err error
		id, err = store.InsertAnnouncement(ctx, q, schoolID, store.AnnouncementInput{Category: req.Category, Target: req.Target,
			RouteID: req.RouteID, Title: req.Title, Message: req.Message, AttachmentURL: req.AttachmentURL,
			ScheduledAt: req.ScheduledAt, CreatedBy: p.UserID})
		if err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{SchoolID: &schoolID, Action: "announcement.create",
			EntityType: "announcement", EntityID: &id, After: req})
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	// "Send now" goes out immediately; scheduled ones are sent by the scheduler job.
	if req.ScheduledAt == nil || !req.ScheduledAt.After(time.Now()) {
		if err := a.SendDueAnnouncements(ctx); err != nil {
			internalError(w, r, err)
			return
		}
	}
	an, err := a.announcementWithStats(ctx, schoolID, id)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, an)
}

// SendDueAnnouncements sends every announcement whose time has come (also run by
// a background job every 30 s).
func (a *API) SendDueAnnouncements(ctx context.Context) error {
	return a.Store.InTx(ctx, func(q store.DBTX) error {
		due, err := store.ClaimDueAnnouncements(ctx, q, 20)
		if err != nil {
			return err
		}
		for _, an := range due {
			var riders []store.Rider
			if an.Target == "route" {
				riders, err = store.RouteRiders(ctx, q, an.SchoolID, *an.RouteID)
			} else {
				riders, err = store.SchoolParents(ctx, q, an.SchoolID)
			}
			if err != nil {
				return err
			}
			data := map[string]string{"announcement_id": an.ID, "category": an.Category}
			if an.AttachmentURL != "" {
				data["attachment_url"] = an.AttachmentURL
			}
			if an.RouteID != nil {
				data["route_id"] = *an.RouteID
			}
			key := "announcement:" + an.ID
			nid, created, err := notify.Notify(ctx, q, store.NotificationInput{SchoolID: an.SchoolID, Type: notify.TypeAnnouncement,
				Title: an.Title, Body: an.Message, Data: data, RouteID: an.RouteID, DedupeKey: &key}, riders)
			if err != nil {
				return err
			}
			var nidPtr *string
			if created {
				nidPtr = &nid
			}
			if err := store.MarkAnnouncementSent(ctx, q, an.ID, nidPtr); err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *API) announcementWithStats(ctx context.Context, schoolID, id string) (*store.Announcement, error) {
	an, err := store.GetAnnouncement(ctx, a.Store.Pool, schoolID, id)
	if err != nil {
		return nil, err
	}
	if an.NotificationID != nil {
		if an.Stats, err = store.NotificationStats(ctx, a.Store.Pool, *an.NotificationID); err != nil {
			return nil, err
		}
	}
	return an, nil
}

// ListAnnouncements: history and scheduled ones, with delivery stats.
func (a *API) ListAnnouncements(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	pg := page(r)
	list, total, err := store.ListAnnouncements(r.Context(), a.Store.Pool, schoolID, r.URL.Query().Get("status"), pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	for i := range list {
		if list[i].NotificationID != nil {
			if list[i].Stats, err = store.NotificationStats(r.Context(), a.Store.Pool, *list[i].NotificationID); err != nil {
				internalError(w, r, err)
				return
			}
		}
	}
	httpx.List(w, list, meta(pg, total))
}

// CancelAnnouncement cancels a scheduled announcement that has not gone out.
func (a *API) CancelAnnouncement(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "announcementID")
	if !ok {
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		an, err := store.GetAnnouncement(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if an.Target == "school" && p.Role == models.RoleTransportManager {
			return errForbidden
		}
		if err := store.CancelAnnouncement(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{SchoolID: &schoolID, Action: "announcement.cancel", EntityType: "announcement", EntityID: &id})
	})
	switch {
	case errors.Is(err, store.ErrStatusChanged):
		httpx.Error(w, http.StatusConflict, "invalid_transition", "Only scheduled announcements that have not been sent can be cancelled.")
	case errors.Is(err, errForbidden):
		httpx.Error(w, http.StatusForbidden, "forbidden", "Transport Managers can manage route announcements only.")
	case err != nil:
		storeError(w, r, err, nil)
	default:
		an, err := a.announcementWithStats(ctx, schoolID, id)
		if err != nil {
			internalError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, an)
	}
}

var errForbidden = errors.New("forbidden")
