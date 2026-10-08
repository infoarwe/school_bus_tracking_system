package handlers

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

const (
	otpMaxAttempts = 5
	otpMaxPerHour  = 5
	otpMinInterval = 30 * time.Second
)

type tokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	ExpiresIn    int          `json:"expires_in"` // access token lifetime, seconds
	User         *models.User `json:"user"`
}

type loginResponse struct {
	MFARequired bool   `json:"mfa_required"`
	MFAToken    string `json:"mfa_token,omitempty"`
	*tokenResponse
}

// Login: email + password, for Super Admin, School Admin and Transport Manager.
func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		DeviceName string `json:"device_name"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	v := validate.New()
	v.Required("email", req.Email)
	v.Required("password", req.Password)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}

	u, err := store.GetUserByEmail(r.Context(), a.Store.Pool, strings.TrimSpace(req.Email))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		internalError(w, r, err)
		return
	}
	var hash *string
	if u != nil && u.Role.IsWeb() {
		hash = u.PasswordHash
	}
	if !auth.CheckPassword(hash, req.Password) {
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect.")
		return
	}
	if !a.checkLoginAllowed(w, r, u) {
		return
	}

	if u.TOTPEnabled {
		mfa, err := a.Tokens.IssueMFA(u)
		if err != nil {
			internalError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, loginResponse{MFARequired: true, MFAToken: mfa})
		return
	}
	a.startSession(w, r, u, req.DeviceName, func(t *tokenResponse) {
		httpx.JSON(w, http.StatusOK, loginResponse{tokenResponse: t})
	})
}

// Login2FA completes a password login for an account with 2FA enabled.
func (a *API) Login2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MFAToken   string `json:"mfa_token"`
		Code       string `json:"code"`
		DeviceName string `json:"device_name"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	claims, err := a.Tokens.ParseMFA(req.MFAToken)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "mfa_token_invalid", "Login expired. Please start again.")
		return
	}
	u, err := store.GetUser(r.Context(), a.Store.Pool, claims.Subject)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if u.TOTPSecret == nil || !auth.ValidateTOTP(strings.TrimSpace(req.Code), *u.TOTPSecret) {
		httpx.Error(w, http.StatusUnauthorized, "invalid_2fa_code", "The authentication code is incorrect.")
		return
	}
	if !a.checkLoginAllowed(w, r, u) {
		return
	}
	a.startSession(w, r, u, req.DeviceName, func(t *tokenResponse) {
		httpx.JSON(w, http.StatusOK, loginResponse{tokenResponse: t})
	})
}

type otpRequest struct {
	Mobile     string `json:"mobile"`
	App        string `json:"app"` // "driver" or "parent"
	Code       string `json:"code"`
	DeviceName string `json:"device_name"`
}

func (req *otpRequest) validate(withCode bool) *validate.V {
	v := validate.New()
	v.Required("mobile", req.Mobile)
	v.Mobile("mobile", &req.Mobile)
	v.OneOf("app", req.App, string(models.RoleDriver), string(models.RoleParent))
	if withCode {
		v.Required("code", req.Code)
	}
	return v
}

// SendOTP sends a login code to a driver or parent. The response is the same
// whether or not the number is registered, so numbers cannot be enumerated.
func (a *API) SendOTP(w http.ResponseWriter, r *http.Request) {
	var req otpRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if v := req.validate(false); !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	role := models.Role(req.App)
	ctx := r.Context()

	stats, err := store.GetOTPStats(ctx, a.Store.Pool, req.Mobile, role)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if stats.LastSentAt != nil && time.Since(*stats.LastSentAt) < otpMinInterval {
		httpx.Error(w, http.StatusTooManyRequests, "otp_too_soon", "Please wait 30 seconds before requesting another code.")
		return
	}
	if stats.SentLastHour >= otpMaxPerHour {
		httpx.Error(w, http.StatusTooManyRequests, "otp_rate_limited", "Too many codes requested. Try again later.")
		return
	}

	sent := map[string]any{"sent": true, "expires_in": int(a.OTPTTL.Seconds())}
	u, err := store.GetUserByMobile(ctx, a.Store.Pool, req.Mobile, role)
	if errors.Is(err, store.ErrNotFound) || (u != nil && u.Status != models.StatusActive) {
		httpx.JSON(w, http.StatusOK, sent)
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}

	code := a.OTPDevCode
	if code == "" {
		if code, err = auth.GenerateOTP(); err != nil {
			internalError(w, r, err)
			return
		}
	}
	if err := store.CreateOTP(ctx, a.Store.Pool, req.Mobile, role, otpHash(req.Mobile, code), time.Now().Add(a.OTPTTL)); err != nil {
		internalError(w, r, err)
		return
	}
	if err := a.SMS.SendOTP(ctx, req.Mobile, code); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sent)
}

