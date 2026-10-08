package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// School users here are the web staff of a school: School Admins and Transport
// Managers. Drivers and parents are managed through their own modules.
var staffRoles = []models.Role{models.RoleSchoolAdmin, models.RoleTransportManager}

var userConflicts = map[string]string{"users_email_key": "email", "users_mobile_role_key": "mobile"}

const minPasswordLen = 8

// canManage: Super Admin manages School Admins and Transport Managers;
// a School Admin manages Transport Managers only.
func canManage(p *auth.Principal, target models.Role) bool {
	switch p.Role {
	case models.RoleSuperAdmin:
		return target == models.RoleSchoolAdmin || target == models.RoleTransportManager
	case models.RoleSchoolAdmin:
		return target == models.RoleTransportManager
	}
	return false
}

func (a *API) ListSchoolUsers(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	roles := staffRoles
	if role := models.Role(r.URL.Query().Get("role")); role == models.RoleSchoolAdmin || role == models.RoleTransportManager {
		roles = []models.Role{role}
	}
	pg := page(r)
	users, total, err := store.ListSchoolUsers(r.Context(), a.Store.Pool, schoolID,
		store.UserFilter{Roles: roles, Status: r.URL.Query().Get("status"), Q: r.URL.Query().Get("q")}, pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.List(w, users, meta(pg, total))
}

func (a *API) GetSchoolUser(w http.ResponseWriter, r *http.Request) {
	u, ok := a.loadStaffUser(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}

type userRequest struct {
	Name     string  `json:"name"`
	Email    string  `json:"email"`
	Mobile   *string `json:"mobile"`
	Role     string  `json:"role"`
	Password string  `json:"password"`
}

func (req *userRequest) validate(v *validate.V) {
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Mobile != nil && strings.TrimSpace(*req.Mobile) == "" {
		req.Mobile = nil
	}
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 200)
	v.Required("email", req.Email)
	v.Email("email", req.Email)
	v.Mobile("mobile", req.Mobile)
}

func (a *API) CreateSchoolUser(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req userRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	req.validate(v)
	v.OneOf("role", req.Role, string(models.RoleSchoolAdmin), string(models.RoleTransportManager))
	v.Check(len(req.Password) >= minPasswordLen, "password", "must be at least 8 characters")
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	p := auth.FromContext(r.Context())
	if !canManage(p, models.Role(req.Role)) {
		httpx.Error(w, http.StatusForbidden, "forbidden", "You cannot create users with this role.")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		internalError(w, r, err)
		return
	}

	var u *models.User
	err = a.Store.InTx(r.Context(), func(q store.DBTX) error {
		var err error
		u, err = store.CreateUser(r.Context(), q, store.UserInput{
			SchoolID: &schoolID, Role: models.Role(req.Role), Name: req.Name,
			Email: &req.Email, Mobile: req.Mobile, PasswordHash: &hash,
		})
		if err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "user.create", EntityType: "user", EntityID: &u.ID, After: u,
		})
	})
	if err != nil {
		storeError(w, r, err, userConflicts)
		return
	}
	httpx.JSON(w, http.StatusCreated, u)
}

func (a *API) UpdateSchoolUser(w http.ResponseWriter, r *http.Request) {
	before, ok := a.loadManageableUser(w, r)
	if !ok {
		return
	}
	var req userRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	req.validate(v)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	var after *models.User
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		var err error
		if after, err = store.UpdateUserProfile(r.Context(), q, before.ID, req.Name, &req.Email, req.Mobile); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: before.SchoolID, Action: "user.update", EntityType: "user", EntityID: &before.ID,
			Before: before, After: after,
		})
	})
	if err != nil {
		storeError(w, r, err, userConflicts)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

// SetSchoolUserStatus suspends or reactivates a user. Suspending ends all their sessions.
func (a *API) SetSchoolUserStatus(w http.ResponseWriter, r *http.Request) {
	before, ok := a.loadManageableUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	v.OneOf("status", req.Status, models.StatusActive, models.StatusSuspended)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	var after *models.User
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		var err error
		if after, err = store.SetUserStatus(r.Context(), q, before.ID, req.Status); err != nil {
			return err
		}
		if req.Status == models.StatusSuspended {
			if err := store.RevokeAllSessions(r.Context(), q, before.ID); err != nil {
				return err
			}
		}
		action := "user.activate"
		if req.Status == models.StatusSuspended {
			action = "user.suspend"
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: before.SchoolID, Action: action, EntityType: "user", EntityID: &before.ID,
			Before: map[string]string{"status": before.Status}, After: map[string]string{"status": after.Status},
		})
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

// ResetSchoolUserPassword sets a new password and ends all the user's sessions.
func (a *API) ResetSchoolUserPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := a.loadManageableUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if len(req.Password) < minPasswordLen {
		httpx.ValidationError(w, validate.Errors{"password": "must be at least 8 characters"})
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		internalError(w, r, err)
		return
	}
	err = a.Store.InTx(r.Context(), func(q store.DBTX) error {
		if err := store.SetUserPassword(r.Context(), q, u.ID, hash); err != nil {
			return err
		}
		if err := store.RevokeAllSessions(r.Context(), q, u.ID); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: u.SchoolID, Action: "user.password_reset", EntityType: "user", EntityID: &u.ID,
		})
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// loadStaffUser loads a School Admin or Transport Manager of the URL's school.
func (a *API) loadStaffUser(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	id, ok := idParam(w, r, "userID")
	if !ok {
		return nil, false
	}
	u, err := store.GetSchoolUser(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return nil, false
	}
	if u.Role != models.RoleSchoolAdmin && u.Role != models.RoleTransportManager {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return nil, false
	}
	return u, true
}

// loadManageableUser is loadStaffUser plus: the caller may manage this role, and is not acting on themselves.
func (a *API) loadManageableUser(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	u, ok := a.loadStaffUser(w, r)
	if !ok {
		return nil, false
	}
	p := auth.FromContext(r.Context())
	if !canManage(p, u.Role) {
		httpx.Error(w, http.StatusForbidden, "forbidden", "You cannot manage users with this role.")
		return nil, false
	}
	if u.ID == p.UserID {
		httpx.Error(w, http.StatusForbidden, "forbidden", "You cannot change your own account here.")
		return nil, false
	}
	return u, true
}
