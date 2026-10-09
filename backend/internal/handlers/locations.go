package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

type locationsResult struct {
	Accepted       int                  `json:"accepted"`
	Rejected       []tracking.Rejection `json:"rejected"`
	LastRecordedAt *time.Time           `json:"last_recorded_at"`
}

// DriverPostLocations receives a batch of GPS fixes for the driver's own trip in
// progress (rule 7: GPS is shared only while a trip is active). Implausible fixes
// are dropped; the newest good one becomes the bus's live position.
func (a *API) DriverPostLocations(w http.ResponseWriter, r *http.Request) {
	d, ok := a.currentDriver(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	var req struct {
		Points []tracking.Point `json:"points"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if len(req.Points) == 0 || len(req.Points) > tracking.MaxPointsPerCall {
		httpx.ValidationError(w, validate.Errors{"points": "send 1 to 100 points"})
		return
	}

	ctx := r.Context()
	t, err := store.GetDriverTrip(ctx, a.Store.Pool, d.ID, id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if t.Status != models.TripStarted || t.StartedAt == nil {
		httpx.Error(w, http.StatusConflict, "trip_not_started",
			"This trip is "+t.Status+". Stop sending locations until a trip is started.")
		return
	}

	last, err := a.Live.Last(ctx, t.ID)
	if err != nil {
		liveUnavailable(w, r, err)
		return
	}
	var lastPoint *tracking.Point
	if last != nil {
		p := last.Point()
		lastPoint = &p
	}
	kept, rejected := tracking.Filter(req.Points, lastPoint, *t.StartedAt, time.Now())
	res := locationsResult{Accepted: len(kept), Rejected: rejected}
	if res.Rejected == nil {
		res.Rejected = []tracking.Rejection{}
	}
	if len(kept) == 0 {
		httpx.JSON(w, http.StatusOK, res)
		return
	}

	settings, err := store.GetTrackingSettings(ctx, a.Store.Pool, t.SchoolID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if settings.LocationRetentionDays > 0 {
		rows := make([]store.LocationRow, len(kept))
		for i, p := range kept {
			rows[i] = store.LocationRow{Latitude: p.Latitude, Longitude: p.Longitude, AccuracyM: p.AccuracyM,
				SpeedMPS: p.SpeedMPS, Heading: p.Heading, RecordedAt: p.RecordedAt}
		}
		if err := store.InsertLocations(ctx, a.Store.Pool, t.SchoolID, t.ID, rows); err != nil {
			internalError(w, r, err)
			return
		}
	}

	newest := kept[len(kept)-1]
	if err := a.Live.Update(ctx, tracking.BusLocation{
		TripID: t.ID, SchoolID: t.SchoolID, TripType: t.TripType, RouteID: t.Route.ID, RouteCode: t.Route.Code,
		BusNumber: t.Bus.VehicleNumber, DriverName: t.Driver.Name,
		Latitude: newest.Latitude, Longitude: newest.Longitude, AccuracyM: newest.AccuracyM,
		SpeedMPS: newest.SpeedMPS, Heading: newest.Heading, RecordedAt: newest.RecordedAt, ReceivedAt: time.Now().UTC(),
	}); err != nil {
		liveUnavailable(w, r, err)
		return
	}
	if err := a.processProgress(ctx, r, t, kept, settings); err != nil {
		liveUnavailable(w, r, err)
		return
	}
	res.LastRecordedAt = &newest.RecordedAt
	httpx.JSON(w, http.StatusOK, res)
}

// liveUnavailable: Redis is down. 503 tells the app to keep the points queued and retry.
func liveUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	internalErrorLog(r, err)
	httpx.Error(w, http.StatusServiceUnavailable, "live_unavailable", "Live tracking is temporarily unavailable. Keep the points and retry.")
}

// LiveSnapshot: every bus with a trip in progress at the school (admin live map).
func (a *API) LiveSnapshot(w http.ResponseWriter, r *http.Request) {
	buses, err := a.Live.Snapshot(r.Context(), chi.URLParam(r, "schoolID"))
	if err != nil {
		liveUnavailable(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, buses)
}

func (a *API) GetTrackingSettings(w http.ResponseWriter, r *http.Request) {
	s, err := store.GetTrackingSettings(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"))
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

func (a *API) UpdateTrackingSettings(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req store.TrackingSettings
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	v.Check(req.LocationRetentionDays >= 0 && req.LocationRetentionDays <= 365, "location_retention_days", "must be 0 to 365")
	v.Check(req.StaleAfterSeconds >= 30 && req.StaleAfterSeconds <= 1800, "stale_after_seconds", "must be 30 to 1800")
	v.Check(req.ApproachDistanceM >= 100 && req.ApproachDistanceM <= 5000, "approach_distance_m", "must be 100 to 5000")
	if req.DefaultGeofenceM == 0 {
		req.DefaultGeofenceM = defaultGeofenceM
	}
	v.Check(req.DefaultGeofenceM >= minGeofenceM && req.DefaultGeofenceM <= maxGeofenceM, "default_geofence_m", "must be 25 to 1000")
	v.Check((req.SchoolLatitude == nil) == (req.SchoolLongitude == nil), "school_latitude", "give both latitude and longitude, or neither")
	if req.SchoolLatitude != nil && req.SchoolLongitude != nil {
		v.Check(*req.SchoolLatitude >= -90 && *req.SchoolLatitude <= 90 && *req.SchoolLongitude >= -180 && *req.SchoolLongitude <= 180 &&
			(*req.SchoolLatitude != 0 || *req.SchoolLongitude != 0), "school_latitude", "pick the school's location on the map")
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	ctx := r.Context()
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetTrackingSettings(ctx, q, schoolID)
		if err != nil {
			return err
		}
		if err := store.SetTrackingSettings(ctx, q, schoolID, req); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "settings.tracking_update", EntityType: "school", EntityID: &schoolID,
			Before: before, After: req,
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, req)
}
