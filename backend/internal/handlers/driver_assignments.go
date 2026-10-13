package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// Unique slot indexes → the request field that clashes (mirrors tripSlotConflicts).
var assignmentConflicts = map[string]struct{ field, msg string }{
	"driver_route_assignments_route_key":  {"route_id", "This route already has a current permanent assignment for this trip type."},
	"driver_route_assignments_bus_key":    {"bus_id", "This bus already has a current permanent assignment for this trip type."},
	"driver_route_assignments_driver_key": {"driver_id", "This driver already has a current permanent assignment for this trip type."},
}

func writeAssignmentError(w http.ResponseWriter, r *http.Request, err error) {
	var te *tripError
	var ce *store.ConflictError
	switch {
	case errors.As(err, &te):
		httpx.ErrorWithFields(w, te.status, te.body)
	case errors.As(err, &ce) && assignmentConflicts[ce.Constraint].field != "":
		c := assignmentConflicts[ce.Constraint]
		httpx.ErrorWithFields(w, http.StatusConflict, httpx.ErrorBody{
			Code: "assignment_conflict", Message: c.msg, Fields: map[string]string{c.field: c.msg},
		})
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
	default:
		storeError(w, r, err, nil)
	}
}

type driverAssignmentRequest struct {
	RouteID  string `json:"route_id"`
	TripType string `json:"trip_type"`
	DriverID string `json:"driver_id"`
	BusID    string `json:"bus_id"`
}

func (req *driverAssignmentRequest) input() store.AssignmentInput {
	return store.AssignmentInput{RouteID: req.RouteID, TripType: req.TripType, DriverID: req.DriverID, BusID: req.BusID}
}

// validateDriverAssignment checks the trip type and that route, bus and driver belong to the
// school, are active, and the route fits the trip type (same fit-check validateTrip uses).
func validateDriverAssignment(ctx context.Context, q store.DBTX, schoolID string, req *driverAssignmentRequest) error {
	v := validate.New()
	v.OneOf("trip_type", req.TripType, models.TripMorningPickup, models.TripEveningDrop)
	for field, id := range map[string]string{"route_id": req.RouteID, "bus_id": req.BusID, "driver_id": req.DriverID} {
		_, err := uuid.Parse(id)
		v.Check(err == nil, field, "is required")
	}
	if !v.OK() {
		return tripValidation(v.Errors())
	}

	routeID := req.RouteID
	refs, err := store.GetTripRefs(ctx, q, schoolID, &routeID, req.BusID, req.DriverID)
	if err != nil {
		return err
	}
	v.Check(refs.RouteFound, "route_id", "route not found")
	v.Check(!refs.RouteFound || refs.RouteStatus == models.StatusActive, "route_id", "route is inactive")
	switch req.TripType {
	case models.TripMorningPickup:
		v.Check(!refs.RouteFound || refs.SupportsPickup, "route_id", "route does not run Morning Pickup")
	case models.TripEveningDrop:
		v.Check(!refs.RouteFound || refs.SupportsDrop, "route_id", "route does not run Evening Drop")
		v.Check(!refs.RouteFound || refs.DropSequenceComplete, "route_id", "the route's Evening Drop stop order is not fully configured yet")
	}
	v.Check(refs.BusFound, "bus_id", "bus not found")
	v.Check(!refs.BusFound || refs.BusStatus == models.StatusActive, "bus_id", "bus is not active")
	v.Check(refs.DriverFound, "driver_id", "driver not found")
	v.Check(!refs.DriverFound || refs.DriverStatus == models.StatusActive, "driver_id", "driver is not active")
	if !v.OK() {
		return tripValidation(v.Errors())
	}
	return nil
}

// GetDriverAssignment returns one current or past assignment by id.
func (a *API) GetDriverAssignment(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "assignmentID")
	if !ok {
		return
	}
	out, err := store.GetAssignment(r.Context(), a.Store.Pool, schoolID, id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ListDriverAssignments lists current permanent Morning/Evening route assignments,
// optionally filtered to one driver (?driver_id=).
func (a *API) ListDriverAssignments(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	driverID := r.URL.Query().Get("driver_id")
	if driverID != "" {
		if _, err := uuid.Parse(driverID); err != nil {
			httpx.ValidationError(w, validate.Errors{"driver_id": "must be a valid id"})
			return
		}
	}
	list, err := store.ListAssignments(r.Context(), a.Store.Pool, schoolID, driverID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

// CreateDriverAssignment permanently binds a driver+bus to a route for Morning Pickup or
// Evening Drop. It never creates a trip itself - GET /driver/trips materializes today's
// trip from it lazily (store.EnsureDriverDailyTrips).
func (a *API) CreateDriverAssignment(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req driverAssignmentRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	var out *models.DriverRouteAssignment
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		if err := validateDriverAssignment(ctx, q, schoolID, &req); err != nil {
			return err
		}
		id, err := store.CreateAssignment(ctx, q, schoolID, req.input(), &p.UserID)
		if err != nil {
			return err
		}
		if out, err = store.GetAssignment(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "driver_assignment.create", EntityType: "driver_route_assignment", EntityID: &id, After: out,
		})
	})
	if err != nil {
		writeAssignmentError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// UpdateDriverAssignment changes the driver, bus or route of a current assignment: the
// old row is ended and a new one created (history preserved), never mutated in place.
// Trips already materialized under the old assignment are untouched.
func (a *API) UpdateDriverAssignment(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "assignmentID")
	if !ok {
		return
	}
	var req driverAssignmentRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	var before, after *models.DriverRouteAssignment
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		var err error
		if before, err = store.GetAssignment(ctx, q, schoolID, id); err != nil {
			return err
		}
		if err := validateDriverAssignment(ctx, q, schoolID, &req); err != nil {
			return err
		}
		newID, err := store.UpdateAssignment(ctx, q, schoolID, id, req.input(), &p.UserID)
		if err != nil {
			return err
		}
		if after, err = store.GetAssignment(ctx, q, schoolID, newID); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "driver_assignment.update", EntityType: "driver_route_assignment", EntityID: &newID, Before: before, After: after,
		})
	})
	if err != nil {
		writeAssignmentError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

// DeactivateDriverAssignment ends a current assignment with no replacement. Historical
// trips already created from it are never touched.
func (a *API) DeactivateDriverAssignment(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "assignmentID")
	if !ok {
		return
	}
	ctx := r.Context()
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetAssignment(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if err := store.DeactivateAssignment(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "driver_assignment.deactivate", EntityType: "driver_route_assignment", EntityID: &id, Before: before,
		})
	})
	if err != nil {
		writeAssignmentError(w, r, err)
		return
	}
	httpx.NoContent(w)
}
