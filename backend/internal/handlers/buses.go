package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

var (
	vehicleNumberRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9 -]{3,19}$`)
	spacesRe        = regexp.MustCompile(`\s+`)
	busConflicts    = map[string]string{"buses_school_vehicle_key": "vehicle_number"}
)

type busRequest struct {
	VehicleNumber string `json:"vehicle_number"`
	Capacity      int    `json:"capacity"`
	MakeModel     string `json:"make_model"`
	GPSDeviceID   string `json:"gps_device_id"`
	Notes         string `json:"notes"`
}

func (req *busRequest) toInput() (store.BusInput, validate.Errors) {
	req.VehicleNumber = spacesRe.ReplaceAllString(strings.ToUpper(strings.TrimSpace(req.VehicleNumber)), " ")
	v := validate.New()
	v.Check(vehicleNumberRe.MatchString(req.VehicleNumber), "vehicle_number", "must be 4-20 letters, digits, spaces or hyphens, e.g. TN-38-AB-1234")
	v.Check(req.Capacity >= 1 && req.Capacity <= 100, "capacity", "must be between 1 and 100")
	v.MaxLen("make_model", req.MakeModel, 100)
	v.MaxLen("gps_device_id", req.GPSDeviceID, 100)
	v.MaxLen("notes", req.Notes, 1000)
	return store.BusInput{
		VehicleNumber: req.VehicleNumber, Capacity: req.Capacity, MakeModel: strings.TrimSpace(req.MakeModel),
		GPSDeviceID: strings.TrimSpace(req.GPSDeviceID), Notes: req.Notes,
	}, v.Errors()
}

func (a *API) ListBuses(w http.ResponseWriter, r *http.Request) {
	pg := page(r)
	buses, total, err := store.ListBuses(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), listFilter(r), pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.List(w, buses, meta(pg, total))
}

func (a *API) GetBus(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "busID")
	if !ok {
		return
	}
	b, err := store.GetBus(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, b)
}

func (a *API) CreateBus(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req busRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	var b *models.Bus
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		var err error
		if b, err = store.CreateBus(r.Context(), q, schoolID, in); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "bus.create", EntityType: "bus", EntityID: &b.ID, After: b,
		})
	})
	if err != nil {
		storeError(w, r, err, busConflicts)
		return
	}
	httpx.JSON(w, http.StatusCreated, b)
}

func (a *API) UpdateBus(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "busID")
	if !ok {
		return
	}
	var req busRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	in, errs := req.toInput()
	if len(errs) > 0 {
		httpx.ValidationError(w, errs)
		return
	}
	var after *models.Bus
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		before, err := store.GetBus(r.Context(), q, schoolID, id)
		if err != nil {
			return err
		}
		if after, err = store.UpdateBus(r.Context(), q, schoolID, id, in); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "bus.update", EntityType: "bus", EntityID: &id, Before: before, After: after,
		})
	})
	if err != nil {
		storeError(w, r, err, busConflicts)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}

// SetBusStatus: active, inactive or maintenance. Only active buses can be assigned to trips (Sprint 4).
func (a *API) SetBusStatus(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	id, ok := idParam(w, r, "busID")
	if !ok {
		return
	}
	status, ok := decodeStatus(w, r, models.StatusActive, models.StatusInactive, models.StatusMaintenance)
	if !ok {
		return
	}
	var after *models.Bus
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		before, err := store.GetBus(r.Context(), q, schoolID, id)
		if err != nil {
			return err
		}
		if after, err = store.SetBusStatus(r.Context(), q, schoolID, id, status); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "bus.status_change", EntityType: "bus", EntityID: &id,
			Before: map[string]string{"status": before.Status}, After: map[string]string{"status": status},
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, after)
}
