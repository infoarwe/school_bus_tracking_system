package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// Driver self-assignment: a driver takes an available route (and a free bus) for a
// date and trip type in the Driver app; no approval step. The claim is an ordinary
// trip (created by the driver, already confirmed), so tracking, stop detection,
// parent notifications, alerts and reports work exactly as for staff-assigned trips.
//
// Policy (CLAUDE.md, "Driver self-assignment"):
//   - any active driver may take any active route of their school that has stops and
//     runs the trip type, from today to claimDaysAhead days ahead (school time zone);
//   - the bus must be an active bus of the school not used by another trip in the slot;
//   - a driver may release their own claim before starting it; staff-assigned trips
//     are changed only by staff;
//   - School Admins and Transport Managers get an in-app notification for every claim
//     and release, written in the same transaction as the trip.

const claimDaysAhead = 6

// claimableTripTypes: a driver may self-assign Morning Pickup or Evening Drop
// only. Special Trips/Outings are admin/staff-created only (they have no
// route-slot browsing concept — RouteSlots is entirely route-keyed), so they
// are deliberately kept out of tripTypes' superset here.
var claimableTripTypes = []string{models.TripMorningPickup, models.TripEveningDrop}

// Claim conflicts, by unique index. The insert itself is the lock: of two
// simultaneous claims for the same route or bus, exactly one succeeds.
var claimConflicts = map[string]httpx.ErrorBody{
	"trips_route_slot_key": {Code: "route_taken", Message: "Another driver has just taken this route. Choose another route.",
		Fields: map[string]string{"route_id": "already taken"}},
	"trips_bus_slot_key": {Code: "bus_taken", Message: "This bus is already on another trip at this time. Choose another bus.",
		Fields: map[string]string{"bus_id": "already in use"}},
	"trips_driver_slot_key": {Code: "already_assigned", Message: "You already have a route for this trip. Release it first to take another."},
}

func writeClaimError(w http.ResponseWriter, r *http.Request, err error) {
	var ce *store.ConflictError
	if errors.As(err, &ce) {
		if body, ok := claimConflicts[ce.Constraint]; ok {
			httpx.ErrorWithFields(w, http.StatusConflict, body)
			return
		}
	}
	writeTripError(w, r, err)
}

// claimWindow returns today and the last date a driver may claim (school time zone).
func claimWindow(ctx context.Context, q store.DBTX, schoolID string) (today, last string, err error) {
	if today, err = store.SchoolToday(ctx, q, schoolID); err != nil {
		return "", "", err
	}
	t, _ := time.Parse(time.DateOnly, today)
	return today, t.AddDate(0, 0, claimDaysAhead).Format(time.DateOnly), nil
}

func checkClaimSlot(v *validate.V, date, tripType, today, last string) {
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		v.Check(false, "trip_date", "must be a date YYYY-MM-DD")
	} else {
		v.Check(date >= today, "trip_date", "cannot be in the past")
		v.Check(date <= last, "trip_date", fmt.Sprintf("can be at most %d days ahead (until %s)", claimDaysAhead, last))
	}
	v.OneOf("trip_type", tripType, claimableTripTypes...)
}

// --- GET /driver/route-slots -------------------------------------------------------

type routeSlotView struct {
	Route struct {
		ID         string `json:"id"`
		Code       string `json:"code"`
		Name       string `json:"name"`
		StartPoint string `json:"start_point"`
		StopCount  int    `json:"stop_count"`
	} `json:"route"`
	// Students on this route with a stop for this trip type (a count only: rule 7).
	StudentCount int `json:"student_count"`
	// available: nobody has it · taken: another trip has it · mine: the caller's trip.
	Availability string           `json:"availability"`
	Stops        []store.SlotStop `json:"stops"`
}

type routeSlotsResponse struct {
	TripDate string `json:"trip_date"`
	TripType string `json:"trip_type"`
	// The dates a driver may claim: today … last_date (school time zone).
	Today    string `json:"today"`
	LastDate string `json:"last_date"`
	// The caller's trip in this slot (any route), or null.
	MyTrip *driverTrip     `json:"my_trip"`
	Routes []routeSlotView `json:"routes"`
	Buses  []store.SlotBus `json:"buses"`
}

