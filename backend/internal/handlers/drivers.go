package handlers

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

var (
	driverConflicts = map[string]string{"users_mobile_role_key": "mobile"}
	isoDateRe       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

type driverRequest struct {
	Name                  string  `json:"name"`
	Mobile                string  `json:"mobile"`
	LicenseNumber         string  `json:"license_number"`
	LicenseExpiry         *string `json:"license_expiry"`
	Address               string  `json:"address"`
	EmergencyContactName  string  `json:"emergency_contact_name"`
	EmergencyContactPhone string  `json:"emergency_contact_phone"`
	IDProofType           string  `json:"id_proof_type"`
	IDProofNumber         string  `json:"id_proof_number"`
	Notes                 string  `json:"notes"`
}

func (req *driverRequest) validate() validate.Errors {
	req.Name = strings.TrimSpace(req.Name)
	req.LicenseNumber = strings.ToUpper(strings.TrimSpace(req.LicenseNumber))
	if req.LicenseExpiry != nil && strings.TrimSpace(*req.LicenseExpiry) == "" {
		req.LicenseExpiry = nil
	}

	v := validate.New()
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 200)
	v.Required("mobile", req.Mobile)
	v.Mobile("mobile", &req.Mobile)
	v.MaxLen("license_number", req.LicenseNumber, 30)
	if req.LicenseExpiry != nil {
		_, err := time.Parse(time.DateOnly, *req.LicenseExpiry)
		v.Check(isoDateRe.MatchString(*req.LicenseExpiry) && err == nil, "license_expiry", "must be a date YYYY-MM-DD")
	}
	v.MaxLen("address", req.Address, 500)
	v.MaxLen("emergency_contact_name", req.EmergencyContactName, 200)
	v.Mobile("emergency_contact_phone", &req.EmergencyContactPhone)
	v.MaxLen("id_proof_type", req.IDProofType, 50)
	v.MaxLen("id_proof_number", req.IDProofNumber, 50)
	v.MaxLen("notes", req.Notes, 1000)
	return v.Errors()
}

func (req *driverRequest) profile() store.DriverProfile {
	return store.DriverProfile{
		LicenseNumber: req.LicenseNumber, LicenseExpiry: req.LicenseExpiry, Address: req.Address,
		EmergencyContactName: req.EmergencyContactName, EmergencyContactPhone: req.EmergencyContactPhone,
		IDProofType: req.IDProofType, IDProofNumber: req.IDProofNumber, Notes: req.Notes,
	}
}

func (a *API) ListDrivers(w http.ResponseWriter, r *http.Request) {
	pg := page(r)
	drivers, total, err := store.ListDrivers(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), listFilter(r), pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.List(w, drivers, meta(pg, total))
}

func (a *API) GetDriver(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "driverID")
	if !ok {
		return
	}
	d, err := store.GetDriver(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// CreateDriver creates the driver and their login user (mobile + OTP) together.
func (a *API) CreateDriver(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req driverRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if errs := req.validate(); len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	ctx := r.Context()
	var d *models.Driver
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		u, err := store.CreateUser(ctx, q, store.UserInput{
			SchoolID: &schoolID, Role: models.RoleDriver, Name: req.Name, Mobile: &req.Mobile,
		})
		if err != nil {
			return err
		}
		id, err := store.CreateDriver(ctx, q, schoolID, u.ID, req.profile())
		if err != nil {
			return err
		}
		if d, err = store.GetDriver(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "driver.create", EntityType: "driver", EntityID: &d.ID, After: d,
		})
	})
	if err != nil {
		storeError(w, r, err, driverConflicts)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (a *API) UpdateDriver(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "driverID")
	if !ok {
		return
	}
	var req driverRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if errs := req.validate(); len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	ctx := r.Context()
	var after *models.Driver
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetDriver(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if _, err := store.UpdateUserProfile(ctx, q, before.UserID, req.Name, nil, &req.Mobile); err != nil {
			return err
		}
		if err := store.UpdateDriver(ctx, q, schoolID, id, req.profile()); err != nil {
			return err
		}
		if after, err = store.GetDriver(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "driver.update", EntityType: "driver", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		storeError(w, r, err, driverConflicts)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

// SetDriverStatus: only an active driver can log in. Inactive or suspended
// drivers are logged out everywhere and their login is suspended.
func (a *API) SetDriverStatus(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "driverID")
	if !ok {
		return
	}
	status, ok := decodeStatus(w, r, models.StatusActive, models.StatusInactive, models.StatusSuspended)
	if !ok {
		return
	}
	ctx := r.Context()
	var after *models.Driver
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetDriver(ctx, q, schoolID, id)
		if err != nil {
			return err
		}
		if err := store.SetDriverStatus(ctx, q, schoolID, id, status); err != nil {
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
		if after, err = store.GetDriver(ctx, q, schoolID, id); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "driver.status_change", EntityType: "driver", EntityID: &id,
			Before: map[string]string{"status": before.Status}, After: map[string]string{"status": status},
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}
