package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// PushKeyChecker asks Google for an access token with a service-account JSON
// (proves the key works). Replaceable in tests.
type PushKeyChecker func(ctx context.Context, credentialsJSON []byte) error

// CheckFCMKey is the real checker.
func CheckFCMKey(ctx context.Context, raw []byte) error {
	s, err := notify.NewFCMSenderJSON(ctx, raw)
	if err != nil {
		return err
	}
	return s.CheckAuth(ctx)
}

// pushSettingsResponse never includes the key itself.
type pushSettingsResponse struct {
	Configured  bool       `json:"configured"`
	ProjectID   string     `json:"project_id"`
	ClientEmail string     `json:"client_email"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

func pushView(p *store.PushSettings) pushSettingsResponse {
	return pushSettingsResponse{Configured: p.CredentialsEncrypted != nil, ProjectID: p.ProjectID,
		ClientEmail: p.ClientEmail, UpdatedAt: p.UpdatedAt}
}

// GetPushSettings: which Firebase project the school's pushes go through.
func (a *API) GetPushSettings(w http.ResponseWriter, r *http.Request) {
	p, err := store.GetPushSettings(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"))
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, pushView(p))
}

// UpdatePushSettings stores the school's Firebase service-account key (the JSON
// file's contents), encrypted. Super Admin or School Admin.
func (a *API) UpdatePushSettings(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req struct {
		ServiceAccountJSON string `json:"service_account_json"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if len(req.ServiceAccountJSON) > 16<<10 {
		httpx.ValidationError(w, validate.Errors{"service_account_json": "the file is too large for a Firebase key"})
		return
	}
	sa, err := notify.ParseServiceAccount([]byte(req.ServiceAccountJSON))
	if bad, ok := notify.IsBadServiceAccount(err); ok {
		httpx.ValidationError(w, validate.Errors{"service_account_json": bad.Reason})
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	enc, err := a.Secrets.Encrypt(req.ServiceAccountJSON)
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.savePush(w, r, schoolID, enc, sa.ProjectID, sa.ClientEmail, "settings.push_key_upload")
}

// DeletePushSettings removes the school's key (pushes fall back to the server-wide key, or the log).
func (a *API) DeletePushSettings(w http.ResponseWriter, r *http.Request) {
	a.savePush(w, r, chi.URLParam(r, "schoolID"), nil, "", "", "settings.push_key_remove")
}

func (a *API) savePush(w http.ResponseWriter, r *http.Request, schoolID string, enc []byte, projectID, email, action string) {
	ctx := r.Context()
	var after *store.PushSettings
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		if err := store.SetPushSettings(ctx, q, schoolID, enc, projectID, email); err != nil {
			return err
		}
		var err error
		if after, err = store.GetPushSettings(ctx, q, schoolID); err != nil {
			return err
		}
		// The audit log records which project, never the key.
		return a.audit(ctx, q, r, store.AuditEntry{SchoolID: &schoolID, Action: action, EntityType: "school",
			EntityID: &schoolID, After: map[string]string{"project_id": projectID, "client_email": email}})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, pushView(after))
}

// TestPushSettings checks the stored key with Google (gets an access token).
func (a *API) TestPushSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := store.GetPushSettings(ctx, a.Store.Pool, chi.URLParam(r, "schoolID"))
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if p.CredentialsEncrypted == nil {
		httpx.Error(w, http.StatusConflict, "push_not_configured", "Upload the school's Firebase key first.")
		return
	}
	raw, err := a.Secrets.Decrypt(p.CredentialsEncrypted)
	if err != nil {
		internalError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := a.CheckPushKey(ctx, []byte(raw)); err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "message": "Google rejected this key: " + err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "message": "The key works with Firebase project " + p.ProjectID + "."})
}
