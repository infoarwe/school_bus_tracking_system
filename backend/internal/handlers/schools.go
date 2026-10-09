package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

var (
	schoolCodeRe     = regexp.MustCompile(`^[A-Za-z0-9-]{2,20}$`)
	pincodeRe        = regexp.MustCompile(`^\d{6}$`)
	validWorkingDays = map[string]bool{"mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true, "sun": true}
	schoolConflicts  = map[string]string{"schools_code_key": "code"}
)

type schoolRequest struct {
	Name            string          `json:"name"`
	Code            string          `json:"code"`
	Address         string          `json:"address"`
	City            string          `json:"city"`
	State           string          `json:"state"`
	Pincode         string          `json:"pincode"`
	ContactName     string          `json:"contact_name"`
	ContactPhone    string          `json:"contact_phone"`
	ContactEmail    string          `json:"contact_email"`
	WorkingDays     []string        `json:"working_days"`
	Timezone        string          `json:"timezone"`
	TransportConfig json.RawMessage `json:"transport_config"`
}

func (req *schoolRequest) toInput() (store.SchoolInput, validate.Errors) {
	req.Name = strings.TrimSpace(req.Name)
	req.Code = strings.ToUpper(strings.TrimSpace(req.Code))
	req.ContactEmail = strings.TrimSpace(req.ContactEmail)
	if req.Timezone == "" {
		req.Timezone = "Asia/Kolkata"
	}
	if len(req.WorkingDays) == 0 {
		req.WorkingDays = []string{"mon", "tue", "wed", "thu", "fri"}
	}
	if len(req.TransportConfig) == 0 || string(req.TransportConfig) == "null" {
		req.TransportConfig = json.RawMessage(`{}`)
	}

	v := validate.New()
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 200)
	v.Check(schoolCodeRe.MatchString(req.Code), "code", "must be 2-20 letters, digits or hyphens")
	v.MaxLen("address", req.Address, 500)
	v.Check(req.Pincode == "" || pincodeRe.MatchString(req.Pincode), "pincode", "must be 6 digits")
	v.MaxLen("contact_name", req.ContactName, 200)
	v.Mobile("contact_phone", &req.ContactPhone)
	v.Email("contact_email", req.ContactEmail)
	seen := map[string]bool{}
	for _, d := range req.WorkingDays {
		v.Check(validWorkingDays[d] && !seen[d], "working_days", "must be unique values from mon..sun")
		seen[d] = true
	}
	_, tzErr := time.LoadLocation(req.Timezone)
	v.Check(tzErr == nil, "timezone", "must be a valid IANA time zone, e.g. Asia/Kolkata")
	var obj map[string]any
	v.Check(json.Unmarshal(req.TransportConfig, &obj) == nil, "transport_config", "must be a JSON object")

	return store.SchoolInput{
		Name: req.Name, Code: req.Code, Address: req.Address, City: req.City, State: req.State,
		Pincode: req.Pincode, ContactName: req.ContactName, ContactPhone: req.ContactPhone,
		ContactEmail: req.ContactEmail, WorkingDays: req.WorkingDays, Timezone: req.Timezone,
		TransportConfig: req.TransportConfig,
	}, v.Errors()
}

// ListSchools: Super Admin only.
func (a *API) ListSchools(w http.ResponseWriter, r *http.Request) {
	pg := page(r)
	f := store.SchoolFilter{Q: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status")}
	schools, total, err := store.ListSchools(r.Context(), a.Store.Pool, f, pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.List(w, schools, meta(pg, total))
}

// GetSchool: Super Admin, or a member of that school (enforced by SchoolScope).
func (a *API) GetSchool(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "schoolID")
	if !ok {
		return
	}
	s, err := store.GetSchool(r.Context(), a.Store.Pool, id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

func (a *API) CreateSchool(w http.ResponseWriter, r *http.Request) {
	var req schoolRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	var s *models.School
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		var err error
		if s, err = store.CreateSchool(r.Context(), q, in); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &s.ID, Action: "school.create", EntityType: "school", EntityID: &s.ID, After: s,
		})
	})
	if err != nil {
		storeError(w, r, err, schoolConflicts)
		return
	}
	httpx.JSON(w, http.StatusCreated, s)
}

func (a *API) UpdateSchool(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "schoolID")
	if !ok {
		return
	}
	var req schoolRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	var after *models.School
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		before, err := store.GetSchool(r.Context(), q, id)
		if err != nil {
			return err
		}
		if after, err = store.UpdateSchool(r.Context(), q, id, in); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &id, Action: "school.update", EntityType: "school", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		storeError(w, r, err, schoolConflicts)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

// SetSchoolStatus activates or deactivates a school. Users of an inactive school cannot log in.
func (a *API) SetSchoolStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "schoolID")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
		// Critical action: deactivating requires typing the school's code.
		ConfirmCode string `json:"confirm_code"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	v.OneOf("status", req.Status, models.StatusActive, models.StatusInactive)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	var after *models.School
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		before, err := store.GetSchool(r.Context(), q, id)
		if err != nil {
			return err
		}
		if req.Status == models.StatusInactive && !strings.EqualFold(strings.TrimSpace(req.ConfirmCode), before.Code) {
			return errConfirmCode
		}
		if after, err = store.SetSchoolStatus(r.Context(), q, id, req.Status); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &id, Action: "school.status_change", EntityType: "school", EntityID: &id,
			Before: map[string]string{"status": before.Status}, After: map[string]string{"status": after.Status},
		})
	})
	if errors.Is(err, errConfirmCode) {
		httpx.ValidationError(w, validate.Errors{"confirm_code": "type the school code to confirm deactivation"})
		return
	}
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

var errConfirmCode = errors.New("confirmation code does not match")