// VerifyOTP exchanges a valid code for tokens.
func (a *API) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req otpRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if v := req.validate(true); !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	role := models.Role(req.App)
	ctx := r.Context()

	otp, err := store.GetActiveOTP(ctx, a.Store.Pool, req.Mobile, role)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusUnauthorized, "otp_invalid", "The code is incorrect or has expired.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	if otp.Attempts >= otpMaxAttempts {
		_ = store.ConsumeOTP(ctx, a.Store.Pool, otp.ID)
		httpx.Error(w, http.StatusUnauthorized, "otp_attempts_exceeded", "Too many wrong attempts. Request a new code.")
		return
	}
	if subtle.ConstantTimeCompare([]byte(otp.CodeHash), []byte(otpHash(req.Mobile, strings.TrimSpace(req.Code)))) != 1 {
		_ = store.IncrementOTPAttempts(ctx, a.Store.Pool, otp.ID)
		httpx.Error(w, http.StatusUnauthorized, "otp_invalid", "The code is incorrect or has expired.")
		return
	}
	if err := store.ConsumeOTP(ctx, a.Store.Pool, otp.ID); err != nil {
		internalError(w, r, err)
		return
	}

	u, err := store.GetUserByMobile(ctx, a.Store.Pool, req.Mobile, role)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if !a.checkLoginAllowed(w, r, u) {
		return
	}
	a.startSession(w, r, u, req.DeviceName, func(t *tokenResponse) { httpx.JSON(w, http.StatusOK, t) })
}

// Refresh rotates the refresh token: the old one stops working.
func (a *API) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	ctx := r.Context()

	plain, hash, err := auth.NewRefreshToken()
	if err != nil {
		internalError(w, r, err)
		return
	}
	sessionID, userID, err := store.RotateRefreshToken(ctx, a.Store.Pool, auth.HashToken(req.RefreshToken), hash,
		time.Now().Add(a.Tokens.RefreshTTL))
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusUnauthorized, "refresh_token_invalid", "Session expired. Please log in again.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	u, err := store.GetUser(ctx, a.Store.Pool, userID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if !a.checkLoginAllowed(w, r, u) {
		_ = store.RevokeSession(ctx, a.Store.Pool, u.ID, sessionID)
		return
	}
	access, err := a.Tokens.IssueAccess(u, sessionID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a.tokenResponse(access, plain, u))
}

func (a *API) Logout(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	err := a.Store.InTx(r.Context(), func(q store.DBTX) error {
		if err := store.RevokeSession(r.Context(), q, p.UserID, p.SessionID); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: p.SchoolID, Action: "auth.logout", EntityType: "user", EntityID: &p.UserID,
		})
	})
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		internalError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// Me returns the caller's profile and school. Apps and web call it after login and on start-up.
func (a *API) Me(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	u, err := store.GetUser(r.Context(), a.Store.Pool, p.UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	var school *models.School
	if u.SchoolID != nil {
		if school, err = store.GetSchool(r.Context(), a.Store.Pool, *u.SchoolID); err != nil {
			internalError(w, r, err)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"user":                      u,
		"school":                    school,
		"two_factor_setup_required": a.RequireSuperAdmin2FA && u.Role == models.RoleSuperAdmin && !u.TOTPEnabled,
	})
}

func (a *API) ListSessions(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	sessions, err := store.ListActiveSessions(r.Context(), a.Store.Pool, p.UserID, p.SessionID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sessions)
}

func (a *API) RevokeSession(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "sessionID")
	if !ok {
		return
	}
	p := auth.FromContext(r.Context())
	if err := store.RevokeSession(r.Context(), a.Store.Pool, p.UserID, id); err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.NoContent(w)
}

