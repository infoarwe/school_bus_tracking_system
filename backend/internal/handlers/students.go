package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

var studentConflicts = map[string]string{"students_school_admission_key": "admission_no"}

// transportView strips what a Transport Manager may not see: their student
// access is limited to transport needs (CLAUDE.md roles).
func transportView(r *http.Request, students ...*models.Student) {
	if p := auth.FromContext(r.Context()); p != nil && p.Role == models.RoleTransportManager {
		for _, s := range students {
			s.Notes = nil
		}
	}
}

type studentRequest struct {
	AdmissionNo     string `json:"admission_no"`
	Name            string `json:"name"`
	Class           string `json:"class"`
	Section         string `json:"section"`
	Notes           string `json:"notes"`
	TransportStatus string `json:"transport_status"`
}

func (req *studentRequest) toInput() (store.StudentInput, validate.Errors) {
	in := store.StudentInput{
		AdmissionNo:     strings.ToUpper(strings.TrimSpace(req.AdmissionNo)),
		Name:            strings.TrimSpace(req.Name),
		Class:           strings.ToUpper(strings.TrimSpace(req.Class)),
		Section:         strings.ToUpper(strings.TrimSpace(req.Section)),
		Notes:           req.Notes,
		TransportStatus: req.TransportStatus,
	}
	if in.TransportStatus == "" {
		in.TransportStatus = models.TransportUses
	}
	v := validate.New()
	v.Required("admission_no", in.AdmissionNo)
	v.MaxLen("admission_no", in.AdmissionNo, 30)
	v.Required("name", in.Name)
	v.MaxLen("name", in.Name, 200)
	v.MaxLen("class", in.Class, 20)
	v.MaxLen("section", in.Section, 10)
	v.MaxLen("notes", in.Notes, 1000)
	v.OneOf("transport_status", in.TransportStatus, models.TransportUses, models.TransportNotUsed)
	return in, v.Errors()
}

func (a *API) ListStudents(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	pg := page(r)
	students, total, err := store.ListStudents(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), store.StudentFilter{
		Q: qs.Get("q"), Status: qs.Get("status"), TransportStatus: qs.Get("transport_status"),
		Class: strings.ToUpper(qs.Get("class")), Section: strings.ToUpper(qs.Get("section")),
		RouteID: qs.Get("route_id"), Assigned: qs.Get("assigned"),
	}, pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	for i := range students {
		transportView(r, &students[i])
	}
	httpx.List(w, students, meta(pg, total))
}

// GetStudent includes the linked parents.
func (a *API) GetStudent(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "studentID")
	if !ok {
		return
	}
	s, err := store.GetStudent(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if s.Parents, err = store.StudentParents(r.Context(), a.Store.Pool, id); err != nil {
		internalError(w, r, err)
		return
	}
	transportView(r, s)
	httpx.JSON(w, http.StatusOK, s)
}

func (a *API) StudentClasses(w http.ResponseWriter, r *http.Request) {
	classes, err := store.StudentClasses(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"))
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, classes)
}

func (a *API) CreateStudent(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req studentRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	ctx := r.Context()
	var s *models.Student
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		id, err := store.CreateStudent(ctx, q, schoolID, in)
		if err != nil {
			return err
		}
		if s, err = store.GetStudent(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "student.create", EntityType: "student", EntityID: &id, After: s,
		})
	})
	if err != nil {
		storeError(w, r, err, studentConflicts)
		return
	}
	s.Parents = []models.ParentLink{}
	httpx.JSON(w, http.StatusCreated, s)
}

