package router_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/handlers"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/ratelimit"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/router"
)

// doFrom is e.do from a given client address (and optional X-Forwarded-For) on handler h.
func doFrom(h http.Handler, remote, xff, method, path string, body any) (resp, string) {
	req := newJSONRequest(method, path, "", body)
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return decodeResp(rec), rec.Header().Get("Retry-After")
}

func TestRateLimits(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	e.user(&a, models.RoleDriver, "", "+919000000201")
	rules := &e.api.Limiter.Rules

	// Per account: password guessing is capped whatever the IP (and the email's case).
	rules.LoginAccount = ratelimit.Rule{Name: "login_account", Limit: 3, Window: 15 * time.Minute}
	wrong := map[string]string{"email": "Admin@A.test", "password": "wrong-password"}
	for i := range 3 {
		r, _ := doFrom(e.srv, "10.0.0."+strconv.Itoa(i+1)+":1000", "", "POST", "/api/v1/auth/login", wrong)
		e.expect(r, 401, "invalid_credentials")
	}
	r, retry := doFrom(e.srv, "10.0.0.9:1000", "", "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@a.test", "password": testPassword})
	e.expect(r, 429, "rate_limited")
	if n, _ := strconv.Atoi(retry); n < 1 || n > 900 {
		t.Errorf("Retry-After = %q", retry)
	}
	e.redis.FastForward(16 * time.Minute)
	e.login("admin@a.test") // allowed again after the window

	// Per mobile: OTP verify attempts, however the number is written.
	rules.OTPVerifyMobile = ratelimit.Rule{Name: "otp_verify_mobile", Limit: 2, Window: 15 * time.Minute}
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9000000201", "app": "driver"}), 200, "")
	bad := map[string]string{"mobile": "9000000201", "app": "driver", "code": "000000"}
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", bad), 401, "otp_invalid")
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", bad), 401, "otp_invalid")
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": "+91 90000 00201",
		"app": "driver", "code": devOTP}), 429, "rate_limited")

	// Per IP: OTP sends (SMS cost), even to unregistered numbers.
	rules.OTPSendIP = ratelimit.Rule{Name: "otp_send_ip", Limit: 2, Window: time.Hour}
	for i, want := range []int{200, 200, 429} {
		r, _ := doFrom(e.srv, "10.1.1.1:5", "", "POST", "/api/v1/auth/otp/send",
			map[string]string{"mobile": "98000000" + strconv.Itoa(10+i), "app": "parent"})
		if r.Status != want {
			t.Fatalf("otp send %d: got %d, want %d", i+1, r.Status, want)
		}
	}

	// Per IP: all auth endpoints share one budget.
	rules.Auth = ratelimit.Rule{Name: "auth", Limit: 2, Window: 15 * time.Minute}
	for _, want := range []int{401, 401, 429} {
		r, _ := doFrom(e.srv, "10.2.2.2:5", "", "POST", "/api/v1/auth/refresh", map[string]string{"refresh_token": "x"})
		if r.Status != want {
			t.Fatalf("refresh: got %d, want %d", r.Status, want)
		}
	}
	rules.Auth = ratelimit.DefaultRules().Auth

	// Per user: any authenticated endpoint.
	tok := e.login("admin@a.test")
	rules.User = ratelimit.Rule{Name: "user", Limit: 2, Window: time.Minute}
	e.expect(e.do("GET", "/api/v1/auth/me", tok, nil), 200, "")
	e.expect(e.do("GET", "/api/v1/schools/"+a, tok, nil), 200, "")
	e.expect(e.do("GET", "/api/v1/auth/me", tok, nil), 429, "rate_limited")

	// Per IP: every API request, authenticated or not; other IPs are unaffected.
	rules.IP = ratelimit.Rule{Name: "ip", Limit: 2, Window: time.Minute}
	for _, want := range []int{404, 404, 429} {
		if r, _ := doFrom(e.srv, "10.3.3.3:5", "", "GET", "/api/v1/branding/logos/x.png", nil); r.Status != want {
			t.Fatalf("ip limit: got %d, want %d", r.Status, want)
		}
	}
	if r, _ := doFrom(e.srv, "10.3.3.4:5", "", "GET", "/api/v1/branding/logos/x.png", nil); r.Status != 404 {
		t.Errorf("another IP was limited: %d", r.Status)
	}
	// /health is outside the API limits (monitoring).
	if r, _ := doFrom(e.srv, "10.3.3.3:5", "", "GET", "/health", nil); r.Status == 429 {
		t.Error("/health must not be rate limited")
	}
}

// Proxy headers are trusted only when configured: otherwise a client could dodge
// per-IP limits (and fake audit-log IPs) by sending its own X-Forwarded-For.
func TestProxyHeadersTrust(t *testing.T) {
	e := newEnv(t)
	e.api.Limiter.Rules.IP = ratelimit.Rule{Name: "ip", Limit: 1, Window: time.Minute}
	trusted := router.New(router.Deps{Health: &handlers.HealthHandler{}, API: e.api, RequireSuperAdmin2FA: true,
		TrustProxyHeaders: true})
	call := func(h http.Handler, xff string) int {
		r, _ := doFrom(h, "10.9.9.9:1", xff, "GET", "/api/v1/branding/logos/x.png", nil)
		return r.Status
	}

	// Untrusted: the header is ignored, so changing it does not get a fresh budget.
	if call(e.srv, "1.1.1.1") != 404 || call(e.srv, "2.2.2.2") != 429 {
		t.Error("X-Forwarded-For must be ignored when TRUST_PROXY_HEADERS is off")
	}
	// Trusted (behind Nginx, which overwrites the header): each client IP has its own budget.
	if call(trusted, "3.3.3.3") != 404 || call(trusted, "4.4.4.4") != 404 || call(trusted, "3.3.3.3") != 429 {
		t.Error("X-Forwarded-For must set the client IP when TRUST_PROXY_HEADERS is on")
	}
}
