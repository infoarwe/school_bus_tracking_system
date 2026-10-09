package handlers

// Caller-scoped endpoints for the Driver and Parent apps. The school and user
// come from the authenticated principal; nothing is looked up by a client-sent
// school ID, so an app user can only ever see their own data.

import (
	"errors"
	"net/http"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// DriverMe returns the logged-in driver's own profile.
func (a *API) DriverMe(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	d, err := store.GetDriverByUser(r.Context(), a.Store.Pool, p.UserID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "driver_profile_missing", "No driver profile is linked to this login. Contact your school.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// ParentChildren lists the logged-in parent's linked children with their route and stops.
func (a *API) ParentChildren(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	children, err := store.ParentChildren(r.Context(), a.Store.Pool, p.UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, children)
}

type parentChildDetail struct {
	store.ParentChild
	// Ordered stops of the child's route, for drawing it on the map. Stop names
	// and locations only; no other students' data.
	RouteStops []models.Stop `json:"route_stops"`
}

// ParentChild returns one linked child with the full stop list of their route.
// A student who is not linked to this parent is "not found".
func (a *API) ParentChild(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "studentID")
	if !ok {
		return
	}
	p := auth.FromContext(r.Context())
	children, err := store.ParentChildren(r.Context(), a.Store.Pool, p.UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	for _, c := range children {
		if c.StudentID != id {
			continue
		}
		detail := parentChildDetail{ParentChild: c, RouteStops: []models.Stop{}}
		if c.Assignment != nil {
			if detail.RouteStops, err = store.ListStops(r.Context(), a.Store.Pool, c.Assignment.RouteID); err != nil {
				internalError(w, r, err)
				return
			}
		}
		httpx.JSON(w, http.StatusOK, detail)
		return
	}
	httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
}