// UpdateStudent: switching transport_status to not_using ends the route assignment.
func (a *API) UpdateStudent(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "studentID")
	if !ok {
		return
	}
	var req studentRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	ctx := r.Context()
	var after *models.Student
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetStudent(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if err := store.UpdateStudent(ctx, q, schoolID, id, in); err != nil {
			return err
		}
		if in.TransportStatus == models.TransportNotUsed && before.Assignment != nil {
			if err := store.EndAssignment(ctx, q, schoolID, id); err != nil {
				return err
			}
		}
		if after, err = store.GetStudent(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "student.update", EntityType: "student", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		storeError(w, r, err, studentConflicts)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

func (a *API) SetStudentStatus(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "studentID")
	if !ok {
		return
	}
	status, ok := decodeStatus(w, r, models.StatusActive, models.StatusInactive)
	if !ok {
		return
	}
	ctx := r.Context()
	var after *models.Student
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetStudent(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if err := store.SetStudentStatus(ctx, q, schoolID, id, status); err != nil {
			return err
		}
		if after, err = store.GetStudent(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "student.status_change", EntityType: "student", EntityID: &id,
			Before: map[string]string{"status": before.Status}, After: map[string]string{"status": status},
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

type assignmentRequest struct {
	RouteID      string  `json:"route_id"`
	PickupStopID *string `json:"pickup_stop_id"`
	DropStopID   *string `json:"drop_stop_id"`
}

// errAssignment carries field errors out of the assignment transaction.
type errAssignment struct{ fields validate.Errors }

func (e *errAssignment) Error() string { return "invalid assignment" }

// validateAssignment enforces rule 5: the stops must belong to the chosen route,
// with a pickup stop when the route runs Morning Pickup and a drop stop when it
// runs Evening Drop.
func validateAssignment(r *http.Request, q store.DBTX, schoolID string, s *models.Student, req *assignmentRequest) (*models.Route, error) {
	c := r.Context()
	v := validate.New()
	v.Check(s.Status == models.StatusActive, "student", "student is inactive")
	v.Check(s.TransportStatus == models.TransportUses, "student", "student is marked as not using school transport")
	if _, err := uuid.Parse(req.RouteID); err != nil {
		v.Check(false, "route_id", "is required")
		return nil, &errAssignment{v.Errors()}
	}
	route, err := store.GetRoute(c, q, schoolID, req.RouteID)
	if errors.Is(err, store.ErrNotFound) {
		v.Check(false, "route_id", "route not found")
		return nil, &errAssignment{v.Errors()}
	}
	if err != nil {
		return nil, err
	}
	v.Check(route.Status == models.StatusActive, "route_id", "route is inactive")
	stops, err := store.RouteStopIDs(c, q, route.ID)
	if err != nil {
		return nil, err
	}
	checkStop := func(field string, id *string, used bool, trip string) {
		switch {
		case used && (id == nil || *id == ""):
			v.Check(false, field, "is required: this route runs "+trip)
		case !used && id != nil && *id != "":
			v.Check(false, field, "must be empty: this route does not run "+trip)
		case used && !stops[*id]:
			v.Check(false, field, "must be a stop of the selected route")
		}
	}
	checkStop("pickup_stop_id", req.PickupStopID, route.SupportsPickup, "Morning Pickup")
	checkStop("drop_stop_id", req.DropStopID, route.SupportsDrop, "Evening Drop")
	if !v.OK() {
		return nil, &errAssignment{v.Errors()}
	}
	if !route.SupportsPickup {
		req.PickupStopID = nil
	}
	if !route.SupportsDrop {
		req.DropStopID = nil
	}
	return route, nil
}

// AssignStudent sets the student's route and stops. The previous assignment is kept as history.
func (a *API) AssignStudent(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "studentID")
	if !ok {
		return
	}
	var req assignmentRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	p := auth.FromContext(ctx)
	var after *models.Student
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetStudent(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if _, err := validateAssignment(r, q, schoolID, before, &req); err != nil {
			return err
		}
		if cur := before.Assignment; cur != nil && cur.RouteID == req.RouteID &&
			sameStop(cur.PickupStop, req.PickupStopID) && sameStop(cur.DropStop, req.DropStopID) {
			after = before // unchanged: do not add a history row
			return nil
		}
		if err := store.AssignStudent(ctx, q, schoolID, id, req.RouteID, req.PickupStopID, req.DropStopID, p.UserID); err != nil {
			return err
		}
		if after, err = store.GetStudent(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "student.assign", EntityType: "student", EntityID: &id,
			Before: before.Assignment, After: after.Assignment,
		})
	})
	var ae *errAssignment
	if errors.As(err, &ae) {
		httpx.ValidationError(w, ae.fields)
		return
	}
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

func sameStop(cur *models.StopRef, id *string) bool {
	if cur == nil || id == nil {
		return cur == nil && (id == nil || *id == "")
	}
	return cur.ID == *id
}

// UnassignStudent removes the student from their route (kept as history).
func (a *API) UnassignStudent(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "studentID")
	if !ok {
		return
	}
	ctx := r.Context()
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetStudent(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if before.Assignment == nil {
			return nil
		}
		if err := store.EndAssignment(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "student.unassign", EntityType: "student", EntityID: &id, Before: before.Assignment,
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.NoContent(w)
}

func (a *API) StudentAssignmentHistory(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "studentID")
	if !ok {
		return
	}
	if _, err := store.GetStudent(r.Context(), a.Store.Pool, schoolID, id); err != nil {
		storeError(w, r, err, nil)
		return
	}
	history, err := store.AssignmentHistory(r.Context(), a.Store.Pool, schoolID, id)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, history)
}

// RouteStudents lists students currently assigned to a route, with their stop IDs.
func (a *API) RouteStudents(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	routeID, ok := idParam(w, r, "routeID")
	if !ok {
		return
	}
	if _, err := store.GetRoute(r.Context(), a.Store.Pool, schoolID, routeID); err != nil {
		storeError(w, r, err, nil)
		return
	}
	students, err := store.RouteStudents(r.Context(), a.Store.Pool, schoolID, routeID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, students)
}
