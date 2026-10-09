package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// Trips can be planned up to this many days ahead.
const maxDaysAhead = 90

var tripTypes = []string{models.TripMorningPickup, models.TripEveningDrop}

// Unique slot indexes → the request field that clashes.
var tripSlotConflicts = map[string]struct{ field, msg string }{
	"trips_route_slot_key":  {"route_id", "This route already has a trip on this date for this trip type."},
	"trips_bus_slot_key":    {"bus_id", "This bus is already assigned to another trip on this date for this trip type."},
	"trips_driver_slot_key": {"driver_id", "This driver is already assigned to another trip on this date for this trip type."},
}

// tripError carries a ready-made API error out of a transaction.
type tripError struct {
	status int
	body   httpx.ErrorBody
}

func (e *tripError) Error() string { return e.body.Message }

func invalidTransition(t *models.Trip, action string) error {
	return &tripError{http.StatusConflict, httpx.ErrorBody{
		Code:    "invalid_transition",
		Message: fmt.Sprintf("This trip is %s and cannot be %s.", t.Status, action),
	}}
}

func tripValidation(fields validate.Errors) error {
	return &tripError{http.StatusBadRequest, httpx.ErrorBody{
		Code: "validation_failed", Message: "One or more fields are invalid.", Fields: fields,
	}}
}

// writeTripError maps trip errors, slot conflicts and store errors to responses.
func writeTripError(w http.ResponseWriter, r *http.Request, err error) {
	var te *tripError
	var ce *store.ConflictError
	switch {
	case errors.As(err, &te):
		httpx.ErrorWithFields(w, te.status, te.body)
	case errors.As(err, &ce) && ce.Constraint == "trips_driver_started_key":
		httpx.Error(w, http.StatusConflict, "another_trip_in_progress", "The driver already has a trip in progress. End it first.")
	case errors.As(err, &ce) && tripSlotConflicts[ce.Constraint].field != "":
		c := tripSlotConflicts[ce.Constraint]
		httpx.ErrorWithFields(w, http.StatusConflict, httpx.ErrorBody{
			Code: "trip_conflict", Message: c.msg, Fields: map[string]string{c.field: c.msg},
		})
	case errors.Is(err, store.ErrStatusChanged):
		httpx.Error(w, http.StatusConflict, "status_changed", err.Error())
	default:
		storeError(w, r, err, nil)
	}
}

type tripRequest struct {
	TripDate string `json:"trip_date"`
	TripType string `json:"trip_type"`
	RouteID  string `json:"route_id"`
	BusID    string `json:"bus_id"`
	DriverID string `json:"driver_id"`
	Notes    string `json:"notes"`
}

// validateTripDate checks YYYY-MM-DD between today (school time zone) and maxDaysAhead.
func validateTripDate(v *validate.V, field, date, today string) {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		v.Check(false, field, "must be a date YYYY-MM-DD")
		return
	}
	t, _ := time.Parse(time.DateOnly, today)
	v.Check(!d.Before(t), field, "cannot be in the past")
	v.Check(!d.After(t.AddDate(0, 0, maxDaysAhead)), field, fmt.Sprintf("can be at most %d days ahead", maxDaysAhead))
}

