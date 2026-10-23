package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
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

var tripTypes = []string{models.TripMorningPickup, models.TripEveningDrop, models.TripSpecial}

// maxWaypoints bounds a Special Trip/Outing's ad-hoc destination list.
const maxWaypoints = 20

// Exclusion/unique constraints → the request field that clashes. The
// _no_overlap constraints fire when two non-cancelled trips on the same
// route/bus/driver have overlapping scheduled windows. The _slot_key indexes
// are the schedule-less counterpart (restored alongside _no_overlap: a trip
// with no scheduled window has no window to compare, so it keeps the original
// one-per-route/bus/driver-per-date+type rule instead) — reached here only
// from staff-facing paths (CreateTrip/UpdateTrip/insertTripIfFree); claims go
// through writeClaimError, which maps the same names to friendlier codes
// first.
var tripSlotConflicts = map[string]struct{ field, msg string }{
	"trips_route_no_overlap":  {"route_id", "This route already has an overlapping trip at that time."},
	"trips_bus_no_overlap":    {"bus_id", "This bus already has an overlapping trip at that time."},
	"trips_driver_no_overlap": {"driver_id", "This driver already has an overlapping trip at that time."},
	"trips_route_slot_key":    {"route_id", "This route already has a trip for that date and trip type."},
	"trips_bus_slot_key":      {"bus_id", "This bus already has a trip for that date and trip type."},
	"trips_driver_slot_key":   {"driver_id", "This driver already has a trip for that date and trip type."},
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
	// RouteID is required for morning_pickup/evening_drop; optional (template) for
	// special_trip. "" or omitted means no route — only valid for special_trip.
	RouteID  string `json:"route_id"`
	BusID    string `json:"bus_id"`
	DriverID string `json:"driver_id"`
	Notes    string `json:"notes"`
	// Title: the outing's name, shown instead of a route name. Required for a
	// routeless special_trip; ignored for the other two trip types.
	Title string `json:"title"`
	// NotifyParents: nil defaults to true (matching the DB default and the other
	// two trip types, which always notify). Only meaningful for special_trip.
	NotifyParents *bool `json:"notify_parents"`
	// ScheduledStartTime/ScheduledEndTime: this trip's own planned window, as
	// HH:MM wall-clock in the school's own time zone (same convention as
	// stops.pickup_time/drop_time) — required for every new trip, so every new
	// trip gets real overlap protection (trips_{route,bus,driver}_no_overlap).
	// An end time at or before the start time means the trip runs past
	// midnight into the next calendar day.
	ScheduledStartTime string `json:"scheduled_start_time"`
	ScheduledEndTime   string `json:"scheduled_end_time"`
	// scheduledStart/scheduledEnd: resolved to real UTC instants by validateTrip
	// (combining the above with TripDate and the school's time zone) — Go-only,
	// never serialized; input() reads these, not the raw HH:MM strings.
	scheduledStart, scheduledEnd *time.Time
	// reassignTripID: set by validateTrip when allowReassign is true and the
	// only clash found is an existing, fully-unassigned trip occupying this
	// exact route+date+type+window — the placeholder Unassign leaves behind.
	// The caller should fill that trip in (store.UpdateTripAssignment) instead
	// of creating a new row or treating it as a duplicate-route conflict. ""
	// means proceed with the normal create path.
	reassignTripID string
	Waypoints      []waypointRequest `json:"waypoints"`
}

