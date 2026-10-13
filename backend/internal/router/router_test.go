package router_test

// Integration tests: the full HTTP stack against a real Postgres.
// Set TEST_DATABASE_URL (the tables are wiped), e.g.
//   TEST_DATABASE_URL=postgres://sbts:sbts@localhost:5433/sbts_test?sslmode=disable go test ./...

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/database"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/handlers"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/ratelimit"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/router"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/secrets"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/storage"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

const (
	testPassword     = "Password@123"
	testMaxLogoBytes = 64 << 10
)

type env struct {
	t      *testing.T
	srv    http.Handler
	pool   *pgxpool.Pool
	live   *tracking.Live
	redis  *miniredis.Miniredis
	eta    *fakeETA
	pushes *fakePush
	worker *notify.Worker
	api    *handlers.API
}

func testSecrets(t *testing.T) *secrets.Box {
	b, err := secrets.New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newEnv(t *testing.T) *env {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	if err := database.Migrate(url, "up"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `TRUNCATE audit_logs, otp_codes, user_sessions, users, schools CASCADE`); err != nil {
		t.Fatal(err)
	}

	// Redis in memory: no server needed for tests.
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	live := tracking.NewLive(rdb)
	hub := tracking.NewHub(live)
	hubCtx, stopHub := context.WithCancel(context.Background())
	t.Cleanup(stopHub)
	go hub.Run(hubCtx)

	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })

	eta := &fakeETA{}
	pushes := &fakePush{}
	api := &handlers.API{
		Store:                store.New(pool),
		Tokens:               auth.NewTokenManager("test-secret-test-secret-test-secret", time.Minute, time.Hour),
		SMS:                  auth.LogSender{},
		OTPTTL:               5 * time.Minute,
		Secrets:              testSecrets(t),
		Live:                 live,
		Hub:                  hub,
		ETA:                  eta,
		CheckPushKey:         func(context.Context, []byte) error { return nil },
		Limiter:              ratelimit.New(rdb, ratelimit.DefaultRules()),
		Files:                files,
		MaxLogoBytes:         testMaxLogoBytes,
		RequireSuperAdmin2FA: true,
	}
	srv := router.New(router.Deps{Health: &handlers.HealthHandler{}, API: api, RequireSuperAdmin2FA: true})
	return &env{t: t, srv: srv, pool: pool, live: live, redis: mr, eta: eta, pushes: pushes, api: api,
		worker: &notify.Worker{Store: store.New(pool), Senders: notify.Static{Sender: pushes}}}
}

type resp struct {
	Status int
	Body   map[string]any
}

func (r resp) data() map[string]any { m, _ := r.Body["data"].(map[string]any); return m }
func (r resp) errCode() string {
	e, _ := r.Body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (e *env) do(method, path, token string, body any) resp {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, newJSONRequest(method, path, token, body))
	return decodeResp(rec)
}

