package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// Authenticate requires a valid access token, then re-checks the session, user
// and school in the database so logout, suspension and school deactivation
// take effect on the next request. Role and school come from the database,
// not from the token.
func Authenticate(tokens *auth.TokenManager, st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || raw == "" {
				httpx.Error(w, http.StatusUnauthorized, "unauthorized", "Login required.")
				return
			}
			claims, err := tokens.ParseAccess(raw)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "token_invalid", "Session expired. Please log in again.")
				return
			}

			state, err := store.GetSessionState(r.Context(), st.Pool, claims.SessionID, claims.Subject)
			if errors.Is(err, store.ErrNotFound) {
				httpx.Error(w, http.StatusUnauthorized, "session_revoked", "Session ended. Please log in again.")
				return
			}
			if err != nil {
				slog.Error("load session state", "err", err)
				httpx.Error(w, http.StatusInternalServerError, "internal_error", "Something went wrong.")
				return
			}
			if state.UserStatus != models.StatusActive {
				httpx.Error(w, http.StatusForbidden, "account_suspended", "Your account is suspended.")
				return
			}
			if state.SchoolStatus != nil && *state.SchoolStatus != models.StatusActive {
				httpx.Error(w, http.StatusForbidden, "school_inactive", "Your school's account is inactive.")
				return
			}

			p := &auth.Principal{
				UserID:      claims.Subject,
				Role:        state.Role,
				SchoolID:    state.SchoolID,
				SessionID:   claims.SessionID,
				TOTPEnabled: state.TOTPEnabled,
			}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		})
	}
}

// RequireRoles allows only the listed roles. Must run after Authenticate.
func RequireRoles(roles ...models.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.FromContext(r.Context())
			if p == nil || !p.HasRole(roles...) {
				httpx.Error(w, http.StatusForbidden, "forbidden", "You do not have permission for this action.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SchoolScope guards every /schools/{schoolID}/... route: non-Super-Admin callers
// may only reach their own school. Other schools answer 404, not 403, so their
// existence is not revealed.
func SchoolScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "schoolID")
		p := auth.FromContext(r.Context())
		if _, err := uuid.Parse(id); err != nil || p == nil || !p.CanAccessSchool(id) {
			httpx.Error(w, http.StatusNotFound, "not_found", "School not found.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSuperAdmin2FA blocks a Super Admin who has not enabled 2FA from
// everything except the routes needed to set it up (mounted outside this group).
func RequireSuperAdmin2FA(enforce bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.FromContext(r.Context())
			if enforce && p != nil && p.IsSuperAdmin() && !p.TOTPEnabled {
				httpx.Error(w, http.StatusForbidden, "2fa_setup_required",
					"Set up two-factor authentication to continue.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