// validateTrip checks the date, trip type and that route, bus and driver belong to
// the school, are active and fit the trip type.
func validateTrip(ctx context.Context, q store.DBTX, schoolID string, req *tripRequest) error {
	today, err := store.SchoolToday(ctx, q, schoolID)
	if err != nil {
		return err
	}
	req.Notes = strings.TrimSpace(req.Notes)
	v := validate.New()
	validateTripDate(v, "trip_date", req.TripDate, today)
	v.OneOf("trip_type", req.TripType, tripTypes...)
	for field, id := range map[string]string{"route_id": req.RouteID, "bus_id": req.BusID, "driver_id": req.DriverID} {
		_, err := uuid.Parse(id)
		v.Check(err == nil, field, "is required")
	}
	v.MaxLen("notes", req.Notes, 500)
	if !v.OK() {
		return tripValidation(v.Errors())
	}

	refs, err := store.GetTripRefs(ctx, q, schoolID, req.RouteID, req.BusID, req.DriverID)
	if err != nil {
		return err
	}
	v.Check(refs.RouteFound, "route_id", "route not found")
	v.Check(!refs.RouteFound || refs.RouteStatus == models.StatusActive, "route_id", "route is inactive")
	v.Check(!refs.RouteFound || refs.StopCount > 0, "route_id", "route has no stops yet")
	if refs.RouteFound && req.TripType == models.TripMorningPickup {
		v.Check(refs.SupportsPickup, "route_id", "route does not run Morning Pickup")
	}
	if refs.RouteFound && req.TripType == models.TripEveningDrop {
		v.Check(refs.SupportsDrop, "route_id", "route does not run Evening Drop")
	}
	v.Check(refs.BusFound, "bus_id", "bus not found")
	v.Check(!refs.BusFound || refs.BusStatus == models.StatusActive, "bus_id", "bus is not active ("+refs.BusStatus+")")
	v.Check(refs.DriverFound, "driver_id", "driver not found")
	v.Check(!refs.DriverFound || refs.DriverStatus == models.StatusActive, "driver_id", "driver is not active")
	if !v.OK() {
		return tripValidation(v.Errors())
	}
	return nil
}

func (req *tripRequest) input() store.TripInput {
	return store.TripInput{TripDate: req.TripDate, TripType: req.TripType, RouteID: req.RouteID,
		BusID: req.BusID, DriverID: req.DriverID, Notes: req.Notes}
}

// changeTripStatus moves a trip to a new status inside a transaction, writing
// trip history and the audit log.
func (a *API) changeTripStatus(ctx context.Context, q store.DBTX, r *http.Request, t *models.Trip, to, reason, action string) error {
	if err := store.SetTripStatus(ctx, q, t.ID, t.Status, to, reason); err != nil {
		return err
	}
	p := auth.FromContext(ctx)
	from := t.Status
	if err := store.InsertTripHistory(ctx, q, store.TripHistoryEntry{
		TripID: t.ID, SchoolID: t.SchoolID, From: &from, To: to, ByUserID: &p.UserID, ByRole: string(p.Role), Reason: reason,
	}); err != nil {
		return err
	}
	return a.audit(ctx, q, r, store.AuditEntry{
		SchoolID: &t.SchoolID, Action: action, EntityType: "trip", EntityID: &t.ID,
		Before: map[string]string{"status": from}, After: map[string]string{"status": to, "reason": reason},
	})
}

// publishTripStatus tells live subscribers (after the change is committed). A failure
// only delays the live view, so it is logged, not returned.
func (a *API) publishTripStatus(r *http.Request, t *models.Trip) {
	if err := a.Live.TripStatusChanged(r.Context(), t.SchoolID, t.ID, t.Status); err != nil {
		internalErrorLog(r, err)
	}
}

// --- Admin web: /schools/{schoolID}/trips ---

func (a *API) ListTrips(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	pg := page(r)
	trips, total, err := store.ListTrips(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), store.TripFilter{
		Date: qs.Get("date"), From: qs.Get("from"), To: qs.Get("to"), TripType: qs.Get("trip_type"),
		Status: qs.Get("status"), RouteID: qs.Get("route_id"), DriverID: qs.Get("driver_id"), BusID: qs.Get("bus_id"),
	}, pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.List(w, trips, meta(pg, total))
}

type tripDetail struct {
	*models.Trip
	History []models.TripStatusChange `json:"history"`
	Stops   []tripStop                `json:"stops"`
}

// tripStop is a route stop plus how many students board (or get off) there on this trip.
type tripStop struct {
	models.Stop
	StudentCount int `json:"student_count"`
}