// DriverRouteSlots lists the routes the driver can take for ?date= (default today)
// and ?trip_type= (default morning_pickup), with their stops and the free buses.
func (a *API) DriverRouteSlots(w http.ResponseWriter, r *http.Request) {
	d, ok := a.currentDriver(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	today, last, err := claimWindow(ctx, a.Store.Pool, d.SchoolID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	date, tripType := r.URL.Query().Get("date"), r.URL.Query().Get("trip_type")
	if date == "" {
		date = today
	}
	if tripType == "" {
		tripType = models.TripMorningPickup
	}
	v := validate.New()
	checkClaimSlot(v, date, tripType, today, last)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}

	slots, err := store.RouteSlots(ctx, a.Store.Pool, d.SchoolID, date, tripType)
	if err != nil {
		internalError(w, r, err)
		return
	}
	ids := make([]string, len(slots))
	for i, s := range slots {
		ids[i] = s.RouteID
	}
	stops, err := store.SlotStops(ctx, a.Store.Pool, ids, tripType)
	if err != nil {
		internalError(w, r, err)
		return
	}
	buses, err := store.SlotBuses(ctx, a.Store.Pool, d.SchoolID, date, tripType)
	if err != nil {
		internalError(w, r, err)
		return
	}
	res := routeSlotsResponse{TripDate: date, TripType: tripType, Today: today, LastDate: last,
		Routes: make([]routeSlotView, 0, len(slots)), Buses: buses}

	mine, err := store.DriverTripsOn(ctx, a.Store.Pool, d.ID, date)
	if err != nil {
		internalError(w, r, err)
		return
	}
	for i := range mine {
		if mine[i].TripType == tripType {
			res.MyTrip = &driverTrip{Trip: &mine[i]}
			if res.MyTrip.Stops, err = a.tripStops(ctx, &mine[i]); err != nil {
				internalError(w, r, err)
				return
			}
		}
	}
	for i := range res.Buses {
		// The caller's own bus counts as theirs, not as taken.
		if b := &res.Buses[i]; res.MyTrip != nil && b.TripID != nil && *b.TripID == res.MyTrip.ID {
			b.Available = true
		}
	}
	for _, s := range slots {
		var view routeSlotView
		view.Route.ID, view.Route.Code, view.Route.Name = s.RouteID, s.Code, s.Name
		view.Route.StartPoint, view.Route.StopCount = s.StartPoint, s.StopCount
		view.StudentCount = s.StudentCount
		view.Stops = stops[s.RouteID]
		if view.Stops == nil {
			view.Stops = []store.SlotStop{}
		}
		switch {
		case s.TripID == nil:
			view.Availability = "available"
		case s.DriverID != nil && *s.DriverID == d.ID:
			view.Availability = "mine"
		default:
			view.Availability = "taken" // never says who: drivers see only their own trips
		}
		res.Routes = append(res.Routes, view)
	}
	httpx.JSON(w, http.StatusOK, res)
}

// --- POST /driver/route-slots/claim ------------------------------------------------------

// DriverClaimRoute: the driver takes a route and a bus for a date and trip type. The
// trip is created already confirmed. Repeating the same request returns the same trip
// (200) instead of creating another, so a retry after a lost response is safe.
func (a *API) DriverClaimRoute(w http.ResponseWriter, r *http.Request) {
	d, ok := a.currentDriver(w, r)
	if !ok {
		return
	}
	var req struct {
		TripDate string `json:"trip_date"`
		TripType string `json:"trip_type"`
		RouteID  string `json:"route_id"`
		BusID    string `json:"bus_id"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	var (
		t        *models.Trip
		replay   bool
		notified *store.NotificationInput
	)
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		today, last, err := claimWindow(ctx, q, d.SchoolID)
		if err != nil {
			return err
		}
		v := validate.New()
		checkClaimSlot(v, req.TripDate, req.TripType, today, last)
		if !v.OK() {
			return tripValidation(v.Errors())
		}

		// Idempotency and "one route per slot" for this driver.
		mine, err := store.DriverTripsOn(ctx, q, d.ID, req.TripDate)
		if err != nil {
			return err
		}
		for i := range mine {
			if mine[i].TripType != req.TripType {
				continue
			}
			if mine[i].Route.ID == req.RouteID && mine[i].Bus.ID == req.BusID {
				t, replay = &mine[i], true
				return nil
			}
			return &tripError{http.StatusConflict, httpx.ErrorBody{Code: "already_assigned",
				Message: fmt.Sprintf("You already have %s for this trip. Release it first to take another.", mine[i].Route.Code)}}
		}

		tr := tripRequest{TripDate: req.TripDate, TripType: req.TripType, RouteID: req.RouteID,
			BusID: req.BusID, DriverID: d.ID}
		if err := validateTrip(ctx, q, d.SchoolID, &tr); err != nil {
			return err
		}
		id, err := store.CreateTrip(ctx, q, d.SchoolID, p.UserID, tr.input())
		if err != nil {
			return err
		}
		if err := store.InsertTripHistory(ctx, q, store.TripHistoryEntry{TripID: id, SchoolID: d.SchoolID,
			To: models.TripScheduled, ByUserID: &p.UserID, ByRole: string(p.Role), Reason: "Taken by the driver in the Driver app"}); err != nil {
			return err
		}
		if t, err = store.GetTrip(ctx, q, d.SchoolID, id); err != nil {
			return err
		}
		// Taking a route means "I will drive it": confirmed straight away.
		if err := a.changeTripStatus(ctx, q, r, t, models.TripConfirmed, "Taken by the driver", "trip.confirm"); err != nil {
			return err
		}
		if t, err = store.GetTrip(ctx, q, d.SchoolID, id); err != nil {
			return err
		}
		if err := a.audit(ctx, q, r, store.AuditEntry{SchoolID: &d.SchoolID, Action: "trip.claim",
			EntityType: "trip", EntityID: &id, After: t}); err != nil {
			return err
		}
		// Notify staff in the same transaction: no notification unless the claim is saved.
		n := claimNotification(t, "claimed")
		staff, err := store.SchoolStaff(ctx, q, d.SchoolID)
		if err != nil {
			return err
		}
		if _, _, err := notify.Notify(ctx, q, n, staff); err != nil {
			return err
		}
		notified = &n
		return nil
	})
	if err != nil {
		writeClaimError(w, r, err)
		return
	}
	if !replay {
		a.afterAssignmentChange(r, t, notified)
	}
	res := driverTrip{Trip: t}
	if res.Stops, err = a.tripStops(ctx, t); err != nil {
		internalError(w, r, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	httpx.JSON(w, status, res)
}

// --- POST /driver/trips/{tripID}/release --------------------------------------------------

// DriverReleaseTrip gives back a route the driver took: the trip is cancelled (history
// kept) and the route is free for another driver or for staff to assign.
func (a *API) DriverReleaseTrip(w http.ResponseWriter, r *http.Request) {
	d, ok := a.currentDriver(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 && !httpx.Decode(w, r, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.Reason) > 300 {
		httpx.ValidationError(w, validate.Errors{"reason": "must be at most 300 characters"})
		return
	}
	ctx := r.Context()
	var (
		t        *models.Trip
		notified *store.NotificationInput
	)
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		var err error
		if t, err = store.GetDriverTrip(ctx, q, d.ID, id); err != nil {
			return err
		}
		if t.AssignedBy != models.AssignedByDriver {
			return &tripError{http.StatusConflict, httpx.ErrorBody{Code: "assigned_by_school",
				Message: "The school assigned you this trip. Ask the school to change it."}}
		}
		if t.Status != models.TripScheduled && t.Status != models.TripConfirmed {
			return invalidTransition(t, "released")
		}
		today, err := store.SchoolToday(ctx, q, d.SchoolID)
		if err != nil {
			return err
		}
		if t.TripDate < today {
			return &tripError{http.StatusConflict, httpx.ErrorBody{Code: "invalid_transition",
				Message: "This trip's date has passed."}}
		}
		reason := "Released by the driver"
		if req.Reason != "" {
			reason += ": " + req.Reason
		}
		if err := a.changeTripStatus(ctx, q, r, t, models.TripCancelled, reason, "trip.release"); err != nil {
			return err
		}
		if t, err = store.GetDriverTrip(ctx, q, d.ID, id); err != nil {
			return err
		}
		n := claimNotification(t, "released")
		staff, err := store.SchoolStaff(ctx, q, d.SchoolID)
		if err != nil {
			return err
		}
		if _, _, err := notify.Notify(ctx, q, n, staff); err != nil {
			return err
		}
		notified = &n
		return nil
	})
	if err != nil {
		writeTripError(w, r, err)
		return
	}
	// Parents are not told: the route still runs, staff will find a driver.
	a.afterAssignmentChange(r, t, notified)
	res := driverTrip{Trip: t}
	if res.Stops, err = a.tripStops(ctx, t); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// claimNotification is the staff inbox item for a claim or release. The dedupe key is
// per trip, so a retried request never notifies twice.
func claimNotification(t *models.Trip, what string) store.NotificationInput {
	date, _ := time.Parse(time.DateOnly, t.TripDate)
	when := date.Format("Mon 2 Jan")
	tripType := tripTypeNames[t.TripType]
	title, body := "", ""
	switch what {
	case "claimed":
		title = fmt.Sprintf("Route taken: %s · %s", t.Route.Code, tripType)
		body = fmt.Sprintf("%s took %s (%s) for %s on %s with bus %s.",
			t.Driver.Name, t.Route.Code, t.Route.Name, tripType, when, t.Bus.VehicleNumber)
	default:
		title = fmt.Sprintf("Needs a driver: %s · %s", t.Route.Code, tripType)
		body = fmt.Sprintf("%s released %s (%s) for %s on %s. Assign another driver or wait for one to take it.",
			t.Driver.Name, t.Route.Code, t.Route.Name, tripType, when)
	}
	key := "trip:" + t.ID + ":" + what
	return store.NotificationInput{
		SchoolID: t.SchoolID, Type: "route_" + what, Title: title, Body: body,
		Data: map[string]string{"trip_id": t.ID, "route_id": t.Route.ID, "route_code": t.Route.Code,
			"driver_name": t.Driver.Name, "bus_number": t.Bus.VehicleNumber, "trip_date": t.TripDate,
			"trip_type": t.TripType, "at": time.Now().UTC().Format(time.RFC3339)},
		TripID: &t.ID, RouteID: &t.Route.ID, DedupeKey: &key,
		// Staff (School Admin / Transport Manager) have no topic subscription
		// model — this must never fall through to the parent-facing route
		// topic, which would leak staff-ops messages to parents on that route.
		StaffOnly: true,
	}
}

var tripTypeNames = map[string]string{
	models.TripMorningPickup: "Morning Pickup",
	models.TripEveningDrop:   "Evening Drop",
}

// afterAssignmentChange tells live views (after the commit): the trip's status for
// trip/school subscribers and the staff inbox item for the admin web.
func (a *API) afterAssignmentChange(r *http.Request, t *models.Trip, n *store.NotificationInput) {
	if err := a.Live.TripStatusChanged(r.Context(), t.SchoolID, t.ID, t.Status); err != nil {
		internalErrorLog(r, err)
	}
	if n == nil {
		return
	}
	if err := a.Live.PublishStaffNotification(r.Context(), t.SchoolID, t.ID, map[string]any{
		"type": n.Type, "title": n.Title, "body": n.Body, "data": n.Data,
	}); err != nil {
		internalErrorLog(r, err)
	}
}

// --- Staff inbox -------------------------------------------------------------------------

// StaffNotifications is the caller's own inbox (route claims and releases), newest first;
// meta.unread is the unread count. Mark items read with POST /staff/notifications/read.
func (a *API) StaffNotifications(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	pg := page(r)
	items, total, err := store.Inbox(r.Context(), a.Store.Pool, p.UserID, "", pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	unread, err := store.UnreadCount(r.Context(), a.Store.Pool, p.UserID, "")
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSONWithMeta(w, items, map[string]any{"page": pg.Page, "page_size": pg.PageSize, "total": total, "unread": unread})
}
