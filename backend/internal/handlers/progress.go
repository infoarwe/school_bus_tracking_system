package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

// ETAProvider computes ETAs with the school's Google key (tracking.GoogleRoutes).
type ETAProvider interface {
	ETAs(ctx context.Context, apiKey string, from tracking.Point, pending []tracking.StopProgress, now time.Time) (*tracking.GoogleETA, error)
}

// tripStopsInOrder returns the stops in the order the bus visits them:
// Morning Pickup runs stop 1 → N and then to the school; Evening Drop leaves
// the school and runs N → 1 (the reverse of the pickup order).
func tripStopsInOrder(t *models.Trip, stops []models.Stop, s *store.TrackingSettings) []tracking.StopInput {
	morning := t.TripType == models.TripMorningPickup
	out := make([]tracking.StopInput, 0, len(stops)+1)
	for _, st := range stops {
		sched := st.DropTime
		if morning {
			sched = st.PickupTime
		}
		out = append(out, tracking.StopInput{ID: st.ID, Sequence: st.Sequence, Name: st.Name,
			Latitude: st.Latitude, Longitude: st.Longitude, RadiusM: float64(st.GeofenceRadiusM), ScheduledTime: sched})
	}
	if !morning {
		slices.Reverse(out)
	} else if s.SchoolLatitude != nil && s.SchoolLongitude != nil {
		out = append(out, tracking.StopInput{ID: tracking.SchoolStopID, Name: "School",
			Latitude: *s.SchoolLatitude, Longitude: *s.SchoolLongitude, RadiusM: tracking.SchoolRadiusM})
	}
	return out
}

// processProgress runs stop detection and ETA for newly accepted fixes, stores
// the stop events, and pushes stop_status and progress events to subscribers.
func (a *API) processProgress(ctx context.Context, r *http.Request, t *models.Trip, kept []tracking.Point,
	settings *store.TrackingSettings) error {
	p, err := a.Live.Progress(ctx, t.ID)
	if err != nil {
		return err
	}
	if p == nil {
		stops, err := store.ListStops(ctx, a.Store.Pool, t.Route.ID)
		if err != nil {
			return err
		}
		p = tracking.NewProgress(t.ID, t.TripType, tripStopsInOrder(t, stops, settings))
	}

	cfg := tracking.DefaultGeofence(float64(settings.ApproachDistanceM))
	var events []tracking.StopEvent
	for _, pt := range kept {
		events = append(events, p.Apply(pt, cfg)...)
	}
	now := time.Now()
	g, err := a.Live.GoogleETA(ctx, t.ID)
	if err != nil {
		return err
	}
	p.UpdateETAs(now, g)
	p.UpdatedAt = now.UTC()

	for _, e := range events {
		stopID := e.StopID
		if stopID == tracking.SchoolStopID {
			stopID = ""
		}
		if err := store.InsertStopEvent(ctx, a.Store.Pool, t.SchoolID, t.ID, stopID, e.Type, e.Missed,
			e.At, e.Latitude, e.Longitude); err != nil {
			return err
		}
	}
	if err := a.Live.SaveProgress(ctx, t.SchoolID, p, events); err != nil {
		return err
	}
	if p.NeedsGoogleRefresh(now, g) {
		a.refreshGoogleETA(t, p)
	}
	return nil
}

// refreshGoogleETA asks Google for fresh ETAs in the background, at most one call
// per trip at a time and only if the school has a server key. The next location
// update uses the result; until then the estimate is shown.
func (a *API) refreshGoogleETA(t *models.Trip, p *tracking.TripProgress) {
	if a.ETA == nil || p.LastPoint == nil {
		return
	}
	from, pending := *p.LastPoint, p.Pending()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m, err := store.GetMapsSettings(ctx, a.Store.Pool, t.SchoolID)
		if err != nil || m.ServerKeyEncrypted == nil {
			return // no server key: the estimate is used
		}
		key, err := a.Secrets.Decrypt(m.ServerKeyEncrypted)
		if err != nil {
			slog.Error("decrypt maps server key", "school_id", t.SchoolID, "err", err)
			return
		}
		// One Google call per trip at a time, across API instances.
		lock := "google_eta:" + t.ID
		if ok, err := a.Live.TryLock(ctx, lock, 30*time.Second); err != nil || !ok {
			return
		}
		defer a.Live.Unlock(context.Background(), lock)
		g, err := a.ETA.ETAs(ctx, key, from, pending, time.Now())
		if err != nil {
			slog.Warn("google ETA failed; using estimate", "trip_id", t.ID, "err", err)
			return
		}
		if err := a.Live.SaveGoogleETA(ctx, t.ID, g); err != nil {
			slog.Error("save google eta", "err", err)
		}
	}()
}

