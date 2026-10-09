package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// --- Automatic notifications (S7-03) ---

func tripData(t *models.Trip) map[string]string {
	return map[string]string{"trip_id": t.ID, "route_id": t.Route.ID, "trip_type": t.TripType}
}

// notifyTrip sends one automatic notification to the parents of the trip's riders
// (optionally only those at stopID). The dedupe key makes it fire at most once.
func (a *API) notifyTrip(ctx context.Context, t *models.Trip, typ string, msg notify.Message, stopID, dedupe string, extra map[string]string) error {
	riders, err := store.TripRiders(ctx, a.Store.Pool, t.ID, stopID)
	if err != nil {
		return err
	}
	data := tripData(t)
	for k, v := range extra {
		data[k] = v
	}
	_, _, err = notify.Notify(ctx, a.Store.Pool, store.NotificationInput{
		SchoolID: t.SchoolID, Type: typ, Title: msg.Title, Body: msg.Body, Data: data,
		TripID: &t.ID, RouteID: &t.Route.ID, DedupeKey: &dedupe,
	}, riders)
	return err
}

// notifyTripStatus: Bus Started, Trip Completed, Trip Cancelled.
func (a *API) notifyTripStatus(ctx context.Context, t *models.Trip) error {
	switch t.Status {
	case models.TripStarted:
		return a.notifyTrip(ctx, t, notify.TypeBusStarted, notify.BusStarted(t.TripType, t.Bus.VehicleNumber), "", "trip:"+t.ID+":started", nil)
	case models.TripCompleted:
		return a.notifyTrip(ctx, t, notify.TypeTripCompleted, notify.TripCompleted(t.TripType), "", "trip:"+t.ID+":completed", nil)
	case models.TripCancelled:
		msg := notify.Message{Title: "Trip cancelled", Body: "Today's bus trip has been cancelled."}
		if t.TripType == models.TripEveningDrop {
			msg.Body = "Today's Evening Drop trip has been cancelled."
		} else {
			msg.Body = "Today's Morning Pickup trip has been cancelled."
		}
		if t.CancelReason != "" {
			msg.Body += " Reason: " + t.CancelReason
		}
		return a.notifyTrip(ctx, t, "trip_cancelled", msg, "", "trip:"+t.ID+":cancelled", nil)
	}
	return nil
}

// notifyStopEvents: Approaching (with ETA), Reached, Crossed for the parents at
// that stop; School Reached for everyone on the trip.
func (a *API) notifyStopEvents(ctx context.Context, t *models.Trip, p *tracking.TripProgress, events []tracking.StopEvent) error {
	for _, e := range events {
		key := "trip:" + t.ID + ":stop:" + e.StopID + ":" + e.Type
		extra := map[string]string{"stop_id": e.StopID}
		var err error
		switch e.Type {
		case tracking.EventApproaching:
			var eta *int
			if s := p.Stop(e.StopID); s != nil {
				eta = s.ETASeconds
			}
			err = a.notifyTrip(ctx, t, notify.TypeApproaching, notify.Approaching(eta), e.StopID, key, extra)
		case tracking.EventReached:
			err = a.notifyTrip(ctx, t, notify.TypeReached, notify.Reached(), e.StopID, key, extra)
		case tracking.EventCrossed:
			err = a.notifyTrip(ctx, t, notify.TypeCrossed, notify.Crossed(e.Missed), e.StopID, key, extra)
		case tracking.EventSchoolReached:
			err = a.notifyTrip(ctx, t, notify.TypeSchoolReached, notify.SchoolReached(), "", "trip:"+t.ID+":school_reached", nil)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// --- Devices (S7-01) ---

// RegisterDevice: the Driver and Parent apps register their FCM token after
// login (and whenever FCM gives them a new one). It is tied to this login
// session, so logging out stops pushes to the device.
func (a *API) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	v := validate.New()
	v.Check(len(req.Token) >= 20 && len(req.Token) <= 4096, "token", "must be the FCM registration token")
	v.OneOf("platform", req.Platform, "android", "ios")
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	p := auth.FromContext(r.Context())
	if err := store.UpsertDeviceToken(r.Context(), a.Store.Pool, p.UserID, p.SessionID, req.Token, req.Platform); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// UnregisterDevice: e.g. when the user turns notifications off.
func (a *API) UnregisterDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	p := auth.FromContext(r.Context())
	if err := store.DeleteUserDeviceToken(r.Context(), a.Store.Pool, p.UserID, strings.TrimSpace(req.Token)); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// --- Parent inbox ---

// parentStudentFilter validates ?student_id= belongs to the parent ("" = all children).
func (a *API) parentStudentFilter(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.URL.Query().Get("student_id")
	if id == "" {
		return "", true
	}
	if _, err := uuid.Parse(id); err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return "", false
	}
	children, err := store.ParentChildren(r.Context(), a.Store.Pool, auth.FromContext(r.Context()).UserID)
	if err != nil {
		internalError(w, r, err)
		return "", false
	}
	for _, c := range children {
		if c.StudentID == id {
			return id, true
		}
	}
	httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
	return "", false
}

// ParentNotifications lists the inbox, newest first; ?student_id= shows one child's
// notifications plus school-wide ones. meta.unread is the unread count for the same filter.
func (a *API) ParentNotifications(w http.ResponseWriter, r *http.Request) {
	studentID, ok := a.parentStudentFilter(w, r)
	if !ok {
		return
	}
	p := auth.FromContext(r.Context())
	pg := page(r)
	items, total, err := store.Inbox(r.Context(), a.Store.Pool, p.UserID, studentID, pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	unread, err := store.UnreadCount(r.Context(), a.Store.Pool, p.UserID, studentID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSONWithMeta(w, items, map[string]any{"page": pg.Page, "page_size": pg.PageSize, "total": total, "unread": unread})
}

// MarkNotificationsRead marks inbox items read: {"ids": [...]} (ids from the inbox list).
func (a *API) MarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	for _, id := range req.IDs {
		if _, err := uuid.Parse(id); err != nil {
			httpx.ValidationError(w, validate.Errors{"ids": "must be inbox item IDs"})
			return
		}
	}
	if len(req.IDs) == 0 || len(req.IDs) > 200 {
		httpx.ValidationError(w, validate.Errors{"ids": "give 1 to 200 IDs"})
		return
	}
	if err := store.MarkRead(r.Context(), a.Store.Pool, auth.FromContext(r.Context()).UserID, req.IDs); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.NoContent(w)
}
