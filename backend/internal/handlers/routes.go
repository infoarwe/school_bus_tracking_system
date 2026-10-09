package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

const (
	defaultGeofenceM = 100
	minGeofenceM     = 25
	maxGeofenceM     = 1000
)

var (
	routeCodeRe    = regexp.MustCompile(`^[A-Z0-9-]{1,20}$`)
	clockTimeRe    = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	routeConflicts = map[string]string{"routes_school_code_key": "code"}
)

type routeRequest struct {
	Name           string `json:"name"`
	Code           string `json:"code"`
	StartPoint     string `json:"start_point"`
	Description    string `json:"description"`
	SupportsPickup *bool  `json:"supports_pickup"`
	SupportsDrop   *bool  `json:"supports_drop"`
}

func (req *routeRequest) toInput() (store.RouteInput, validate.Errors) {
	in := store.RouteInput{
		Name:        strings.TrimSpace(req.Name),
		Code:        strings.ToUpper(strings.TrimSpace(req.Code)),
		StartPoint:  strings.TrimSpace(req.StartPoint),
		Description: req.Description,
		// Both trip types unless told otherwise.
		SupportsPickup: req.SupportsPickup == nil || *req.SupportsPickup,
		SupportsDrop:   req.SupportsDrop == nil || *req.SupportsDrop,
	}
	v := validate.New()
	v.Required("name", in.Name)
	v.MaxLen("name", in.Name, 200)
	v.Check(routeCodeRe.MatchString(in.Code), "code", "must be 1-20 letters, digits or hyphens, e.g. RS-01")
	v.MaxLen("start_point", in.StartPoint, 200)
	v.MaxLen("description", in.Description, 1000)
	v.Check(in.SupportsPickup || in.SupportsDrop, "supports_pickup", "a route must support Morning Pickup, Evening Drop, or both")
	return in, v.Errors()
}

func (a *API) ListRoutes(w http.ResponseWriter, r *http.Request) {
	pg := page(r)
	routes, total, err := store.ListRoutes(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), listFilter(r), pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.List(w, routes, meta(pg, total))
}

// GetRoute returns the route with its stops in order.
func (a *API) GetRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "routeID")
	if !ok {
		return
	}
	route, err := store.GetRoute(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if route.Stops, err = store.ListStops(r.Context(), a.Store.Pool, id); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, route)
}

func (a *API) CreateRoute(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req routeRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	var route *models.Route
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		var err error
		if route, err = store.CreateRoute(r.Context(), q, schoolID, in); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "route.create", EntityType: "route", EntityID: &route.ID, After: route,
		})
	})
	if err != nil {
		storeError(w, r, err, routeConflicts)
		return
	}
	route.Stops = []models.Stop{}
	httpx.JSON(w, http.StatusCreated, route)
}

func (a *API) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "routeID")
	if !ok {
		return
	}
	var req routeRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	var after *models.Route
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		before, err := store.GetRoute(r.Context(), q, schoolID, id)
		if err != nil {
			return err
		}
		if after, err = store.UpdateRoute(r.Context(), q, schoolID, id, in); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "route.update", EntityType: "route", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		storeError(w, r, err, routeConflicts)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

func (a *API) SetRouteStatus(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "routeID")
	if !ok {
		return
	}
	status, ok := decodeStatus(w, r, models.StatusActive, models.StatusInactive)
	if !ok {
		return
	}
	var after *models.Route
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		before, err := store.GetRoute(r.Context(), q, schoolID, id)
		if err != nil {
			return err
		}
		if after, err = store.SetRouteStatus(r.Context(), q, schoolID, id, status); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "route.status_change", EntityType: "route", EntityID: &id,
			Before: map[string]string{"status": before.Status}, After: map[string]string{"status": status},
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

// Stops

type stopRequest struct {
	Name            string   `json:"name"`
	Landmark        string   `json:"landmark"`
	Latitude        *float64 `json:"latitude"`
	Longitude       *float64 `json:"longitude"`
	PickupTime      *string  `json:"pickup_time"`
	DropTime        *string  `json:"drop_time"`
	GeofenceRadiusM *int     `json:"geofence_radius_m"`
	// Position (1-based) to insert at on create; omitted appends to the end.
	Position *int `json:"position"`
}