type tripProgressResponse struct {
	// Live stop statuses and ETAs while the trip is in progress (null otherwise).
	Progress *tracking.TripProgress `json:"progress"`
	// Latest bus position (null if none yet / trip not in progress).
	Location *tracking.BusLocation `json:"location"`
	// Stored stop events, for the timeline and for finished trips.
	Events []store.StopEventRow `json:"events"`
}

func (a *API) tripProgress(ctx context.Context, tripID string) (*tripProgressResponse, error) {
	res := &tripProgressResponse{}
	p, err := a.Live.Progress(ctx, tripID)
	if err != nil {
		return nil, err
	}
	if p != nil {
		res.Progress = p.View()
	}
	if res.Location, err = a.Live.Last(ctx, tripID); err != nil {
		return nil, err
	}
	if res.Events, err = store.TripStopEvents(ctx, a.Store.Pool, tripID); err != nil {
		return nil, err
	}
	return res, nil
}

// TripProgress (admin): stop-by-stop status, ETAs and the event timeline.
func (a *API) TripProgress(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	if _, err := store.GetTrip(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id); err != nil {
		storeError(w, r, err, nil)
		return
	}
	res, err := a.tripProgress(r.Context(), id)
	if err != nil {
		liveUnavailable(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// DriverTripProgress: the same for the driver's own trip (next stop, ETAs).
func (a *API) DriverTripProgress(w http.ResponseWriter, r *http.Request) {
	d, ok := a.currentDriver(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	if _, err := store.GetDriverTrip(r.Context(), a.Store.Pool, d.ID, id); err != nil {
		storeError(w, r, err, nil)
		return
	}
	res, err := a.tripProgress(r.Context(), id)
	if err != nil {
		liveUnavailable(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// parentBus is what a parent sees of the bus: no driver mobile, no other children.
type parentBus struct {
	BusNumber string    `json:"bus_number"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	SpeedMPS  *float64  `json:"speed_mps"`
	Heading   *float64  `json:"heading"`
	State     string    `json:"state"` // moving | stopped | offline
	LastSeen  time.Time `json:"last_seen"`
}

type parentChildStop struct {
	StopID        string     `json:"stop_id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"` // upcoming | approaching | reached | crossed
	Missed        bool       `json:"missed"`
	ScheduledTime *string    `json:"scheduled_time"`
	ETASeconds    *int       `json:"eta_seconds"` // from the bus's current position (rule 9)
	DistanceM     *int       `json:"distance_m"`
	ReachedAt     *time.Time `json:"reached_at"`
	CrossedAt     *time.Time `json:"crossed_at"`
}

type parentLive struct {
	// The child's most relevant trip today: the one in progress, else the next
	// one, else the last one. Null if the child has no trip today.
	Trip      *parentTrip      `json:"trip"`
	Bus       *parentBus       `json:"bus"`        // null until the bus sends GPS
	ChildStop *parentChildStop `json:"child_stop"` // the child's stop for this trip
	ETASource string           `json:"eta_source"` // google | estimate
}

// BusState matches the admin live map: offline when stale, moving above ~4 km/h.
func busState(l *tracking.BusLocation) string {
	switch {
	case l.Stale:
		return "offline"
	case l.SpeedMPS != nil && *l.SpeedMPS > 1:
		return "moving"
	}
	return "stopped"
}

// ParentChildLive: one call for the Parent app's tracking screen: the child's
// trip, where the bus is, and the status and ETA of the child's own stop.
func (a *API) ParentChildLive(w http.ResponseWriter, r *http.Request) {
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
	idx := slices.IndexFunc(children, func(c store.ParentChild) bool { return c.StudentID == id })
	if idx < 0 {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	child := children[idx]
	res := parentLive{}
	if child.Assignment == nil {
		httpx.JSON(w, http.StatusOK, res)
		return
	}
	today, err := store.SchoolToday(ctx, a.Store.Pool, *p.SchoolID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	trips, err := store.RouteTripsOn(ctx, a.Store.Pool, child.Assignment.RouteID, today)
	if err != nil {
		internalError(w, r, err)
		return
	}
	t, stop := pickChildTrip(trips, child.Assignment)
	if t == nil {
		httpx.JSON(w, http.StatusOK, res)
		return
	}
	res.Trip = &parentTrip{ID: t.ID, TripDate: t.TripDate, TripType: t.TripType, Status: t.Status,
		BusNumber: t.Bus.VehicleNumber, DriverName: t.Driver.Name, StartedAt: t.StartedAt, EndedAt: t.EndedAt,
		CancelReason: t.CancelReason, ChildStopName: stop.Name}
	res.ChildStop = &parentChildStop{StopID: stop.ID, Name: stop.Name, Status: tracking.StopUpcoming}
	if t.TripType == models.TripMorningPickup {
		res.ChildStop.ScheduledTime = stop.PickupTime
	} else {
		res.ChildStop.ScheduledTime = stop.DropTime
	}

	if t.Status == models.TripStarted {
		loc, err := a.Live.Last(ctx, t.ID)
		if err != nil {
			liveUnavailable(w, r, err)
			return
		}
		if loc != nil {
			res.Bus = &parentBus{BusNumber: loc.BusNumber, Latitude: loc.Latitude, Longitude: loc.Longitude,
				SpeedMPS: loc.SpeedMPS, Heading: loc.Heading, State: busState(loc), LastSeen: loc.ReceivedAt}
		}
	}
	// Stop status: live while in progress, else from the stored events.
	if prog, err := a.Live.Progress(ctx, t.ID); err == nil && prog != nil {
		if s := prog.Stop(stop.ID); s != nil {
			res.ChildStop.Status, res.ChildStop.Missed = s.Status, s.Missed
			res.ChildStop.ETASeconds, res.ChildStop.DistanceM = s.ETASeconds, s.DistanceM
			res.ChildStop.ReachedAt, res.ChildStop.CrossedAt = s.ReachedAt, s.CrossedAt
			res.ETASource = prog.ETASource
		}
	} else if err != nil {
		liveUnavailable(w, r, err)
		return
	} else {
		events, err := store.TripStopEvents(ctx, a.Store.Pool, t.ID)
		if err != nil {
			internalError(w, r, err)
			return
		}
		for _, e := range events {
			if e.StopID == nil || *e.StopID != stop.ID {
				continue
			}
			switch e.EventType {
			case tracking.EventApproaching:
				res.ChildStop.Status = tracking.StopApproaching
			case tracking.EventReached:
				res.ChildStop.Status, res.ChildStop.ReachedAt = tracking.StopReached, &e.OccurredAt
			case tracking.EventCrossed:
				res.ChildStop.Status, res.ChildStop.CrossedAt, res.ChildStop.Missed = tracking.StopCrossed, &e.OccurredAt, e.Missed
			}
		}
	}
	httpx.JSON(w, http.StatusOK, res)
}

// pickChildTrip chooses the child's most relevant trip today and their stop on it.
func pickChildTrip(trips []models.Trip, as *models.StudentAssignment) (*models.Trip, *models.StopRef) {
	stopFor := func(t *models.Trip) *models.StopRef {
		if t.TripType == models.TripMorningPickup {
			return as.PickupStop
		}
		return as.DropStop
	}
	rank := map[string]int{models.TripStarted: 0, models.TripConfirmed: 1, models.TripScheduled: 1, models.TripCompleted: 2, models.TripCancelled: 3}
	var best *models.Trip
	for i := range trips {
		t := &trips[i]
		if stopFor(t) == nil {
			continue // the child does not ride this trip type
		}
		switch {
		case best == nil, rank[t.Status] < rank[best.Status]:
			best = t
		case rank[t.Status] == rank[best.Status] && t.Status == models.TripCompleted && t.TripType == models.TripEveningDrop:
			best = t // after school, the evening trip is the latest one
		}
	}
	if best == nil {
		return nil, nil
	}
	return best, stopFor(best)
}