// waypointRequest is one ad-hoc destination of a special_trip, in the order given.
type waypointRequest struct {
	Name            string   `json:"name"`
	Landmark        string   `json:"landmark"`
	Latitude        *float64 `json:"latitude"`
	Longitude       *float64 `json:"longitude"`
	GeofenceRadiusM *int     `json:"geofence_radius_m"`
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

var hhmmPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// maxTripHours bounds a single trip's scheduled window — generous enough for a
// genuine overnight outing, tight enough to catch an admin swapping start/end
// by mistake (which combineScheduledWindow would otherwise read as "runs
// almost all the way around the clock").
const maxTripHours = 18 * time.Hour

// combineScheduledWindow turns a trip's calendar date plus HH:MM start/end
// wall-clock times (school-local, same convention as stops.pickup_time) into
// real UTC instants. An end time at or before the start time means the trip
// runs past midnight — its end resolves on the following calendar day, so a
// late-evening-drop-into-night outing is represented correctly rather than
// rejected.
func combineScheduledWindow(tripDate, startHHMM, endHHMM, tz string) (start, end time.Time, err error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("school time zone %q: %w", tz, err)
	}
	start, err = time.ParseInLocation("2006-01-02 15:04", tripDate+" "+startHHMM, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err = time.ParseInLocation("2006-01-02 15:04", tripDate+" "+endHHMM, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !end.After(start) {
		end = end.AddDate(0, 0, 1)
	}
	return start, end, nil
}

// validateTrip checks the date, trip type and that route, bus and driver belong to
// the school, are active and fit the trip type. A special_trip may have no route
// (an ad-hoc outing with its own waypoints instead); it then needs a title and at
// least one waypoint. excludeTripID is the trip's own ID when this is an update
// (never a self-conflict against the very row being changed), or "" for a new trip.
// skipClash is true only for BulkCreateTrips' one-time "is this route/bus/driver
// valid at all" check against a placeholder date — the real, date-specific clash
// check still runs for every date in its own per-date loop right after.
// allowReassign enables the "fill in an existing unassigned slot instead of
// rejecting as a duplicate route conflict" behavior (see reassignTripID) —
// true only for the staff create-style paths (CreateTrip, CopyTrips,
// BulkCreateTrips); false for UpdateTrip (editing one specific trip already
// has excludeTripID for self-exclusion; silently redirecting an edit into a
// DIFFERENT trip's slot would be surprising) and for driver route-slot
// claims (a distinct, concurrency-sensitive flow not part of this).
func validateTrip(ctx context.Context, q store.DBTX, schoolID, excludeTripID string, skipClash, allowReassign bool, req *tripRequest) error {
	today, err := store.SchoolToday(ctx, q, schoolID)
	if err != nil {
		return err
	}
	req.Notes = strings.TrimSpace(req.Notes)
	req.Title = strings.TrimSpace(req.Title)
	v := validate.New()
	validateTripDate(v, "trip_date", req.TripDate, today)
	v.OneOf("trip_type", req.TripType, tripTypes...)
	special := req.TripType == models.TripSpecial
	hasRoute := req.RouteID != ""

	if hasRoute || !special {
		_, err := uuid.Parse(req.RouteID)
		v.Check(err == nil, "route_id", "is required")
	}
	for field, id := range map[string]string{"bus_id": req.BusID, "driver_id": req.DriverID} {
		_, err := uuid.Parse(id)
		v.Check(err == nil, field, "is required")
	}
	v.MaxLen("notes", req.Notes, 500)
	if special && !hasRoute {
		v.Required("title", req.Title)
		v.MaxLen("title", req.Title, 200)
		v.Check(len(req.Waypoints) >= 1, "waypoints", "add at least one destination")
		v.Check(len(req.Waypoints) <= maxWaypoints, "waypoints", fmt.Sprintf("at most %d destinations", maxWaypoints))
		for _, wp := range req.Waypoints {
			v.Check(strings.TrimSpace(wp.Name) != "", "waypoints", "every destination needs a name")
			v.Check(wp.Latitude != nil && *wp.Latitude >= -90 && *wp.Latitude <= 90, "waypoints", "every destination needs a valid location")
			v.Check(wp.Longitude != nil && *wp.Longitude >= -180 && *wp.Longitude <= 180, "waypoints", "every destination needs a valid location")
			if wp.GeofenceRadiusM != nil {
				v.Check(*wp.GeofenceRadiusM >= minGeofenceM && *wp.GeofenceRadiusM <= maxGeofenceM,
					"waypoints", "geofence radius must be between 25 and 1000 metres")
			}
		}
	}
	if !v.OK() {
		return tripValidation(v.Errors())
	}

	var routeID *string
	if hasRoute {
		routeID = &req.RouteID
	}
	refs, err := store.GetTripRefs(ctx, q, schoolID, routeID, req.BusID, req.DriverID)
	if err != nil {
		return err
	}
	if hasRoute {
		v.Check(refs.RouteFound, "route_id", "route not found")
		v.Check(!refs.RouteFound || refs.RouteStatus == models.StatusActive, "route_id", "route is inactive")
		v.Check(!refs.RouteFound || refs.StopCount > 0, "route_id", "route has no stops yet")
		switch req.TripType {
		case models.TripMorningPickup:
			v.Check(refs.SupportsPickup, "route_id", "route does not run Morning Pickup")
		case models.TripEveningDrop:
			v.Check(refs.SupportsDrop, "route_id", "route does not run Evening Drop")
			v.Check(refs.DropSequenceComplete, "route_id", "the route's Evening Drop stop order is not fully configured yet")
		case models.TripSpecial:
			v.Check(refs.SupportsOuting, "route_id", "route is not marked for Special Trip/Outing")
		}
	}
	v.Check(refs.BusFound, "bus_id", "bus not found")
	v.Check(!refs.BusFound || refs.BusStatus == models.StatusActive, "bus_id", "bus is not active ("+refs.BusStatus+")")
	v.Check(refs.DriverFound, "driver_id", "driver not found")
	v.Check(!refs.DriverFound || refs.DriverStatus == models.StatusActive, "driver_id", "driver is not active")
	if !v.OK() {
		return tripValidation(v.Errors())
	}

	// Scheduled window: optional at this layer (empty both = "no schedule", the
	// same as a trip created before this feature existed — never invented).
	// The admin web's own form requires both for a brand-new trip; a copy or
	// bulk-create that carries over a legacy, schedule-less source trip passes
	// both empty on purpose and must not be forced to invent a time here.
	// Giving only one of the two is always a mistake, not a valid "no schedule".
	hasStart, hasEnd := req.ScheduledStartTime != "", req.ScheduledEndTime != ""
	if hasStart || hasEnd {
		v.Check(hhmmPattern.MatchString(req.ScheduledStartTime), "scheduled_start_time", "must be HH:MM")
		v.Check(hhmmPattern.MatchString(req.ScheduledEndTime), "scheduled_end_time", "must be HH:MM (give both start and end, or neither)")
		if !v.OK() {
			return tripValidation(v.Errors())
		}
		tz, err := store.SchoolTimezone(ctx, q, schoolID)
		if err != nil {
			return err
		}
		start, end, err := combineScheduledWindow(req.TripDate, req.ScheduledStartTime, req.ScheduledEndTime, tz)
		if err != nil {
			return tripValidation(validate.Errors{"scheduled_start_time": "could not be resolved: " + err.Error()})
		}
		v.Check(end.Sub(start) <= maxTripHours, "scheduled_end_time", "a single trip cannot be longer than 18 hours")
		if !v.OK() {
			return tripValidation(v.Errors())
		}
		req.scheduledStart, req.scheduledEnd = &start, &end
	}

	if skipClash {
		return nil
	}
	// A slot being re-assigned (staff create, Bulk Assign, Copy) might
	// already have an existing, fully-unassigned trip occupying this exact
	// route+date+type — the placeholder Unassign leaves behind. This check
	// runs independently of whether the window below would even detect a
	// clash: an admin correcting that trip's schedule while re-assigning it
	// means the new window may not overlap the old one at all, so waiting
	// for TripSlotClash to report a route clash first (and only then asking
	// whether it was self-caused) would miss exactly that case — letting the
	// insert below succeed and silently orphan the old row forever while
	// creating a duplicate alongside it. The bus and driver are still
	// checked for a real conflict elsewhere (never against route, since this
	// is that route's own slot) before anything is reused.
	if allowReassign && hasRoute {
		existingID, ferr := store.FindUnassignedSlot(ctx, q, schoolID, req.RouteID, req.TripDate, req.TripType, req.scheduledStart, req.scheduledEnd)
		if ferr != nil {
			return ferr
		}
		if existingID != "" {
			clash2, ferr := store.TripSlotClash(ctx, q, schoolID, nil, &req.BusID, &req.DriverID, req.TripDate, req.TripType, existingID, req.scheduledStart, req.scheduledEnd)
			if ferr != nil {
				return ferr
			}
			clashField2 := map[string]string{
				"bus is already on another trip":    "bus_id",
				"driver is already on another trip": "driver_id",
			}[clash2]
			if clashField2 == "" {
				req.reassignTripID = existingID
				return nil
			}
			msg := "This " + strings.TrimSuffix(clashField2, "_id") + " already has an overlapping trip at that time."
			return &tripError{http.StatusConflict, httpx.ErrorBody{
				Code: "trip_conflict", Message: msg, Fields: map[string]string{clashField2: msg},
			}}
		}
	}
	// Friendlier, field-level pre-check ahead of the transactional guards
	// (trips_{route,bus,driver}_no_overlap for scheduled trips, the restored
	// trips_{route,bus,driver}_slot_key for schedule-less ones) — a race
	// between two concurrent requests is still only actually prevented by
	// those DB constraints, same as it always was for the scheduled case.
	// Runs for every request, schedule or none: a schedule-less request (a
	// driver's route-slot claim, or a copy/bulk-create that intentionally
	// carries a legacy trip's lack of schedule forward) has no window to
	// compare, so TripSlotClash treats it as a clash with anything already on
	// the same route/bus/driver/date+type — scheduled or not — closing the
	// gap where a schedule-less trip could otherwise be slipped in alongside
	// an existing scheduled one undetected. A route-slot claim reaching this
	// still gets its usual route_taken/bus_taken/already_assigned codes:
	// writeClaimError translates this generic trip_conflict back to them.
	clash, err := store.TripSlotClash(ctx, q, schoolID, routeID, &req.BusID, &req.DriverID, req.TripDate, req.TripType, excludeTripID, req.scheduledStart, req.scheduledEnd)
	if err != nil {
		return err
	}
	clashField := map[string]string{
		"route already has a trip":          "route_id",
		"bus is already on another trip":    "bus_id",
		"driver is already on another trip": "driver_id",
	}[clash]
	if clashField != "" {
		msg := "This " + strings.TrimSuffix(clashField, "_id") + " already has an overlapping trip at that time."
		return &tripError{http.StatusConflict, httpx.ErrorBody{
			Code: "trip_conflict", Message: msg, Fields: map[string]string{clashField: msg},
		}}
	}
	return nil
}

func (req *tripRequest) input() store.TripInput {
	var routeID *string
	if req.RouteID != "" {
		routeID = &req.RouteID
	}
	notify := true
	if req.NotifyParents != nil {
		notify = *req.NotifyParents
	}
	return store.TripInput{TripDate: req.TripDate, TripType: req.TripType, RouteID: routeID,
		BusID: req.BusID, DriverID: req.DriverID, Notes: req.Notes, Title: req.Title, NotifyParents: notify,
		ScheduledStartAt: req.scheduledStart, ScheduledEndAt: req.scheduledEnd}
}

// waypointInputs converts the request's waypoints for store.ReplaceWaypoints.
func (req *tripRequest) waypointInputs() []store.WaypointInput {
	out := make([]store.WaypointInput, len(req.Waypoints))
	for i, wp := range req.Waypoints {
		radius := defaultGeofenceM
		if wp.GeofenceRadiusM != nil {
			radius = *wp.GeofenceRadiusM
		}
		var lat, lng float64
		if wp.Latitude != nil {
			lat = *wp.Latitude
		}
		if wp.Longitude != nil {
			lng = *wp.Longitude
		}
		out[i] = store.WaypointInput{Name: strings.TrimSpace(wp.Name), Landmark: strings.TrimSpace(wp.Landmark),
			Latitude: lat, Longitude: lng, GeofenceRadiusM: radius}
	}
	return out
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

// publishTripStatus tells live subscribers and notifies parents (Bus Started, Trip
// Completed, Trip Cancelled), after the change is committed. A failure here only
// delays the live view or a push, so it is logged, not returned.
func (a *API) publishTripStatus(r *http.Request, t *models.Trip) {
	if err := a.Live.TripStatusChanged(r.Context(), t.SchoolID, t.ID, t.Status); err != nil {
		internalErrorLog(r, err)
	}
	if err := a.notifyTripStatus(r.Context(), t); err != nil {
		internalErrorLog(r, err)
	}
	if t.Status == models.TripCompleted {
		a.snapshotRouteLater(t)
	}
}

// --- Admin web: /schools/{schoolID}/trips ---

func (a *API) ListTrips(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	pg := page(r)
	trips, total, err := store.ListTrips(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), store.TripFilter{
		Date: qs.Get("date"), From: qs.Get("from"), To: qs.Get("to"), TripType: qs.Get("trip_type"),
		Status: qs.Get("status"), RouteID: qs.Get("route_id"), DriverID: qs.Get("driver_id"), BusID: qs.Get("bus_id"),
		Ascending: qs.Get("sort") == "asc",
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

// tripStops returns a trip's stops (or, for a routeless special_trip, its
// waypoints converted to the same shape) with per-stop student counts. A
// special_trip's destinations never have students assigned (they are not
// permanent stops), so their count is always 0.
func (a *API) tripStops(ctx context.Context, t *models.Trip) ([]tripStop, error) {
	if t.Route == nil {
		wps, err := store.ListWaypoints(ctx, a.Store.Pool, t.ID)
		if err != nil {
			return nil, err
		}
		out := make([]tripStop, len(wps))
		for i, wp := range wps {
			out[i] = tripStop{Stop: store.WaypointAsStop(wp)}
		}
		return out, nil
	}
	stops, err := store.ListStops(ctx, a.Store.Pool, t.Route.ID)
	if err != nil {
		return nil, err
	}
	// ListStops orders by Sequence (the Morning Pickup order). An Evening Drop
	// trip shows its own, independently configured order instead, and only the
	// stops that are actually part of the evening leg.
	if t.TripType == models.TripEveningDrop {
		evening := make([]models.Stop, 0, len(stops))
		for _, s := range stops {
			if s.DropSequence != nil {
				evening = append(evening, s)
			}
		}
		sort.Slice(evening, func(i, j int) bool { return *evening[i].DropSequence < *evening[j].DropSequence })
		stops = evening
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
	var before *models.Trip
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		if err := validateTrip(ctx, q, schoolID, "", false, true, &req); err != nil {
			return err
		}
		var id string
		if req.reassignTripID != "" {
			// Not a new trip: an existing, fully-unassigned one for this
			// exact route+date+type+window — fill it in instead of a second
			// row or a "route already has an overlapping trip" rejection of
			// itself. Status never changed (no new Scheduled history entry).
			id = req.reassignTripID
			var err error
			if before, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
				return err
			}
			if err := store.UpdateTripAssignment(ctx, q, schoolID, id, req.input()); err != nil {
				return err
			}
		} else {
			var err error
			id, err = store.CreateTrip(ctx, q, schoolID, p.UserID, req.input())
			if err != nil {
				return err
			}
			if err := store.InsertTripHistory(ctx, q, store.TripHistoryEntry{
				TripID: id, SchoolID: schoolID, To: models.TripScheduled, ByUserID: &p.UserID, ByRole: string(p.Role),
			}); err != nil {
				return err
			}
		}
		if req.TripType == models.TripSpecial {
			if err := store.ReplaceWaypoints(ctx, q, schoolID, id, req.waypointInputs()); err != nil {
				return err
			}
		}
		var err error
		if t, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		action := "trip.create"
		if before != nil {
			action = "trip.reassign"
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: action, EntityType: "trip", EntityID: &id, Before: before, After: t,
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
		if err := validateTrip(ctx, q, schoolID, id, false, false, &req); err != nil {
			return err
		}
		if err := store.UpdateTripAssignment(ctx, q, schoolID, id, req.input()); err != nil {
			return err
		}
		if req.TripType == models.TripSpecial {
			if err := store.ReplaceWaypoints(ctx, q, schoolID, id, req.waypointInputs()); err != nil {
				return err
			}
		}
		if before.Status == models.TripConfirmed && (before.Driver == nil || before.Driver.ID != req.DriverID) {
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
			if t.Bus == nil || t.Driver == nil {
				return "", &tripError{http.StatusConflict, httpx.ErrorBody{
					Code: "trip_unassigned", Message: "Assign a bus and driver before starting this trip.",
				}}
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

// UnassignTrip clears a trip's bus and driver together (Part B: Single Trip
// Unassign) — its route, schedule, trip_date, trip_type, stops and students
// are all untouched; only while it's still open (scheduled/confirmed), same
// gate as CancelTrip. A confirmed trip resets to scheduled first (it can no
// longer be "confirmed" by a driver it no longer has). Idempotent: calling it
// again on an already-unassigned trip succeeds (200) rather than erroring, so
// a retried/duplicate request is always safe.
func (a *API) UnassignTrip(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	ctx := r.Context()
	var before, after *models.Trip
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		var err error
		if before, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		if before.Status != models.TripScheduled && before.Status != models.TripConfirmed {
			return invalidTransition(before, "unassigned")
		}
		if before.Status == models.TripConfirmed {
			if err := a.changeTripStatus(ctx, q, r, before, models.TripScheduled, "unassigned", "trip.reset"); err != nil {
				return err
			}
		}
		if err := store.UnassignTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		if after, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "trip.unassign", EntityType: "trip", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	a.afterAssignmentChange(r, after, nil)
	httpx.JSON(w, http.StatusOK, after)
}

type bulkUnassignResult struct {
	TripID string `json:"trip_id"`
	Status string `json:"status"` // unassigned | skipped
	Reason string `json:"reason,omitempty"`
}

// bulkUnassignReason turns an UnassignTrip-style error into a short, specific
// skip reason for the bulk response — never a blanket "failed".
func bulkUnassignReason(err error) string {
	var te *tripError
	if errors.As(err, &te) {
		return te.body.Message
	}
	if errors.Is(err, store.ErrNotFound) {
		return "not found"
	}
	return "could not be unassigned"
}

// maxBulkUnassignFilterTrips bounds a filter-resolved batch (date range / all
// dates) — much larger than the explicit-trip_ids cap below, since Trip
// Planner's cross-date flow legitimately targets many more trips than a
// Daily Trips row selection ever would, but still bounded: a request that
// would resolve more than this many trips is refused outright (400) rather
// than silently processed only up to the limit, so the caller always knows
// to narrow the filter (e.g. a shorter date range) and never fire-and-forgets
// a mystery-sized operation.
const maxBulkUnassignFilterTrips = 500

// bulkUnassignFilterRequest is the date-range/All Dates alternative to an
// explicit trip_ids list — Trip Planner's cross-date Bulk Unassign. At least
// one dimension must narrow the request (see hasMeaningfulScope): a
// completely bare filter is rejected rather than silently defaulting to
// "every trip", so an accidental omission can never resolve to more than the
// caller actually meant. A route/bus/driver/trip_type filter alone is
// enough — its applicable date span is auto-detected from the matching trip
// records themselves (see BulkUnassignPreview), so the caller is never
// forced to type in a date range it would have to work out by hand.
type bulkUnassignFilterRequest struct {
	AllDates bool   `json:"all_dates"`
	From     string `json:"from"`
	To       string `json:"to"`
	RouteID  string `json:"route_id"`
	BusID    string `json:"bus_id"`
	DriverID string `json:"driver_id"`
	TripType string `json:"trip_type"`
}

func (f bulkUnassignFilterRequest) toTripFilter() store.TripFilter {
	return store.TripFilter{From: f.From, To: f.To, RouteID: f.RouteID, BusID: f.BusID, DriverID: f.DriverID, TripType: f.TripType}
}

// hasMeaningfulScope reports whether a filter request narrows the operation
// enough to resolve server-side without ever silently defaulting to "every
// trip": an explicit date signal (all_dates/from/to), or an entity/trip-type
// filter (route/bus/driver/trip_type) whose applicable period gets
// auto-detected from the matching trips instead. A completely bare filter —
// nothing at all — is rejected: the admin must say All Dates explicitly.
func (f bulkUnassignFilterRequest) hasMeaningfulScope() bool {
	return f.AllDates || f.From != "" || f.To != "" ||
		f.RouteID != "" || f.BusID != "" || f.DriverID != "" || f.TripType != ""
}

const bulkUnassignScopeMessage = "give trip_ids, choose All Dates, a date range, or a route/bus/driver/trip type filter"

// BulkUnassignPreview reports how many trips a Bulk Unassign filter would
// target — split into eligible (scheduled/confirmed, what would actually be
// unassigned) and ineligible (already started/completed, would be reported
// skipped) — plus the earliest/latest date among all of them. That date span
// is the auto-detected "applicable assignment period" for a route/driver/bus
// selection: Trip Planner shows it, and the exact matching count, before the
// confirm step ever becomes available, so the admin always sees the real
// scope first — especially for All Dates.
func (a *API) BulkUnassignPreview(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req bulkUnassignFilterRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	v.Check(req.hasMeaningfulScope(), "all_dates", bulkUnassignScopeMessage)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	total, eligible, ineligible, earliest, latest, err := store.BulkUnassignPreview(r.Context(), a.Store.Pool, schoolID, req.toTripFilter())
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"count":      total,
		"eligible":   eligible,
		"ineligible": ineligible,
		"from":       nullableDate(earliest),
		"to":         nullableDate(latest),
		"over_limit": total > maxBulkUnassignFilterTrips,
		"limit":      maxBulkUnassignFilterTrips,
	})
}

func nullableDate(d string) any {
	if d == "" {
		return nil
	}
	return d
}

// BulkUnassignTrips unassigns many trips at once (Part C: Bulk Unassign,
// alongside the existing Bulk Assign) — either an explicit trip_ids list
// (Daily Trips' row-selection flow, capped at 200) or a date-range/All-Dates
// filter (Trip Planner's cross-date flow, resolved server-side and capped at
// maxBulkUnassignFilterTrips) — never silently restricted to "whatever page
// happens to be open" the way the old day-scoped-only selection was. Either
// way, each trip ID is its own transaction, per this project's established
// bulk-operation pattern (BulkCreateTrips, CopyTrips): one ineligible,
// already-changed, or cross-school trip is reported and skipped, never
// silently dropped and never blocking the rest. A trip ID outside the
// caller's school comes back "not found" — the same tenant-isolation
// behavior as every other school-scoped lookup — never revealing that it
// exists elsewhere (and a filter never resolves another school's trips at
// all, since it is itself school-scoped).
func (a *API) BulkUnassignTrips(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req struct {
		TripIDs []string `json:"trip_ids"`
		bulkUnassignFilterRequest
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	filterMode := len(req.TripIDs) == 0
	if filterMode {
		v.Check(req.hasMeaningfulScope(), "all_dates", bulkUnassignScopeMessage)
	} else {
		v.Check(len(req.TripIDs) <= 200, "trip_ids", "give at most 200 trip IDs directly — use all_dates/from/to for more")
		seen := map[string]bool{}
		for _, id := range req.TripIDs {
			if _, err := uuid.Parse(id); err != nil {
				v.Check(false, "trip_ids", "every ID must be a valid UUID")
				break
			}
			v.Check(!seen[id], "trip_ids", "a trip is listed twice")
			seen[id] = true
		}
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	ctx := r.Context()
	tripIDs := req.TripIDs
	if filterMode {
		ids, err := store.BulkUnassignResolveIDs(ctx, a.Store.Pool, schoolID, req.toTripFilter(), maxBulkUnassignFilterTrips)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if len(ids) > maxBulkUnassignFilterTrips {
			httpx.ValidationError(w, validate.Errors{"from": fmt.Sprintf(
				"matches more than %d trips — narrow the date range or add a route/bus/driver/trip type filter", maxBulkUnassignFilterTrips)})
			return
		}
		if len(ids) == 0 {
			httpx.JSON(w, http.StatusOK, map[string]any{"results": []bulkUnassignResult{}})
			return
		}
		tripIDs = ids
	}
	results := make([]bulkUnassignResult, 0, len(tripIDs))
	for _, id := range tripIDs {
		var after *models.Trip
		err := a.Store.InTx(ctx, func(q store.DBTX) error {
			before, err := store.GetTrip(ctx, q, schoolID, id)
			if err != nil {
				return err
			}
			if before.Status != models.TripScheduled && before.Status != models.TripConfirmed {
				return invalidTransition(before, "unassigned")
			}
			if before.Status == models.TripConfirmed {
				if err := a.changeTripStatus(ctx, q, r, before, models.TripScheduled, "bulk unassigned", "trip.reset"); err != nil {
					return err
				}
			}
			if err := store.UnassignTrip(ctx, q, schoolID, id); err != nil {
				return err
			}
			if after, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
				return err
			}
			return a.audit(ctx, q, r, store.AuditEntry{
				SchoolID: &schoolID, Action: "trip.unassign", EntityType: "trip", EntityID: &id, Before: before, After: after,
			})
		})
		if err != nil {
			results = append(results, bulkUnassignResult{TripID: id, Status: "skipped", Reason: bulkUnassignReason(err)})
			continue
		}
		a.afterAssignmentChange(r, after, nil)
		results = append(results, bulkUnassignResult{TripID: id, Status: "unassigned"})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"results": results})
}

// ReassignTripDriver replaces a trip's driver only (Part D/E: Replace Driver,
// one atomic step, no separate unassign-then-reassign) — bus, route,
// schedule, status and everything else stay exactly as they are; this one
// trip row is the only thing that changes, so other dates and any recurring
// driver_route_assignments entry are completely untouched. The replacement is
// checked only on the driver dimension (store.TripSlotClash with route and
// bus passed as nil so they can never contribute a false match) against this
// trip's own existing schedule — the same overlap rule used everywhere else
// in this feature, not a parallel one.
func (a *API) ReassignTripDriver(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	var req struct {
		DriverID string `json:"driver_id"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	_, uerr := uuid.Parse(req.DriverID)
	v.Check(uerr == nil, "driver_id", "is required")
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	ctx := r.Context()
	var before, after *models.Trip
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		var err error
		if before, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		if before.Status != models.TripScheduled && before.Status != models.TripConfirmed {
			return invalidTransition(before, "changed")
		}
		refs, err := store.GetTripRefs(ctx, q, schoolID, nil, "", req.DriverID)
		if err != nil {
			return err
		}
		rv := validate.New()
		rv.Check(refs.DriverFound, "driver_id", "driver not found")
		rv.Check(!refs.DriverFound || refs.DriverStatus == models.StatusActive, "driver_id", "driver is not active")
		if !rv.OK() {
			return tripValidation(rv.Errors())
		}
		clash, err := store.TripSlotClash(ctx, q, schoolID, nil, nil, &req.DriverID,
			before.TripDate, before.TripType, before.ID, before.ScheduledStartAt, before.ScheduledEndAt)
		if err != nil {
			return err
		}
		if clash != "" {
			msg := "This driver already has an overlapping trip at that time."
			return &tripError{http.StatusConflict, httpx.ErrorBody{Code: "trip_conflict", Message: msg, Fields: map[string]string{"driver_id": msg}}}
		}
		if err := store.ReassignDriver(ctx, q, schoolID, id, req.DriverID); err != nil {
			return err
		}
		if before.Status == models.TripConfirmed && (before.Driver == nil || before.Driver.ID != req.DriverID) {
			if err := a.changeTripStatus(ctx, q, r, before, models.TripScheduled, "driver replaced", "trip.reset"); err != nil {
				return err
			}
		}
		if after, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "trip.reassign_driver", EntityType: "trip", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	a.afterAssignmentChange(r, after, nil)
	httpx.JSON(w, http.StatusOK, after)
}

// ReassignTripBus replaces a trip's bus only (Part F) — driver, route,
// schedule and status stay exactly as they are. Same pattern as
// ReassignTripDriver, checked only on the bus dimension.
func (a *API) ReassignTripBus(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	var req struct {
		BusID string `json:"bus_id"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	_, uerr := uuid.Parse(req.BusID)
	v.Check(uerr == nil, "bus_id", "is required")
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	ctx := r.Context()
	var before, after *models.Trip
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		var err error
		if before, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		if before.Status != models.TripScheduled && before.Status != models.TripConfirmed {
			return invalidTransition(before, "changed")
		}
		refs, err := store.GetTripRefs(ctx, q, schoolID, nil, req.BusID, "")
		if err != nil {
			return err
		}
		rv := validate.New()
		rv.Check(refs.BusFound, "bus_id", "bus not found")
		rv.Check(!refs.BusFound || refs.BusStatus == models.StatusActive, "bus_id", "bus is not active ("+refs.BusStatus+")")
		if !rv.OK() {
			return tripValidation(rv.Errors())
		}
		clash, err := store.TripSlotClash(ctx, q, schoolID, nil, &req.BusID, nil,
			before.TripDate, before.TripType, before.ID, before.ScheduledStartAt, before.ScheduledEndAt)
		if err != nil {
			return err
		}
		if clash != "" {
			msg := "This bus already has an overlapping trip at that time."
			return &tripError{http.StatusConflict, httpx.ErrorBody{Code: "trip_conflict", Message: msg, Fields: map[string]string{"bus_id": msg}}}
		}
		if err := store.ReassignBus(ctx, q, schoolID, id, req.BusID); err != nil {
			return err
		}
		if after, err = store.GetTrip(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "trip.reassign_bus", EntityType: "trip", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	a.afterAssignmentChange(r, after, nil)
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
	res := copyResult{Skipped: []copySkipped{}}
	err = a.Store.InTx(ctx, func(q store.DBTX) error {
		tz, err := store.SchoolTimezone(ctx, q, schoolID)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return fmt.Errorf("school time zone %q: %w", tz, err)
		}
		source, err := store.TripsOnDate(ctx, q, schoolID, req.FromDate, req.TripTypes)
		if err != nil {
			return err
		}
		for _, d := range req.ToDates {
			for _, t := range source {
				routeCode := ""
				var routeID *string
				if t.Route != nil {
					routeCode = t.Route.Code
					routeID = &t.Route.ID
				}
				// Carry over the source trip's own time of day (same wall-clock
				// time, school-local, on the new date) — never invented: a source
				// with no schedule (a legacy trip) copies to another with none.
				var startHHMM, endHHMM string
				if t.ScheduledStartAt != nil && t.ScheduledEndAt != nil {
					startHHMM = t.ScheduledStartAt.In(loc).Format("15:04")
					endHHMM = t.ScheduledEndAt.In(loc).Format("15:04")
				}
				// An unassigned source trip (no bus/driver) copies as empty
				// bus_id/driver_id, same as any other missing-field source —
				// validateTrip rejects it below with a normal "is required"
				// skip reason, never a panic, never an invented assignment.
				busID, driverID := "", ""
				if t.Bus != nil {
					busID = t.Bus.ID
				}
				if t.Driver != nil {
					driverID = t.Driver.ID
				}
				tr := tripRequest{TripDate: d, TripType: t.TripType, BusID: busID, DriverID: driverID,
					Notes: t.Notes, Title: t.Title, NotifyParents: &t.NotifyParents,
					ScheduledStartTime: startHHMM, ScheduledEndTime: endHHMM}
				if routeID != nil {
					tr.RouteID = *routeID
				}
				skip := func(reason string) {
					res.Skipped = append(res.Skipped, copySkipped{TripDate: d, TripType: t.TripType, RouteCode: routeCode, Reason: reason})
				}
				if err := validateTrip(ctx, q, schoolID, "", false, true, &tr); err != nil {
					var te *tripError
					if !errors.As(err, &te) {
						return err
					}
					skip(firstFieldError(te.body.Fields))
					continue
				}
				var id string
				if tr.reassignTripID != "" {
					// An existing, fully-unassigned trip already occupies this
					// exact route+date+type+window on the target date — fill
					// it in instead of skipping it as a duplicate.
					id = tr.reassignTripID
					if err := store.UpdateTripAssignment(ctx, q, schoolID, id, tr.input()); err != nil {
						return err
					}
				} else {
					var clash string
					var err error
					id, clash, err = insertTripIfFree(ctx, q, schoolID, tr.input(), "copied from "+req.FromDate)
					if err != nil {
						return err
					}
					if clash != "" {
						skip(clash)
						continue
					}
				}
				if t.TripType == models.TripSpecial {
					srcWaypoints, err := store.ListWaypoints(ctx, q, t.ID)
					if err != nil {
						return err
					}
					in := make([]store.WaypointInput, len(srcWaypoints))
					for i, wp := range srcWaypoints {
						in[i] = store.WaypointInput{Name: wp.Name, Landmark: wp.Landmark,
							Latitude: wp.Latitude, Longitude: wp.Longitude, GeofenceRadiusM: wp.GeofenceRadiusM}
					}
					if err := store.ReplaceWaypoints(ctx, q, schoolID, id, in); err != nil {
						return err
					}
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

// insertTripIfFree creates a scheduled trip (with its first history entry) unless its
// route, bus or driver already has a trip in the slot. Then it returns what clashes
// instead, without aborting the transaction (ON CONFLICT DO NOTHING).
func insertTripIfFree(ctx context.Context, q store.DBTX, schoolID string, in store.TripInput, reason string) (id, clash string, err error) {
	p := auth.FromContext(ctx)
	err = q.QueryRow(ctx, `
		INSERT INTO trips (school_id, trip_date, trip_type, route_id, bus_id, driver_id, notes, title, notify_parents,
			scheduled_start_at, scheduled_end_at, created_by)
		VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) ON CONFLICT DO NOTHING RETURNING id`,
		schoolID, in.TripDate, in.TripType, in.RouteID, in.BusID, in.DriverID, in.Notes, in.Title, in.NotifyParents,
		in.ScheduledStartAt, in.ScheduledEndAt, p.UserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) { // nothing inserted: name what is taken
		clash, err = store.TripSlotClash(ctx, q, schoolID, in.RouteID, &in.BusID, &in.DriverID, in.TripDate, in.TripType, "", in.ScheduledStartAt, in.ScheduledEndAt)
		if err == nil && clash == "" {
			clash = "route, bus or driver already has a trip in this slot"
		}
		return "", clash, err
	}
	if err != nil {
		return "", "", err
	}
	return id, "", store.InsertTripHistory(ctx, q, store.TripHistoryEntry{
		TripID: id, SchoolID: schoolID, To: models.TripScheduled, ByUserID: &p.UserID, ByRole: string(p.Role), Reason: reason,
	})
}

// maxBulkDates bounds one bulk assignment: every day from today to maxDaysAhead.
const maxBulkDates = maxDaysAhead + 1

// BulkCreateTrips assigns one route + bus + driver to Morning Pickup and/or Evening
// Drop on many dates at once (planning ahead). Route, bus and driver are checked once
// (400 with field errors); then each date + trip type is created, or skipped and
// listed when the date is out of range or the slot is already taken.
func (a *API) BulkCreateTrips(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req struct {
		Dates     []string `json:"dates"`
		TripTypes []string `json:"trip_types"`
		RouteID   string   `json:"route_id"`
		BusID     string   `json:"bus_id"`
		DriverID  string   `json:"driver_id"`
		Notes     string   `json:"notes"`
		// Each included trip type needs its own window (Morning Pickup and
		// Evening Drop never run at the same time of day).
		MorningStartTime string `json:"morning_start_time"`
		MorningEndTime   string `json:"morning_end_time"`
		EveningStartTime string `json:"evening_start_time"`
		EveningEndTime   string `json:"evening_end_time"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	v.Check(len(req.Dates) >= 1 && len(req.Dates) <= maxBulkDates, "dates", fmt.Sprintf("give 1 to %d dates", maxBulkDates))
	seen := map[string]bool{}
	for _, d := range req.Dates {
		_, err := time.Parse(time.DateOnly, d)
		v.Check(err == nil, "dates", "every date must be YYYY-MM-DD")
		v.Check(!seen[d], "dates", "a date is listed twice")
		seen[d] = true
	}
	v.Check(len(req.TripTypes) >= 1 && len(req.TripTypes) <= 2, "trip_types", "choose Morning Pickup, Evening Drop or both")
	for _, tt := range req.TripTypes {
		v.OneOf("trip_types", tt, models.TripMorningPickup, models.TripEveningDrop)
	}
	v.Check(len(req.TripTypes) != 2 || req.TripTypes[0] != req.TripTypes[1], "trip_types", "a trip type is listed twice")
	windowFor := func(tt string) (start, end string) {
		if tt == models.TripMorningPickup {
			return req.MorningStartTime, req.MorningEndTime
		}
		return req.EveningStartTime, req.EveningEndTime
	}
	// Optional per type, same rule as a single trip's own scheduled_start_time/
	// end_time: empty both = no schedule (never invented), one without the
	// other is always a mistake.
	for _, tt := range req.TripTypes {
		start, end := windowFor(tt)
		if start == "" && end == "" {
			continue
		}
		label := map[string]string{models.TripMorningPickup: "morning", models.TripEveningDrop: "evening"}[tt]
		v.Check(hhmmPattern.MatchString(start), label+"_start_time", "must be HH:MM")
		v.Check(hhmmPattern.MatchString(end), label+"_end_time", "must be HH:MM (give both start and end, or neither)")
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	sort.Strings(req.Dates)

	ctx := r.Context()
	res := copyResult{Skipped: []copySkipped{}}
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		today, err := store.SchoolToday(ctx, q, schoolID)
		if err != nil {
			return err
		}
		// Route, bus and driver are the same on every date: check them once, as for today.
		for _, tt := range req.TripTypes {
			check := tripRequest{TripDate: today, TripType: tt, RouteID: req.RouteID, BusID: req.BusID,
				DriverID: req.DriverID, Notes: req.Notes}
			if err := validateTrip(ctx, q, schoolID, "", true, false, &check); err != nil {
				return err
			}
			req.Notes = check.Notes
		}
		route, err := store.GetRoute(ctx, q, schoolID, req.RouteID)
		if err != nil {
			return err
		}
		for _, d := range req.Dates {
			for _, tt := range req.TripTypes {
				skip := func(reason string) {
					res.Skipped = append(res.Skipped, copySkipped{TripDate: d, TripType: tt, RouteCode: route.Code, Reason: reason})
				}
				dv := validate.New()
				validateTripDate(dv, "trip_date", d, today)
				if !dv.OK() {
					skip("date " + dv.Errors()["trip_date"])
					continue
				}
				start, end := windowFor(tt)
				tr := tripRequest{TripDate: d, TripType: tt, RouteID: req.RouteID, BusID: req.BusID, DriverID: req.DriverID, Notes: req.Notes,
					ScheduledStartTime: start, ScheduledEndTime: end}
				if err := validateTrip(ctx, q, schoolID, "", false, true, &tr); err != nil {
					var te *tripError
					if !errors.As(err, &te) {
						return err
					}
					skip(firstFieldError(te.body.Fields))
					continue
				}
				if tr.reassignTripID != "" {
					// An existing, fully-unassigned trip already occupies this
					// exact route+date+type+window — fill it in instead of
					// skipping it as a duplicate route conflict.
					if err := store.UpdateTripAssignment(ctx, q, schoolID, tr.reassignTripID, tr.input()); err != nil {
						return err
					}
					res.Created++
					continue
				}
				_, clash, err := insertTripIfFree(ctx, q, schoolID, tr.input(), "bulk assignment")
				if err != nil {
					return err
				}
				if clash != "" {
					skip(clash)
					continue
				}
				res.Created++
			}
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "trip.bulk_create", EntityType: "trip",
			After: map[string]any{"route_id": req.RouteID, "bus_id": req.BusID, "driver_id": req.DriverID,
				"trip_types": req.TripTypes, "dates": req.Dates, "created": res.Created, "skipped": len(res.Skipped)},
		})
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// maxCalendarDays bounds one calendar request (a month view shows up to 42 days).
const maxCalendarDays = 62

// TripCalendar counts trips per date and trip type between from and to (inclusive),
// for the Trip Planner month view.
func (a *API) TripCalendar(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	from, to := qs.Get("from"), qs.Get("to")
	v := validate.New()
	f, errF := time.Parse(time.DateOnly, from)
	t, errT := time.Parse(time.DateOnly, to)
	v.Check(errF == nil, "from", "must be a date YYYY-MM-DD")
	v.Check(errT == nil, "to", "must be a date YYYY-MM-DD")
	if errF == nil && errT == nil {
		v.Check(!t.Before(f), "to", "must not be before from")
		v.Check(t.Sub(f) < maxCalendarDays*24*time.Hour, "to", fmt.Sprintf("at most %d days per request", maxCalendarDays))
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	days, err := store.TripCalendar(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), from, to)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, days)
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
	isToday := date == ""
	if isToday {
		var err error
		if date, err = store.SchoolToday(ctx, a.Store.Pool, d.SchoolID); err != nil {
			internalError(w, r, err)
			return
		}
	} else if _, err := time.Parse(time.DateOnly, date); err != nil {
		httpx.ValidationError(w, validate.Errors{"date": "must be a date YYYY-MM-DD"})
		return
	}
	// Only for today's default (no ?date= override): materialize a trips row from any of the
	// driver's current permanent assignments that don't have one yet (store.EnsureDriverDailyTrips).
	// Browsing another date never speculatively creates a trip there.
	if isToday {
		if err := a.Store.InTx(ctx, func(q store.DBTX) error {
			return store.EnsureDriverDailyTrips(ctx, q, d.SchoolID, d.ID, date)
		}); err != nil {
			internalError(w, r, err)
			return
		}
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
		if t.TripType == models.TripSpecial {
			continue // outings have no assigned pickup/drop stop for this child
		}
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
