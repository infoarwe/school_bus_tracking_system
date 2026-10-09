package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/secrets"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// API holds the dependencies shared by the business handlers.
type API struct {
	Store  *store.Store
	Tokens *auth.TokenManager
	SMS    auth.SMSSender
	// Secrets encrypts stored third-party keys (per-school Google Maps server key).
	Secrets *secrets.Box
	// Live is the current bus positions and live events (Redis); Hub fans events out to WebSockets.
	Live *tracking.Live
	Hub  *tracking.Hub
	// ETA computes Google ETAs with each school's server key; nil disables Google (estimate only).
	ETA ETAProvider
	// WSOriginPatterns are the browser origins allowed to open the WebSocket (host[:port]).
	WSOriginPatterns []string

	OTPTTL     time.Duration
	OTPDevCode string
	// RequireSuperAdmin2FA is reported to the client in /auth/me.
	RequireSuperAdmin2FA bool
}

// page reads ?page and ?page_size (default 1 and 20, max 100).
func page(r *http.Request) store.Page {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if p < 1 {
		p = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return store.Page{Page: p, PageSize: size}
}

func meta(p store.Page, total int) httpx.PageMeta {
	return httpx.PageMeta{Page: p.Page, PageSize: p.PageSize, Total: total}
}

// idParam returns a UUID path parameter, writing a 404 if it is malformed.
func idParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := chi.URLParam(r, name)
	if _, err := uuid.Parse(id); err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return "", false
	}
	return id, true
}

// audit records an action by the current caller in the same transaction as the change.
func (a *API) audit(ctx context.Context, q store.DBTX, r *http.Request, e store.AuditEntry) error {
	if p := auth.FromContext(ctx); p != nil {
		e.ActorUserID = &p.UserID
		e.ActorRole = string(p.Role)
	}
	e.IP = clientIP(r)
	e.RequestID = chimw.GetReqID(ctx)
	return store.InsertAudit(ctx, q, e)
}

func clientIP(r *http.Request) string {
	return r.RemoteAddr // chi RealIP middleware has already applied X-Forwarded-For
}

// storeError maps store errors to responses. conflicts maps a unique constraint
// name to the request field it concerns.
func storeError(w http.ResponseWriter, r *http.Request, err error, conflicts map[string]string) {
	var ce *store.ConflictError
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.As(err, &ce):
		field := conflicts[ce.Constraint]
		if field == "" {
			httpx.Error(w, http.StatusConflict, "conflict", "This record already exists.")
			return
		}
		httpx.ErrorWithFields(w, http.StatusConflict, httpx.ErrorBody{
			Code: "conflict", Message: "A record with this " + field + " already exists.",
			Fields: map[string]string{field: "already exists"},
		})
	default:
		internalError(w, r, err)
	}
}

func internalError(w http.ResponseWriter, r *http.Request, err error) {
	internalErrorLog(r, err)
	httpx.Error(w, http.StatusInternalServerError, "internal_error", "Something went wrong.")
}

func internalErrorLog(r *http.Request, err error) {
	slog.Error("request failed", "request_id", chimw.GetReqID(r.Context()), "path", r.URL.Path, "err", err)
}

// listFilter reads the common ?q= and ?status= list parameters.
func listFilter(r *http.Request) store.ListFilter {
	return store.ListFilter{Q: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status")}
}

// decodeStatus reads {"status": "..."} and checks it is one of allowed, writing a 400 if not.
func decodeStatus(w http.ResponseWriter, r *http.Request, allowed ...string) (string, bool) {
	var req struct {
		Status string `json:"status"`
	}
	if !httpx.Decode(w, r, &req) {
		return "", false
	}
	v := validate.New()
	v.OneOf("status", req.Status, allowed...)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return "", false
	}
	return req.Status, true
}
