package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

var (
	parentConflicts = map[string]string{"users_mobile_role_key": "mobile"}
	relationships   = []string{"father", "mother", "guardian"}
)

type parentRequest struct {
	Name            string `json:"name"`
	Mobile          string `json:"mobile"`
	AlternateMobile string `json:"alternate_mobile"`
	Email           string `json:"email"`
	Address         string `json:"address"`
	Children        []struct {
		StudentID    string `json:"student_id"`
		Relationship string `json:"relationship"`
	} `json:"children"`
}

func (req *parentRequest) validate() ([]store.ChildInput, validate.Errors) {
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	v := validate.New()
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 200)
	v.Required("mobile", req.Mobile)
	v.Mobile("mobile", &req.Mobile)
	v.Mobile("alternate_mobile", &req.AlternateMobile)
	v.Email("email", req.Email)
	v.MaxLen("address", req.Address, 500)

	children := make([]store.ChildInput, 0, len(req.Children))
	seen := map[string]bool{}
	for _, c := range req.Children {
		_, err := uuid.Parse(c.StudentID)
		v.Check(err == nil && !seen[c.StudentID], "children", "must be distinct students")
		seen[c.StudentID] = true
		if c.Relationship == "" {
			c.Relationship = "guardian"
		}
		v.OneOf("children", c.Relationship, relationships...)
		children = append(children, store.ChildInput{StudentID: c.StudentID, Relationship: c.Relationship})
	}
	return children, v.Errors()
}

func (req *parentRequest) profile() store.ParentProfile {
	return store.ParentProfile{AlternateMobile: req.AlternateMobile, Email: req.Email, Address: req.Address}
}

var errForeignStudent = errors.New("student not in school")

// setChildren links the students after checking they all belong to the school.
func setChildren(r *http.Request, q store.DBTX, schoolID, parentID string, children []store.ChildInput) error {
	if len(children) > 0 {
		ids := make([]string, len(children))
		for i, c := range children {
			ids[i] = c.StudentID
		}
		n, err := store.CountSchoolStudents(r.Context(), q, schoolID, ids)
		if err != nil {
			return err
		}
		if n != len(ids) {
			return errForeignStudent
		}
	}
	return store.SetParentChildren(r.Context(), q, schoolID, parentID, children)
}

func parentSaveError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errForeignStudent) {
		httpx.ValidationError(w, validate.Errors{"children": "one or more students were not found"})
		return
	}
	storeError(w, r, err, parentConflicts)
}

func (a *API) ListParents(w http.ResponseWriter, r *http.Request) {
	pg := page(r)
	parents, total, err := store.ListParents(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), listFilter(r), pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.List(w, parents, meta(pg, total))
}

func (a *API) GetParent(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "parentID")
	if !ok {
		return
	}
	p, err := store.GetParent(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

// CreateParent creates the parent, their app login (mobile + OTP) and the links to their children.
func (a *API) CreateParent(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req parentRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	children, errs := req.validate()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	ctx := r.Context()
	var p *models.Parent
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		u, err := store.CreateUser(ctx, q, store.UserInput{
			SchoolID: &schoolID, Role: models.RoleParent, Name: req.Name, Mobile: &req.Mobile,
		})
		if err != nil {
			return err
		}
		id, err := store.CreateParent(ctx, q, schoolID, u.ID, req.profile())
		if err != nil {
			return err
		}
		if err := setChildren(r, q, schoolID, id, children); err != nil {
			return err
		}
		if p, err = store.GetParent(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "parent.create", EntityType: "parent", EntityID: &id, After: p,
		})
	})
	if err != nil {
		parentSaveError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, p)
}

// UpdateParent updates the profile and replaces the list of linked children.
func (a *API) UpdateParent(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "parentID")
	if !ok {
		return
	}
	var req parentRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	children, errs := req.validate()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	ctx := r.Context()
	var after *models.Parent
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetParent(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if _, err := store.UpdateUserProfile(ctx, q, before.UserID, req.Name, nil, &req.Mobile); err != nil {
			return err
		}
		if err := store.UpdateParent(ctx, q, schoolID, id, req.profile()); err != nil {
			return err
		}
		if err := setChildren(r, q, schoolID, id, children); err != nil {
			return err
		}
		if after, err = store.GetParent(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "parent.update", EntityType: "parent", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		parentSaveError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

// SetParentStatus: an inactive parent cannot log in and is logged out everywhere.
func (a *API) SetParentStatus(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "parentID")
	if !ok {
		return
	}
	status, ok := decodeStatus(w, r, models.StatusActive, models.StatusInactive)
	if !ok {
		return
	}
	ctx := r.Context()
	var after *models.Parent
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetParent(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if err := store.SetParentStatus(ctx, q, schoolID, id, status); err != nil {
			return err
		}
		userStatus := models.StatusActive
		if status != models.StatusActive {
			userStatus = models.StatusSuspended
			if err := store.RevokeAllSessions(ctx, q, before.UserID); err != nil {
				return err
			}
		}
		if _, err := store.SetUserStatus(ctx, q, before.UserID, userStatus); err != nil {
			return err
		}
		if after, err = store.GetParent(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "parent.status_change", EntityType: "parent", EntityID: &id,
			Before: map[string]string{"status": before.Status}, After: map[string]string{"status": status},
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}
