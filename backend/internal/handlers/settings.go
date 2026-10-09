package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// Google API keys look like "AIza" followed by 35 URL-safe characters.
var googleKeyRe = regexp.MustCompile(`^AIza[0-9A-Za-z_-]{35}$`)

type mapsSettingsResponse struct {
	// Used by the admin web to load Google Maps. Empty = the web falls back to OpenStreetMap.
	BrowserKey string `json:"browser_key"`
	// The server key is never returned; only whether one is set and its last 4 characters.
	ServerKeySet  bool   `json:"server_key_set"`
	ServerKeyHint string `json:"server_key_hint"`
}

func (a *API) mapsResponse(m *store.MapsSettings) (*mapsSettingsResponse, error) {
	res := &mapsSettingsResponse{BrowserKey: m.BrowserKey}
	if m.ServerKeyEncrypted != nil {
		key, err := a.Secrets.Decrypt(m.ServerKeyEncrypted)
		if err != nil {
			return nil, err
		}
		res.ServerKeySet = true
		res.ServerKeyHint = "…" + key[len(key)-4:]
	}
	return res, nil
}

// GetMapsSettings: any staff of the school (they all need the browser key to see maps).
func (a *API) GetMapsSettings(w http.ResponseWriter, r *http.Request) {
	m, err := store.GetMapsSettings(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"))
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	res, err := a.mapsResponse(m)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// UpdateMapsSettings: Super Admin or School Admin. server_key omitted or null keeps
// the stored key; "" removes it.
func (a *API) UpdateMapsSettings(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req struct {
		BrowserKey string  `json:"browser_key"`
		ServerKey  *string `json:"server_key"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	const keyMsg = "must be a Google API key (starts with AIza, 39 characters)"
	req.BrowserKey = strings.TrimSpace(req.BrowserKey)
	v := validate.New()
	v.Check(req.BrowserKey == "" || googleKeyRe.MatchString(req.BrowserKey), "browser_key", keyMsg)
	if req.ServerKey != nil {
		*req.ServerKey = strings.TrimSpace(*req.ServerKey)
		v.Check(*req.ServerKey == "" || googleKeyRe.MatchString(*req.ServerKey), "server_key", keyMsg)
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}

	ctx := r.Context()
	var res *mapsSettingsResponse
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetMapsSettings(ctx, q, schoolID)
		if err != nil {
			return err
		}
		after := store.MapsSettings{BrowserKey: req.BrowserKey, ServerKeyEncrypted: before.ServerKeyEncrypted}
		if req.ServerKey != nil {
			after.ServerKeyEncrypted = nil
			if *req.ServerKey != "" {
				if after.ServerKeyEncrypted, err = a.Secrets.Encrypt(*req.ServerKey); err != nil {
					return err
				}
			}
		}
		if err := store.SetMapsSettings(ctx, q, schoolID, after); err != nil {
			return err
		}
		if res, err = a.mapsResponse(&after); err != nil {
			return err
		}
		// Never put key values in the audit log.
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "settings.maps_update", EntityType: "school", EntityID: &schoolID,
			After: map[string]bool{
				"browser_key_changed": before.BrowserKey != after.BrowserKey,
				"server_key_changed":  req.ServerKey != nil,
				"server_key_set":      after.ServerKeyEncrypted != nil,
			},
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}