func newJSONRequest(method, path, token string, body any) *http.Request {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func decodeResp(rec *httptest.ResponseRecorder) resp {
	out := resp{Status: rec.Code}
	_ = json.Unmarshal(rec.Body.Bytes(), &out.Body)
	return out
}

func (e *env) expect(r resp, status int, code string) {
	e.t.Helper()
	if r.Status != status || (code != "" && r.errCode() != code) {
		e.t.Fatalf("got %d %q, want %d %q; body=%v", r.Status, r.errCode(), status, code, r.Body)
	}
}

func (e *env) school(code string) string {
	e.t.Helper()
	s, err := store.CreateSchool(context.Background(), e.pool, store.SchoolInput{
		Name: code + " School", Code: code, WorkingDays: []string{"mon"}, Timezone: "Asia/Kolkata",
		TransportConfig: []byte(`{}`),
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return s.ID
}

func (e *env) user(schoolID *string, role models.Role, email, mobile string) string {
	e.t.Helper()
	in := store.UserInput{SchoolID: schoolID, Role: role, Name: string(role)}
	if email != "" {
		h, _ := auth.HashPassword(testPassword)
		in.Email, in.PasswordHash = &email, &h
	}
	if mobile != "" {
		in.Mobile = &mobile
	}
	u, err := store.CreateUser(context.Background(), e.pool, in)
	if err != nil {
		e.t.Fatal(err)
	}
	return u.ID
}

func (e *env) login(email string) string {
	e.t.Helper()
	r := e.do("POST", "/api/v1/auth/login", "", map[string]string{"email": email, "password": testPassword})
	e.expect(r, 200, "")
	tok, _ := r.data()["access_token"].(string)
	if tok == "" {
		e.t.Fatalf("no access token: %v", r.Body)
	}
	return tok
}

func TestTenantIsolation(t *testing.T) {
	e := newEnv(t)
	a, b := e.school("AAA"), e.school("BBB")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	tok := e.login("admin@a.test")

	e.expect(e.do("GET", "/api/v1/schools/"+a, tok, nil), 200, "")
	e.expect(e.do("GET", "/api/v1/schools/"+a+"/users", tok, nil), 200, "")

	// Another school's records look like they do not exist.
	e.expect(e.do("GET", "/api/v1/schools/"+b, tok, nil), 404, "not_found")
	e.expect(e.do("GET", "/api/v1/schools/"+b+"/users", tok, nil), 404, "not_found")
	e.expect(e.do("POST", "/api/v1/schools/"+b+"/users", tok, map[string]string{
		"name": "x", "email": "x@b.test", "role": "transport_manager", "password": testPassword,
	}), 404, "not_found")

	// Only Super Admin lists or creates schools.
	e.expect(e.do("GET", "/api/v1/schools", tok, nil), 403, "forbidden")
	e.expect(e.do("PUT", "/api/v1/schools/"+a, tok, map[string]string{"name": "x", "code": "AAA"}), 403, "forbidden")
}

func TestUserManagementPermissions(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	admin := e.login("admin@a.test")
	base := "/api/v1/schools/" + a + "/users"

	created := e.do("POST", base, admin, map[string]string{
		"name": "TM", "email": "TM@a.test", "role": "transport_manager", "password": testPassword,
	})
	e.expect(created, 201, "")
	if got := created.data()["email"]; got != "tm@a.test" {
		t.Errorf("email not normalized: %v", got)
	}
	tmID := created.data()["id"].(string)

	// School Admin cannot create another School Admin (Super Admin only).
	e.expect(e.do("POST", base, admin, map[string]string{
		"name": "SA", "email": "sa2@a.test", "role": "school_admin", "password": testPassword,
	}), 403, "forbidden")
	// Duplicate email reports the field.
	dup := e.do("POST", base, admin, map[string]string{
		"name": "TM", "email": "tm@a.test", "role": "transport_manager", "password": testPassword,
	})
	e.expect(dup, 409, "conflict")
	// Validation errors list the fields.
	bad := e.do("POST", base, admin, map[string]string{"role": "driver", "password": "short"})
	e.expect(bad, 400, "validation_failed")
	if f, _ := bad.Body["error"].(map[string]any)["fields"].(map[string]any); len(f) < 4 {
		t.Errorf("expected name, email, role, password errors; got %v", f)
	}

	// Transport Manager has no access to user management.
	tm := e.login("tm@a.test")
	e.expect(e.do("GET", base, tm, nil), 403, "forbidden")

	// Suspending ends the TM's sessions and blocks login.
	e.expect(e.do("PATCH", base+"/"+tmID+"/status", admin, map[string]string{"status": "suspended"}), 200, "")
	e.expect(e.do("GET", "/api/v1/auth/me", tm, nil), 401, "session_revoked")
	e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"email": "tm@a.test", "password": testPassword}), 403, "account_suspended")

	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action IN ('user.create', 'user.suspend')`).Scan(&n)
	if n != 2 {
		t.Errorf("audit entries = %d, want 2", n)
	}
}

func TestOTPLoginAndRefresh(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleDriver, "", "+919000000001")

	send := map[string]string{"mobile": "90000 00001", "app": "driver"}
	first := e.do("POST", "/api/v1/auth/otp/send", "", send)
	e.expect(first, 200, "")
	code, _ := first.data()["development_otp"].(string)
	if len(code) != 6 {
		t.Fatalf("development_otp = %q, want a fresh 6-digit code", code)
	}
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", send), 429, "otp_too_soon")
	// Driver login intentionally rejects unknown numbers up front (see SendOTP's doc comment);
	// unknown parent numbers still get the enumeration-safe 200 (TestSendOTPDevelopmentCode).
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9111111111", "app": "driver"}), 403, "driver_not_registered")

	verify := map[string]string{"mobile": "9000000001", "app": "driver", "code": "000000"}
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", verify), 401, "otp_invalid")
	verify["code"] = code
	ok := e.do("POST", "/api/v1/auth/otp/verify", "", verify)
	e.expect(ok, 200, "")
	access := ok.data()["access_token"].(string)
	refresh := ok.data()["refresh_token"].(string)

	// A code works once.
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", verify), 401, "otp_invalid")

	me := e.do("GET", "/api/v1/auth/me", access, nil)
	e.expect(me, 200, "")
	if role := me.data()["user"].(map[string]any)["role"]; role != "driver" {
		t.Errorf("role = %v", role)
	}
	// Drivers have no admin web endpoints, even in their own school.
	e.expect(e.do("GET", "/api/v1/schools/"+a, access, nil), 403, "forbidden")

	// Refresh rotates: the old refresh token stops working.
	r1 := e.do("POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
	e.expect(r1, 200, "")
	e.expect(e.do("POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh}), 401, "refresh_token_invalid")

	// Logout revokes the session.
	e.expect(e.do("POST", "/api/v1/auth/logout", access, nil), 204, "")
	e.expect(e.do("GET", "/api/v1/auth/me", access, nil), 401, "session_revoked")
}

// clearOTPCooldown backdates otp_codes rows for mobile so the next send isn't rejected by the
// 30-second resend cooldown; tests can't wait 30 seconds between every call. mobile must be the
// normalized form (+91XXXXXXXXXX) actually stored in otp_codes - the same form the SMS-dev log
// line prints - not necessarily the raw string a request body used.
func clearOTPCooldown(e *env, mobile string) {
	e.t.Helper()
	tag, err := e.pool.Exec(context.Background(),
		`UPDATE otp_codes SET created_at = now() - interval '1 hour' WHERE mobile = $1`, mobile)
	if err != nil {
		e.t.Fatal(err)
	}
	if tag.RowsAffected() == 0 {
		e.t.Fatalf("clearOTPCooldown: no otp_codes rows matched mobile %q (check normalization, e.g. +91 prefix)", mobile)
	}
}

// TestSendOTPDevelopmentCode covers the development_otp response field: fresh random generation
// (not a fixed/.env-sourced value), exact match with what verify accepts, decoy parity for
// unregistered numbers (anti-enumeration), and absence in production.
func TestSendOTPDevelopmentCode(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleDriver, "", "+919000000002")

	mobile := "9000000002"
	normalizedMobile := "+919000000002" // the form actually stored in otp_codes.mobile after validation
	first := e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "driver"})
	e.expect(first, 200, "")
	code1, _ := first.data()["development_otp"].(string)
	if len(code1) != 6 {
		t.Fatalf("development_otp = %q, want a 6-digit code", code1)
	}
	for _, c := range code1 {
		if c < '0' || c > '9' {
			t.Fatalf("development_otp = %q, want digits only", code1)
		}
	}

	// It is exactly the code /auth/otp/verify accepts - a wrong code first, to show both paths work.
	wrong := e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": "driver", "code": "000000"})
	e.expect(wrong, 401, "otp_invalid")
	ok := e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": "driver", "code": code1})
	e.expect(ok, 200, "")

	// Single-use: the same code cannot be replayed after a successful verify.
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": "driver", "code": code1}), 401, "otp_invalid")

	// A second send also returns a fresh, well-formed code. (That repeated sends are not all the
	// *same* static value, across many numbers, is proven statistically by
	// TestOTPGenerationIsRandomAndSecure - comparing just these two specific draws would be a test
	// that incorrectly assumes two random 6-digit codes can never coincidentally collide.)
	clearOTPCooldown(e, normalizedMobile)
	second := e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "driver"})
	e.expect(second, 200, "")
	if code2, _ := second.data()["development_otp"].(string); len(code2) != 6 {
		t.Fatalf("second development_otp = %q, want a fresh 6-digit code", code2)
	}
	// The cooldown and per-hour rate limit still apply exactly as before this feature.
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "driver"}), 429, "otp_too_soon")

	// An unregistered *parent* number gets the same response shape (development_otp present) so its
	// mere presence/absence cannot be used to discover which numbers are registered - but the decoy
	// value was never stored, so it never verifies. (Driver login's different, intentional behavior
	// for an unregistered number is covered by TestSendOTPDriverNotRegistered.)
	decoy := e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9199999999", "app": "parent"})
	e.expect(decoy, 200, "")
	decoyCode, present := decoy.data()["development_otp"].(string)
	if !present || len(decoyCode) != 6 {
		t.Fatalf("unregistered number's response missing development_otp; want the same shape as a registered number")
	}
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": "9199999999", "app": "parent", "code": decoyCode}), 401, "otp_invalid")

	// Production never returns the code, regardless of any of the above.
	e.api.IsProduction = true
	defer func() { e.api.IsProduction = false }()
	clearOTPCooldown(e, normalizedMobile)
	prod := e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "driver"})
	e.expect(prod, 200, "")
	if _, present := prod.data()["development_otp"]; present {
		t.Fatalf("development_otp must not be present when IsProduction is true; body=%v", prod.Body)
	}
}

// TestSendOTPDriverNotRegistered covers the driver-only product decision: an unregistered or
// inactive driver number gets 403 driver_not_registered and no OTP is ever created for it, while
// an eligible active driver and parent logins of either registration state are unaffected.
func TestSendOTPDriverNotRegistered(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleDriver, "", "+919000000003") // the one active, eligible driver

	var otpCountBefore int
	countOTPs := func(mobile string) int {
		var n int
		if err := e.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM otp_codes WHERE mobile = $1 AND role = 'driver'`, mobile).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// An unregistered driver number: 403, no OTP row created.
	otpCountBefore = countOTPs("+919111111113")
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9111111113", "app": "driver"}), 403, "driver_not_registered")
	if got := countOTPs("+919111111113"); got != otpCountBefore {
		t.Fatalf("otp_codes rows for unregistered driver = %d, want %d (no OTP should be created)", got, otpCountBefore)
	}

	// A registered but inactive driver: same 403, no OTP row created, no account-status distinction leaked.
	suspendedID := e.user(&a, models.RoleDriver, "", "+919000000004")
	if _, err := e.pool.Exec(context.Background(), `UPDATE users SET status = $1 WHERE id = $2`, models.StatusSuspended, suspendedID); err != nil {
		t.Fatal(err)
	}
	otpCountBefore = countOTPs("+919000000004")
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9000000004", "app": "driver"}), 403, "driver_not_registered")
	if got := countOTPs("+919000000004"); got != otpCountBefore {
		t.Fatalf("otp_codes rows for suspended driver = %d, want %d (no OTP should be created)", got, otpCountBefore)
	}

	// An eligible active driver is unaffected: still a normal 200 send.
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9000000003", "app": "driver"}), 200, "")

	// An unregistered *parent* number is unaffected: still the enumeration-safe 200 (not 403).
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9222222222", "app": "parent"}), 200, "")
}

