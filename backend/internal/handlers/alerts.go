package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

var delayReasons = []string{"traffic", "bus_breakdown", "road_block", "weather", "driver_issue", "other"}

type delayRequest struct {
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason"`
	Note    string `json:"note"`
}

// reportDelay records a delay on a trip and notifies the parents of the trip's
// riders ("affected-route parents"). A breakdown uses the breakdown message.
func (a *API) reportDelay(w http.ResponseWriter, r *http.Request, t *models.Trip) {
	var req delayRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Note = strings.TrimSpace(req.Note)
	v := validate.New()
	v.Check(req.Minutes >= 1 && req.Minutes <= 240, "minutes", "must be 1 to 240")
	v.OneOf("reason", req.Reason, delayReasons...)
	v.MaxLen("note", req.Note, 500)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	if t.Status == models.TripCompleted || t.Status == models.TripCancelled {
		httpx.Error(w, http.StatusConflict, "invalid_transition", "This trip is "+t.Status+"; a delay cannot be reported.")
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	var alert *store.Alert
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		id, err := store.InsertDelay(ctx, q, t.SchoolID, store.DelayInput{TripID: t.ID, Minutes: req.Minutes, Reason: req.Reason,
			Note: req.Note, ReportedBy: p.UserID, ReporterRole: string(p.Role)})
		if err != nil {
			return err
		}
		typ, msg := notify.TypeDelayed, notify.Delayed(req.Minutes, t.TripType)
		if req.Reason == "bus_breakdown" {
			typ, msg = notify.TypeBreakdown, notify.Breakdown()
		}
		riders, err := store.TripRiders(ctx, q, t.ID, "")
		if err != nil {
			return err
		}
		key := "delay:" + id
		data := tripData(t)
		data["delay_id"] = id
		nid, created, err := notify.Notify(ctx, q, store.NotificationInput{SchoolID: t.SchoolID, Type: typ, Title: msg.Title,
			Body: msg.Body, Data: data, TripID: &t.ID, RouteID: &t.Route.ID, DedupeKey: &key, CreatedBy: &p.UserID}, riders)
		if err != nil {
			return err
		}
		if created {
			if err := store.SetDelayNotification(ctx, q, id, nid); err != nil {
				return err
			}
		}
		if alert, err = store.GetDelayAlert(ctx, q, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{SchoolID: &t.SchoolID, Action: "trip.delay_report",
			EntityType: "trip", EntityID: &t.ID, After: alert})
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.publishAlert(ctx, t.SchoolID, alert)
	httpx.JSON(w, http.StatusCreated, alert)
}

// DriverReportDelay: the driver reports a delay or breakdown on their own trip.
func (a *API) DriverReportDelay(w http.ResponseWriter, r *http.Request) {
	if t, ok := a.loadDriverTrip(w, r); ok {
		a.reportDelay(w, r, t)
	}
}

// StaffReportDelay: staff report a delay on any trip of their school.
func (a *API) StaffReportDelay(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	t, err := store.GetTrip(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	a.reportDelay(w, r, t)
}

// DriverReportEmergency alerts the school's staff immediately (live, on the
// alerts panel). Parents are not told automatically: staff decide what to send.
func (a *API) DriverReportEmergency(w http.ResponseWriter, r *http.Request) {
	t, ok := a.loadDriverTrip(w, r)
	if !ok {
		return
	}
	var req struct {
		Message   string   `json:"message"`
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		req.Message = "Emergency reported by the driver."
	}
	v := validate.New()
	v.MaxLen("message", req.Message, 1000)
	v.Check((req.Latitude == nil) == (req.Longitude == nil), "latitude", "give both latitude and longitude, or neither")
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	var alert *store.Alert
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		id, err := store.InsertEmergency(ctx, q, t.SchoolID, store.EmergencyInput{TripID: t.ID, Message: req.Message,
			Latitude: req.Latitude, Longitude: req.Longitude, ReportedBy: p.UserID})
		if err != nil {
			return err
		}
		if alert, err = store.GetEmergencyAlert(ctx, q, t.SchoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{SchoolID: &t.SchoolID, Action: "trip.emergency_report",
			EntityType: "emergency", EntityID: &id, After: alert})
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.publishAlert(ctx, t.SchoolID, alert)
	httpx.JSON(w, http.StatusCreated, alert)
}

func (a *API) loadDriverTrip(w http.ResponseWriter, r *http.Request) (*models.Trip, bool) {
	d, ok := a.currentDriver(w, r)
	if !ok {
		return nil, false
	}
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return nil, false
	}
	t, err := store.GetDriverTrip(r.Context(), a.Store.Pool, d.ID, id)
	if err != nil {
		storeError(w, r, err, nil)
		return nil, false
	}
	return t, true
}

// SchoolAlerts: unresolved emergencies and today's delays (staff alerts panel).
func (a *API) SchoolAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := store.SchoolAlerts(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"))
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, alerts)
}

// UpdateEmergency: POST .../emergencies/{id}/acknowledge or /resolve.
func (a *API) UpdateEmergency(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		schoolID := chi.URLParam(r, "schoolID")
		id, ok := idParam(w, r, "emergencyID")
		if !ok {
			return
		}
		ctx := r.Context()
		p := auth.FromContext(ctx)
		var alert *store.Alert
		err := a.Store.InTx(ctx, func(q store.DBTX) error {
			if _, err := store.GetEmergencyAlert(ctx, q, schoolID, id); err != nil {
				return err
			}
			if err := store.SetEmergencyStatus(ctx, q, schoolID, id, status, p.UserID); err != nil {
				return err
			}
			var err error
			if alert, err = store.GetEmergencyAlert(ctx, q, schoolID, id); err != nil {
				return err
			}
			return a.audit(ctx, q, r, store.AuditEntry{SchoolID: &schoolID, Action: "emergency." + status,
				EntityType: "emergency", EntityID: &id, After: map[string]string{"status": status}})
		})
		if errors.Is(err, store.ErrStatusChanged) {
			httpx.Error(w, http.StatusConflict, "invalid_transition", "This emergency is already "+status+" or resolved.")
			return
		}
		if err != nil {
			storeError(w, r, err, nil)
			return
		}
		a.publishAlert(ctx, schoolID, alert)
		httpx.JSON(w, http.StatusOK, alert)
	}
}

// publishAlert pushes an alert to the school's staff only (never to parents).
func (a *API) publishAlert(ctx context.Context, schoolID string, alert *store.Alert) {
	if err := a.Live.PublishAlert(ctx, schoolID, alert.TripID, alert); err != nil {
		internalErrorLog(nil, err)
	}
}