func (req *stopRequest) toInput() (store.StopInput, validate.Errors) {
	emptyToNil := func(s *string) *string {
		if s == nil || strings.TrimSpace(*s) == "" {
			return nil
		}
		t := strings.TrimSpace(*s)
		return &t
	}
	in := store.StopInput{
		Name:            strings.TrimSpace(req.Name),
		Landmark:        strings.TrimSpace(req.Landmark),
		PickupTime:      emptyToNil(req.PickupTime),
		DropTime:        emptyToNil(req.DropTime),
		GeofenceRadiusM: defaultGeofenceM,
	}
	if req.GeofenceRadiusM != nil {
		in.GeofenceRadiusM = *req.GeofenceRadiusM
	}
	v := validate.New()
	v.Required("name", in.Name)
	v.MaxLen("name", in.Name, 200)
	v.MaxLen("landmark", in.Landmark, 200)
	v.Check(req.Latitude != nil && *req.Latitude >= -90 && *req.Latitude <= 90, "latitude", "is required, between -90 and 90")
	v.Check(req.Longitude != nil && *req.Longitude >= -180 && *req.Longitude <= 180, "longitude", "is required, between -180 and 180")
	if req.Latitude != nil && req.Longitude != nil {
		in.Latitude, in.Longitude = *req.Latitude, *req.Longitude
		v.Check(in.Latitude != 0 || in.Longitude != 0, "latitude", "pick the stop's location on the map")
	}
	v.Check(in.PickupTime == nil || clockTimeRe.MatchString(*in.PickupTime), "pickup_time", "must be HH:MM (24-hour)")
	v.Check(in.DropTime == nil || clockTimeRe.MatchString(*in.DropTime), "drop_time", "must be HH:MM (24-hour)")
	v.Check(in.GeofenceRadiusM >= minGeofenceM && in.GeofenceRadiusM <= maxGeofenceM, "geofence_radius_m", "must be between 25 and 1000 metres")
	return in, v.Errors()
}

func (a *API) CreateStop(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	routeID, ok := idParam(w, r, "routeID")
	if !ok {
		return
	}
	var req stopRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.GeofenceRadiusM == nil {
		s, err := store.GetTrackingSettings(r.Context(), a.Store.Pool, schoolID)
		if err != nil {
			internalError(w, r, err)
			return
		}
		req.GeofenceRadiusM = &s.DefaultGeofenceM
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	ctx := r.Context()
	var stop *models.Stop
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		if err := store.LockRoute(ctx, q, schoolID, routeID); err != nil {
			return err
		}
		var err error
		if stop, err = store.CreateStop(ctx, q, schoolID, routeID, req.Position, in); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "stop.create", EntityType: "stop", EntityID: &stop.ID, After: stop,
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusCreated, stop)
}

func (a *API) UpdateStop(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	routeID, ok := idParam(w, r, "routeID")
	if !ok {
		return
	}
	id, ok := idParam(w, r, "stopID")
	if !ok {
		return
	}
	var req stopRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	ctx := r.Context()
	var after *models.Stop
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetStop(ctx, q, schoolID, routeID, id)
		if err != nil {
			return err
		}
		if after, err = store.UpdateStop(ctx, q, schoolID, routeID, id, in); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "stop.update", EntityType: "stop", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

func (a *API) DeleteStop(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	routeID, ok := idParam(w, r, "routeID")
	if !ok {
		return
	}
	id, ok := idParam(w, r, "stopID")
	if !ok {
		return
	}
	ctx := r.Context()
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		if err := store.LockRoute(ctx, q, schoolID, routeID); err != nil {
			return err
		}
		before, err := store.GetStop(ctx, q, schoolID, routeID, id)
		if err != nil {
			return err
		}
		inUse, err := store.CountStopAssignments(ctx, q, id)
		if err != nil {
			return err
		}
		if inUse > 0 {
			return &stopInUseError{students: inUse}
		}
		if err := store.DeleteStop(ctx, q, schoolID, routeID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "stop.delete", EntityType: "stop", EntityID: &id, Before: before,
		})
	})
	var inUse *stopInUseError
	if errors.As(err, &inUse) {
		httpx.Error(w, http.StatusConflict, "stop_in_use", fmt.Sprintf(
			"%d student(s) are assigned to this stop. Move them to another stop first.", inUse.students))
		return
	}
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.NoContent(w)
}

// stopInUseError aborts a stop deletion while students are assigned to it.
type stopInUseError struct{ students int }

func (e *stopInUseError) Error() string { return "stop in use" }

// ReorderStops takes every stop ID of the route in the new travel order.
func (a *API) ReorderStops(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	routeID, ok := idParam(w, r, "routeID")
	if !ok {
		return
	}
	var req struct {
		StopIDs []string `json:"stop_ids"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	for _, id := range req.StopIDs {
		if _, err := uuid.Parse(id); err != nil {
			httpx.ValidationError(w, validate.Errors{"stop_ids": "must be stop IDs"})
			return
		}
	}
	ctx := r.Context()
	var stops []models.Stop
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		if err := store.LockRoute(ctx, q, schoolID, routeID); err != nil {
			return err
		}
		before, err := store.ListStops(ctx, q, routeID)
		if err != nil {
			return err
		}
		if err := store.ReorderStops(ctx, q, routeID, req.StopIDs); err != nil {
			return err
		}
		if stops, err = store.ListStops(ctx, q, routeID); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "route.stops_reorder", EntityType: "route", EntityID: &routeID,
			Before: stopIDs(before), After: stopIDs(stops),
		})
	})
	if errors.Is(err, store.ErrStopOrderMismatch) {
		httpx.ValidationError(w, validate.Errors{"stop_ids": "must list every stop of this route exactly once"})
		return
	}
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, stops)
}

func stopIDs(stops []models.Stop) []string {
	ids := make([]string, len(stops))
	for i, s := range stops {
		ids[i] = s.ID
	}
	return ids
}
