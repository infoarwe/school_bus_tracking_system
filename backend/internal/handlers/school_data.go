package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// errDryRun rolls back a clear-data preview after counting.
var errDryRun = errors.New("dry run")

type clearDataResult struct {
	DryRun  bool             `json:"dry_run"`
	Scopes  []string         `json:"scopes"`
	Deleted map[string]int64 `json:"deleted"`
}

// ClearSchoolData (Super Admin) deletes a school's test/operational data: the
// chosen parts of trips, notifications, students + parents, routes + stops,
// buses and drivers. The school, its settings, staff users and audit log stay.
// dry_run=true returns the counts without deleting; otherwise the school code
// must be typed as confirm_code (critical action, audited as school.clear_data).
func (a *API) ClearSchoolData(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "schoolID")
	if !ok {
		return
	}
	var req struct {
		Scopes      []string `json:"scopes"`
		DryRun      bool     `json:"dry_run"`
		ConfirmCode string   `json:"confirm_code"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	scopes := map[string]bool{}
	v := validate.New()
	v.Check(len(req.Scopes) > 0, "scopes", "choose what to clear")
	for _, s := range req.Scopes {
		v.OneOf("scopes", s, store.ClearScopes...)
		scopes[s] = true
	}
	for s := range scopes {
		for _, need := range store.ClearScopeNeeds[s] {
			v.Check(scopes[need], "scopes", s+" can only be cleared together with "+need)
		}
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	chosen := make([]string, 0, len(scopes))
	for s := range scopes {
		chosen = append(chosen, s)
	}
	sort.Strings(chosen)

	ctx := r.Context()
	school, err := store.GetSchool(ctx, a.Store.Pool, id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if !req.DryRun && !strings.EqualFold(strings.TrimSpace(req.ConfirmCode), school.Code) {
		httpx.ValidationError(w, validate.Errors{"confirm_code": "type the school code to confirm"})
		return
	}
	if scopes[store.ClearTrips] {
		n, err := store.StartedTripCount(ctx, a.Store.Pool, id)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if n > 0 {
			httpx.Error(w, http.StatusConflict, "trips_in_progress",
				"A trip is in progress. End or cancel running trips before clearing trips.")
			return
		}
	}

	var snapshots []string
	if !req.DryRun && scopes[store.ClearTrips] {
		if snapshots, err = store.RouteSnapshotTripIDs(ctx, a.Store.Pool, id); err != nil {
			internalError(w, r, err)
			return
		}
	}
	// Parents' phones leave their push topics before their logins are removed
	// (best effort, like logout: a Firebase failure does not stop the clear).
	if !req.DryRun && scopes[store.ClearStudents] && a.Senders != nil {
		tokens, err := store.SchoolParentDeviceTokens(ctx, a.Store.Pool, id)
		if err != nil {
			internalError(w, r, err)
			return
		}
		for _, tok := range tokens {
			if err := notify.UnsubscribeAllForToken(ctx, a.Store.Pool, a.Senders, id, tok); err != nil {
				slog.Warn("clear data: topic unsubscribe failed", "err", err, "school_id", id)
			}
		}
	}

	res := clearDataResult{DryRun: req.DryRun, Scopes: chosen}
	err = a.Store.InTx(ctx, func(q store.DBTX) error {
		if res.Deleted, err = store.ClearSchoolData(ctx, q, id, scopes); err != nil {
			return err
		}
		if req.DryRun {
			return errDryRun
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &id, Action: "school.clear_data", EntityType: "school", EntityID: &id,
			After: map[string]any{"scopes": chosen, "deleted": res.Deleted},
		})
	})
	if err != nil && !errors.Is(err, errDryRun) {
		internalError(w, r, err)
		return
	}
	// Route images of deleted trips (files, after commit).
	if a.Files != nil {
		for _, tripID := range snapshots {
			if err := a.Files.Delete(ctx, routeImageKey(id, tripID)); err != nil {
				slog.Warn("clear data: route image not deleted", "err", err, "trip_id", tripID)
			}
		}
	}
	httpx.JSON(w, http.StatusOK, res)
}