func (a *API) tripStops(ctx context.Context, t *models.Trip) ([]tripStop, error) {
	stops, err := store.ListStops(ctx, a.Store.Pool, t.Route.ID)
	if err != nil {
		return nil, err
	}
	counts, err := store.StopStudentCounts(ctx, a.Store.Pool, t.Route.ID, t.TripType)
	if err != nil {
		return nil, err
	}
	out := make([]tripStop, len(stops))
	for i, s := range stops {
		out[i] = tripStop{Stop: s, StudentCount: counts[s.ID]}
	}
	return out, nil
}

func (a *API) GetTrip(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	ctx := r.Context()
	t, err := store.GetTrip(ctx, a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	d := tripDetail{Trip: t}
	if d.History, err = store.TripHistory(ctx, a.Store.Pool, id); err != nil {
		internalError(w, r, err)
		return
	}
	if d.Stops, err = a.tripStops(ctx, t); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (a *API) CreateTrip(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req tripRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	var t *models.Trip
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		if err := validateTrip(ctx, q, schoolID, &req); err != nil {
			return err
		}
		id, err := store.CreateTrip(ctx, q, schoolID, p.UserID, req.input())
		if err != nil {
			return err
		}
		if err := store.InsertTripHistory(ctx, q, store.TripHistoryEntry{
			TripID: id, SchoolID: schoolID, To: models.TripScheduled, ByUserID: &p.UserID, ByRole: string(p.Role),
		}); err != nil {
			return err
		}
		if t, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "trip.create", EntityType: "trip", EntityID: &id, After: t,
		})
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, t)
}