// SetupTOTP creates a new authenticator secret. It is only active after EnableTOTP.
func (a *API) SetupTOTP(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	u, err := store.GetUser(r.Context(), a.Store.Pool, p.UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if u.TOTPEnabled {
		httpx.Error(w, http.StatusConflict, "2fa_already_enabled", "Two-factor authentication is already enabled.")
		return
	}
	account := u.Name
	if u.Email != nil {
		account = *u.Email
	}
	setup, err := auth.NewTOTP(account)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := store.SetUserTOTPSecret(r.Context(), a.Store.Pool, u.ID, setup.Secret); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, setup)
}

func (a *API) EnableTOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	p := auth.FromContext(r.Context())
	u, err := store.GetUser(r.Context(), a.Store.Pool, p.UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if u.TOTPEnabled {
		httpx.Error(w, http.StatusConflict, "2fa_already_enabled", "Two-factor authentication is already enabled.")
		return
	}
	if u.TOTPSecret == nil {
		httpx.Error(w, http.StatusBadRequest, "2fa_not_set_up", "Start two-factor setup first.")
		return
	}
	if !auth.ValidateTOTP(strings.TrimSpace(req.Code), *u.TOTPSecret) {
		httpx.Error(w, http.StatusBadRequest, "invalid_2fa_code", "The authentication code is incorrect.")
		return
	}
	err = a.Store.InTx(r.Context(), func(q store.DBTX) error {
		if err := store.EnableUserTOTP(r.Context(), q, u.ID); err != nil {
			return err
		}
		return a.audit(r.Context(), q, r, store.AuditEntry{
			SchoolID: u.SchoolID, Action: "auth.2fa_enabled", EntityType: "user", EntityID: &u.ID,
		})
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// checkLoginAllowed rejects suspended users and users of inactive schools.
func (a *API) checkLoginAllowed(w http.ResponseWriter, r *http.Request, u *models.User) bool {
	if u.Status != models.StatusActive {
		httpx.Error(w, http.StatusForbidden, "account_suspended", "Your account is suspended. Contact your school.")
		return false
	}
	if u.SchoolID != nil {
		s, err := store.GetSchool(r.Context(), a.Store.Pool, *u.SchoolID)
		if err != nil {
			internalError(w, r, err)
			return false
		}
		if s.Status != models.StatusActive {
			httpx.Error(w, http.StatusForbidden, "school_inactive", "Your school's account is inactive.")
			return false
		}
	}
	return true
}

// startSession creates a session, records the login and passes the tokens to respond.
func (a *API) startSession(w http.ResponseWriter, r *http.Request, u *models.User, deviceName string, respond func(*tokenResponse)) {
	ctx := r.Context()
	plain, hash, err := auth.NewRefreshToken()
	if err != nil {
		internalError(w, r, err)
		return
	}
	var sessionID string
	err = a.Store.InTx(ctx, func(q store.DBTX) error {
		sessionID, err = store.CreateSession(ctx, q, store.NewSession{
			UserID:           u.ID,
			RefreshTokenHash: hash,
			DeviceName:       truncate(deviceName, 100),
			UserAgent:        truncate(r.UserAgent(), 300),
			IP:               clientIP(r),
			ExpiresAt:        time.Now().Add(a.Tokens.RefreshTTL),
		})
		if err != nil {
			return err
		}
		if err := store.TouchUserLogin(ctx, q, u.ID); err != nil {
			return err
		}
		// The caller is not authenticated yet, so set the actor explicitly.
		actorCtx := auth.WithPrincipal(ctx, &auth.Principal{UserID: u.ID, Role: u.Role, SchoolID: u.SchoolID})
		return a.audit(actorCtx, q, r, store.AuditEntry{
			SchoolID: u.SchoolID, Action: "auth.login", EntityType: "user", EntityID: &u.ID,
		})
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	access, err := a.Tokens.IssueAccess(u, sessionID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	respond(a.tokenResponse(access, plain, u))
}

func (a *API) tokenResponse(access, refresh string, u *models.User) *tokenResponse {
	return &tokenResponse{
		AccessToken: access, RefreshToken: refresh, TokenType: "Bearer",
		ExpiresIn: int(a.Tokens.AccessTTL.Seconds()), User: u,
	}
}

func otpHash(mobile, code string) string { return auth.HashToken(mobile + ":" + code) }

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