// TestOTPGenerationIsRandomAndSecure proves there is no static/hardcoded OTP anywhere in the
// send/verify flow: every request gets its own fresh random code, a new code invalidates the
// previous one, expired codes are rejected, and classic hardcoded placeholders never work.
func TestOTPGenerationIsRandomAndSecure(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleDriver, "", "+919000000010")
	// This test sends far more OTPs from one IP than the default 20/hour SMS-cost limit allows;
	// raise it here so the assertions below are about OTP randomness, not that unrelated limit
	// (which TestRateLimits already covers).
	e.api.Limiter.Rules.OTPSendIP = ratelimit.Rule{Name: "otp_send_ip", Limit: 1000, Window: time.Hour}

	// --- 1. Repeated requests generate different codes (statistical, not a brittle pairwise
	// equality check: two independent 6-digit random draws can coincidentally collide about
	// 1-in-a-million times, so asserting "code2 != code1" would occasionally and incorrectly fail).
	// Across 20 distinct numbers (sidesteps the per-number cooldown), a static/hardcoded OTP would
	// produce exactly 1 distinct value; real crypto/rand draws make more than one collision among
	// 20 values astronomically unlikely.
	seen := map[string]bool{}
	for i := range 20 {
		mobile := fmt.Sprintf("9300%06d", i)
		r := e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "parent"})
		e.expect(r, 200, "")
		code, _ := r.data()["development_otp"].(string)
		if len(code) != 6 {
			t.Fatalf("send %d: development_otp = %q, want a 6-digit code", i, code)
		}
		seen[code] = true
	}
	if len(seen) < 19 { // allow one coincidental collision out of 20, which is still extraordinarily unlikely
		t.Fatalf("20 sends produced only %d distinct codes; OTP generation looks static, not random", len(seen))
	}

	// --- 2/3. A new code replaces the previous one: the old code stops working, the new one is
	// exactly what verify accepts.
	mobile := "9000000010"
	send1 := e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "driver"})
	e.expect(send1, 200, "")
	code1, _ := send1.data()["development_otp"].(string)

	clearOTPCooldown(e, "+919000000010")
	send2 := e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "driver"})
	e.expect(send2, 200, "")
	code2, _ := send2.data()["development_otp"].(string)

	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": "driver", "code": code1}), 401, "otp_invalid")
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": "driver", "code": code2}), 200, "")

	// --- 4. An expired code is rejected even though it was never used or replaced.
	clearOTPCooldown(e, "+919000000010")
	send3 := e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "driver"})
	e.expect(send3, 200, "")
	code3, _ := send3.data()["development_otp"].(string)
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE otp_codes SET expires_at = now() - interval '1 minute' WHERE mobile = $1 AND consumed_at IS NULL`,
		"+919000000010"); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": "driver", "code": code3}), 401, "otp_invalid")

	// --- 5. Classic hardcoded/placeholder codes are never accepted as a fallback, whatever the
	// real generated code turned out to be.
	clearOTPCooldown(e, "+919000000010")
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": "driver"}), 200, "")
	for _, hardcoded := range []string{"123456", "12345", "111111", "000000", "1234"} {
		e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": "driver", "code": hardcoded}), 401, "otp_invalid")
	}
}

func TestSuperAdmin2FAAndSchools(t *testing.T) {
	e := newEnv(t)
	e.user(nil, models.RoleSuperAdmin, "root@sbts.test", "")
	tok := e.login("root@sbts.test")

	// Everything except 2FA setup is blocked until 2FA is on.
	e.expect(e.do("GET", "/api/v1/schools", tok, nil), 403, "2fa_setup_required")
	me := e.do("GET", "/api/v1/auth/me", tok, nil)
	if me.data()["two_factor_setup_required"] != true {
		t.Fatalf("me: %v", me.Body)
	}
	setup := e.do("POST", "/api/v1/auth/2fa/setup", tok, nil)
	e.expect(setup, 200, "")
	secret := setup.data()["secret"].(string)
	code, _ := totp.GenerateCode(secret, time.Now())
	e.expect(e.do("POST", "/api/v1/auth/2fa/enable", tok, map[string]string{"code": code}), 204, "")

	// Next login needs the code.
	first := e.do("POST", "/api/v1/auth/login", "", map[string]string{"email": "root@sbts.test", "password": testPassword})
	e.expect(first, 200, "")
	if first.data()["mfa_required"] != true || first.data()["access_token"] != nil {
		t.Fatalf("expected mfa step: %v", first.Body)
	}
	code, _ = totp.GenerateCode(secret, time.Now())
	second := e.do("POST", "/api/v1/auth/login/2fa", "", map[string]string{"mfa_token": first.data()["mfa_token"].(string), "code": code})
	e.expect(second, 200, "")
	tok = second.data()["access_token"].(string)

	// Super Admin manages schools.
	created := e.do("POST", "/api/v1/schools", tok, map[string]any{"name": "New School", "code": "new-1", "working_days": []string{"mon", "tue"}})
	e.expect(created, 201, "")
	id := created.data()["id"].(string)
	if created.data()["code"] != "NEW-1" {
		t.Errorf("code not upper-cased: %v", created.data()["code"])
	}
	e.expect(e.do("POST", "/api/v1/schools", tok, map[string]any{"name": "Dup", "code": "NEW-1"}), 409, "conflict")
	e.expect(e.do("POST", "/api/v1/schools", tok, map[string]any{"name": "Bad", "code": "x", "timezone": "Mars/Base"}), 400, "validation_failed")

	list := e.do("GET", "/api/v1/schools?q=new", tok, nil)
	e.expect(list, 200, "")
	if total := list.Body["meta"].(map[string]any)["total"]; total != float64(1) {
		t.Errorf("total = %v", total)
	}

	// Deactivating a school locks out its users immediately.
	e.user(&id, models.RoleSchoolAdmin, "admin@new.test", "")
	adminTok := e.login("admin@new.test")
	// Critical action: needs the school code typed to confirm (S8-08).
	e.expect(e.do("PATCH", "/api/v1/schools/"+id+"/status", tok, map[string]string{"status": "inactive"}), 400, "validation_failed")
	e.expect(e.do("PATCH", "/api/v1/schools/"+id+"/status", tok, map[string]string{"status": "inactive", "confirm_code": "wrong"}), 400, "validation_failed")
	e.expect(e.do("PATCH", "/api/v1/schools/"+id+"/status", tok, map[string]string{"status": "inactive", "confirm_code": "new-1"}), 200, "")
	e.expect(e.do("GET", "/api/v1/auth/me", adminTok, nil), 403, "school_inactive")
	e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"email": "admin@new.test", "password": testPassword}), 403, "school_inactive")
}

func TestAuthRequired(t *testing.T) {
	e := newEnv(t)
	e.expect(e.do("GET", "/api/v1/auth/me", "", nil), 401, "unauthorized")
	e.expect(e.do("GET", "/api/v1/auth/me", "not-a-jwt", nil), 401, "token_invalid")
	e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"email": "nobody@x.test", "password": "whatever1"}), 401, "invalid_credentials")
}

func TestOwnProfileAndPassword(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	e.user(&a, models.RoleDriver, "", "+919000000301")
	tok := e.login("tm@a.test")
	other := e.login("tm@a.test")

	// Name only; email stays as the account manager set it.
	r := e.do("PATCH", "/api/v1/auth/me", tok, map[string]string{"name": "  Ravi Kumar ", "email": "x@x.test"})
	e.expect(r, 200, "")
	if r.data()["name"] != "Ravi Kumar" || r.data()["email"] != "tm@a.test" {
		t.Errorf("profile = %v", r.data())
	}
	e.expect(e.do("PATCH", "/api/v1/auth/me", tok, map[string]string{"name": " "}), 400, "validation_failed")

	// Wrong current password is a field error; nothing changes, other devices stay logged in.
	bad := e.do("POST", "/api/v1/auth/password", tok, map[string]string{
		"current_password": "wrong-password", "new_password": "New@Pass123"})
	e.expect(bad, 400, "validation_failed")
	if f, _ := bad.Body["error"].(map[string]any)["fields"].(map[string]any); f["current_password"] == nil {
		t.Errorf("fields = %v", f)
	}
	e.expect(e.do("POST", "/api/v1/auth/password", tok, map[string]string{
		"current_password": testPassword, "new_password": "short"}), 400, "validation_failed")
	e.expect(e.do("GET", "/api/v1/auth/me", other, nil), 200, "")

	// Success: this session stays, every other one ends, the new password works.
	e.expect(e.do("POST", "/api/v1/auth/password", tok, map[string]string{
		"current_password": testPassword, "new_password": "New@Pass123"}), 204, "")
	e.expect(e.do("GET", "/api/v1/auth/me", tok, nil), 200, "")
	e.expect(e.do("GET", "/api/v1/auth/me", other, nil), 401, "session_revoked")
	e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"email": "tm@a.test", "password": testPassword}), 401, "")
	e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"email": "tm@a.test", "password": "New@Pass123"}), 200, "")

	// App users manage neither (their name and mobile belong to the school).
	drv := e.otpLogin("9000000301", "driver")
	e.expect(e.do("PATCH", "/api/v1/auth/me", drv, map[string]string{"name": "x"}), 403, "forbidden")
	e.expect(e.do("POST", "/api/v1/auth/password", drv, map[string]string{"current_password": "x", "new_password": "New@Pass123"}), 403, "forbidden")

	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action IN ('auth.profile_update', 'auth.password_change')`).Scan(&n)
	if n != 2 {
		t.Errorf("audit entries = %d, want 2", n)
	}
}