// UpdateTrip changes date, type, route, bus or driver of a trip that has not started.
// A confirmed trip goes back to scheduled when its driver changes, so the new driver confirms.
func (a *API) UpdateTrip(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	var req tripRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	var after *models.Trip
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetTrip(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if before.Status != models.TripScheduled && before.Status != models.TripConfirmed {
			return invalidTransition(before, "changed")
		}
		if err := validateTrip(ctx, q, schoolID, &req); err != nil {
			return err
		}
		if err := store.UpdateTripAssignment(ctx, q, schoolID, id, req.input()); err != nil {
			return err
		}
		if before.Status == models.TripConfirmed && before.Driver.ID != req.DriverID {
			if err := a.changeTripStatus(ctx, q, r, before, models.TripScheduled, "driver changed", "trip.reset"); err != nil {
				return err
			}
		}
		if after, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "trip.update", EntityType: "trip", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

func decodeReason(w http.ResponseWriter, r *http.Request, dst any, reason *string) bool {
	if !httpx.Decode(w, r, dst) {
		return false
	}
	*reason = strings.TrimSpace(*reason)
	if *reason == "" || len(*reason) > 500 {
		httpx.ValidationError(w, validate.Errors{"reason": "is required (max 500 characters)"})
		return false
	}
	return true
}

// CancelTrip cancels a trip that has not started. The slot becomes free again.
func (a *API) CancelTrip(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decodeReason(w, r, &req, &req.Reason) {
		return
	}
	a.adminTransition(w, r, schoolID, id, func(t *models.Trip) (string, error) {
		if t.Status != models.TripScheduled && t.Status != models.TripConfirmed {
			return "", invalidTransition(t, "cancelled")
		}
		return models.TripCancelled, nil
	}, req.Reason, "trip.cancel")
}

// OverrideTrip lets an admin force a trip's status (e.g. the driver's phone died):
// start a scheduled/confirmed trip today, complete or cancel any open trip.
func (a *API) OverrideTrip(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !decodeReason(w, r, &req, &req.Reason) {
		return
	}
	v := validate.New()
	v.OneOf("status", req.Status, models.TripStarted, models.TripCompleted, models.TripCancelled)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	a.adminTransition(w, r, schoolID, id, func(t *models.Trip) (string, error) {
		open := t.Status == models.TripScheduled || t.Status == models.TripConfirmed
		switch req.Status {
		case models.TripStarted:
			if !open {
				return "", invalidTransition(t, "started")
			}
			if err := a.requireToday(r.Context(), t); err != nil {
				return "", err
			}
		case models.TripCompleted, models.TripCancelled:
			if !open && t.Status != models.TripStarted {
				return "", invalidTransition(t, req.Status)
			}
		}
		return req.Status, nil
	}, req.Reason, "trip.override")
}

func (a *API) adminTransition(w http.ResponseWriter, r *http.Request, schoolID, id string,
	decide func(*models.Trip) (string, error), reason, action string) {
	ctx := r.Context()
	var after *models.Trip
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		t, err := store.GetTrip(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		to, err := decide(t)
		if err != nil {
			return err
		}
		if err := a.changeTripStatus(ctx, q, r, t, to, reason, action); err != nil {
			return err
		}
		after, err = store.GetTrip(ctx, q, schoolID, id)
		return err
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	a.publishTripStatus(r, after)
	httpx.JSON(w, http.StatusOK, after)
}

// requireToday: a trip can only start on its own date (school time zone).
func (a *API) requireToday(ctx context.Context, t *models.Trip) error {
	today, err := store.SchoolToday(ctx, a.Store.Pool, t.SchoolID)
	if err != nil {
		return err
	}
	if t.TripDate != today {
		return &tripError{http.StatusConflict, httpx.ErrorBody{
			Code: "not_trip_day", Message: "This trip is for " + t.TripDate + "; it can only start on that day.",
		}}
	}
	return nil
}

type copyResult struct {
	Created int           `json:"created"`
	Skipped []copySkipped `json:"skipped"`
}

type copySkipped struct {
	TripDate  string `json:"trip_date"`
	TripType  string `json:"trip_type"`
	RouteCode string `json:"route_code"`
	Reason    string `json:"reason"`
}

// CopyTrips copies one day's trips to other dates. Each copy is validated like a new
// trip; ones that clash or whose route, bus or driver is no longer usable are skipped.
func (a *API) CopyTrips(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req struct {
		FromDate  string   `json:"from_date"`
		ToDates   []string `json:"to_dates"`
		TripTypes []string `json:"trip_types"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if len(req.TripTypes) == 0 {
		req.TripTypes = tripTypes
	}
	v := validate.New()
	_, err := time.Parse(time.DateOnly, req.FromDate)
	v.Check(err == nil, "from_date", "must be a date YYYY-MM-DD")
	v.Check(len(req.ToDates) >= 1 && len(req.ToDates) <= 31, "to_dates", "give 1 to 31 dates")
	for _, d := range req.ToDates {
		v.Check(d != req.FromDate, "to_dates", "cannot include the source date")
	}
	for _, tt := range req.TripTypes {
		v.OneOf("trip_types", tt, tripTypes...)
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}

	ctx := r.Context()
	p := auth.FromContext(ctx)
	res := copyResult{Skipped: []copySkipped{}}
	err = a.Store.InTx(ctx, func(q store.DBTX) error {
		source, err := store.TripsOnDate(ctx, q, schoolID, req.FromDate, req.TripTypes)
		if err != nil {
			return err
		}
		for _, d := range req.ToDates {
			for _, t := range source {
				tr := tripRequest{TripDate: d, TripType: t.TripType, RouteID: t.Route.ID, BusID: t.Bus.ID, DriverID: t.Driver.ID, Notes: t.Notes}
				skip := func(reason string) {
					res.Skipped = append(res.Skipped, copySkipped{TripDate: d, TripType: t.TripType, RouteCode: t.Route.Code, Reason: reason})
				}
				if err := validateTrip(ctx, q, schoolID, &tr); err != nil {
					var te *tripError
					if !errors.As(err, &te) {
						return err
					}
					skip(firstFieldError(te.body.Fields))
					continue
				}
				// Skip clashes without aborting the transaction.
				var id string
				err := q.QueryRow(ctx, `
					INSERT INTO trips (school_id, trip_date, trip_type, route_id, bus_id, driver_id, notes, created_by)
					VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING RETURNING id`,
					schoolID, d, t.TripType, t.Route.ID, t.Bus.ID, t.Driver.ID, t.Notes, p.UserID).Scan(&id)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) { // ON CONFLICT DO NOTHING inserted nothing
						skip("route, bus or driver already has a trip in this slot")
						continue
					}
					return err
				}
				if err := store.InsertTripHistory(ctx, q, store.TripHistoryEntry{
					TripID: id, SchoolID: schoolID, To: models.TripScheduled, ByUserID: &p.UserID, ByRole: string(p.Role),
					Reason: "copied from " + req.FromDate,
				}); err != nil {
					return err
				}
				res.Created++
			}
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "trip.copy", EntityType: "trip",
			After: map[string]any{"from_date": req.FromDate, "to_dates": req.ToDates, "created": res.Created, "skipped": len(res.Skipped)},
		})
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func firstFieldError(fields map[string]string) string {
	for _, f := range []string{"trip_date", "route_id", "bus_id", "driver_id"} {
		if msg, ok := fields[f]; ok {
			return strings.TrimSuffix(f, "_id") + ": " + msg
		}
	}
	for f, msg := range fields {
		return f + ": " + msg
	}
	return "invalid"
}

// --- Driver app: /driver/trips (caller-scoped) ---

// currentDriver loads the driver profile of the logged-in driver.
func (a *API) currentDriver(w http.ResponseWriter, r *http.Request) (*models.Driver, bool) {
	d, err := store.GetDriverByUser(r.Context(), a.Store.Pool, auth.FromContext(r.Context()).UserID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "driver_profile_missing", "No driver profile is linked to this login. Contact your school.")
		return nil, false
	}
	if err != nil {
		internalError(w, r, err)
		return nil, false
	}
	return d, true
}

type driverTrip struct {
	*models.Trip
	Stops []tripStop `json:"stops"`
}

// DriverTrips lists the driver's own trips for ?date= (default: today in the school's
// time zone), Morning Pickup first, each with its ordered stops.
func (a *API) DriverTrips(w http.ResponseWriter, r *http.Request) {
	d, ok := a.currentDriver(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	date := r.URL.Query().Get("date")
	if date == "" {
		var err error
		if date, err = store.SchoolToday(ctx, a.Store.Pool, d.SchoolID); err != nil {
			internalError(w, r, err)
			return
		}
	} else if _, err := time.Parse(time.DateOnly, date); err != nil {
		httpx.ValidationError(w, validate.Errors{"date": "must be a date YYYY-MM-DD"})
		return
	}
	trips, err := store.DriverTripsOn(ctx, a.Store.Pool, d.ID, date)
	if err != nil {
		internalError(w, r, err)
		return
	}
	out := make([]driverTrip, len(trips))
	for i := range trips {
		out[i].Trip = &trips[i]
		if out[i].Stops, err = a.tripStops(ctx, &trips[i]); err != nil {
			internalError(w, r, err)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) DriverTrip(w http.ResponseWriter, r *http.Request) {
	a.driverTripAction(w, r, nil)
}

// driverTripAction loads the driver's own trip (another driver's trip is 404) and,
// if act is set, applies a status change.
func (a *API) driverTripAction(w http.ResponseWriter, r *http.Request, act func(*models.Trip) (to string, err error)) {
	d, ok := a.currentDriver(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	ctx := r.Context()
	var t *models.Trip
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		var err error
		if t, err = store.GetDriverTrip(ctx, q, d.ID, id); err != nil {
			return err
		}
		if act == nil {
			return nil
		}
		to, err := act(t)
		if err != nil {
			return err
		}
		if err := a.changeTripStatus(ctx, q, r, t, to, "", "trip."+to); err != nil {
			return err
		}
		t, err = store.GetDriverTrip(ctx, q, d.ID, id)
		return err
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	if act != nil {
		a.publishTripStatus(r, t)
	}
	res := driverTrip{Trip: t}
	if res.Stops, err = a.tripStops(ctx, t); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// DriverConfirmTrip: scheduled → confirmed ("I will drive this trip").
func (a *API) DriverConfirmTrip(w http.ResponseWriter, r *http.Request) {
	a.driverTripAction(w, r, func(t *models.Trip) (string, error) {
		if t.Status != models.TripScheduled {
			return "", invalidTransition(t, "confirmed")
		}
		return models.TripConfirmed, nil
	})
}

// DriverStartTrip: confirmed → started, only on the trip's date. GPS sharing begins (Sprint 5).
func (a *API) DriverStartTrip(w http.ResponseWriter, r *http.Request) {
	a.driverTripAction(w, r, func(t *models.Trip) (string, error) {
		if t.Status != models.TripConfirmed {
			if t.Status == models.TripScheduled {
				return "", &tripError{http.StatusConflict, httpx.ErrorBody{
					Code: "invalid_transition", Message: "Confirm the trip before starting it.",
				}}
			}
			return "", invalidTransition(t, "started")
		}
		if err := a.requireToday(r.Context(), t); err != nil {
			return "", err
		}
		return models.TripStarted, nil
	})
}

// DriverEndTrip: started → completed. GPS sharing stops.
func (a *API) DriverEndTrip(w http.ResponseWriter, r *http.Request) {
	a.driverTripAction(w, r, func(t *models.Trip) (string, error) {
		if t.Status != models.TripStarted {
			return "", invalidTransition(t, "ended")
		}
		return models.TripCompleted, nil
	})
}

// --- Parent app ---

// parentTrip is what a parent may see about a trip: no driver mobile, no other students.
type parentTrip struct {
	ID            string     `json:"id"`
	TripDate      string     `json:"trip_date"`
	TripType      string     `json:"trip_type"`
	Status        string     `json:"status"`
	BusNumber     string     `json:"bus_number"`
	DriverName    string     `json:"driver_name"`
	StartedAt     *time.Time `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at"`
	CancelReason  string     `json:"cancel_reason"`
	ChildStopName string     `json:"child_stop_name"` // pickup stop (morning) or drop stop (evening)
}

// ParentChildTrips lists the child's trips for ?date= (default today): Morning Pickup if
// the child has a pickup stop, Evening Drop if they have a drop stop.
func (a *API) ParentChildTrips(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "studentID")
	if !ok {
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	children, err := store.ParentChildren(ctx, a.Store.Pool, p.UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	var child *store.ParentChild
	for i := range children {
		if children[i].StudentID == id {
			child = &children[i]
		}
	}
	if child == nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	out := []parentTrip{}
	if child.Assignment == nil {
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		if date, err = store.SchoolToday(ctx, a.Store.Pool, *p.SchoolID); err != nil {
			internalError(w, r, err)
			return
		}
	} else if _, err := time.Parse(time.DateOnly, date); err != nil {
		httpx.ValidationError(w, validate.Errors{"date": "must be a date YYYY-MM-DD"})
		return
	}
	trips, err := store.RouteTripsOn(ctx, a.Store.Pool, child.Assignment.RouteID, date)
	if err != nil {
		internalError(w, r, err)
		return
	}
	for _, t := range trips {
		stop := child.Assignment.PickupStop
		if t.TripType == models.TripEveningDrop {
			stop = child.Assignment.DropStop
		}
		if stop == nil {
			continue // the child does not ride this trip type
		}
		out = append(out, parentTrip{
			ID: t.ID, TripDate: t.TripDate, TripType: t.TripType, Status: t.Status, BusNumber: t.Bus.VehicleNumber,
			DriverName: t.Driver.Name, StartedAt: t.StartedAt, EndedAt: t.EndedAt, CancelReason: t.CancelReason,
			ChildStopName: stop.Name,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}
