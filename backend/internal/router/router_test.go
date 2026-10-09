package router_test

// Integration tests: the full HTTP stack against a real Postgres.
// Set TEST_DATABASE_URL (the tables are wiped), e.g.
//   TEST_DATABASE_URL=postgres://sbts:sbts@localhost:5433/sbts_test?sslmode=disable go test ./...

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/router"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/secrets"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

const (
	testPassword = "Password@123"
	devOTP       = "123456"
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

	eta := &fakeETA{}
	pushes := &fakePush{}
	api := &handlers.API{
		Store:                store.New(pool),
		Tokens:               auth.NewTokenManager("test-secret-test-secret-test-secret", time.Minute, time.Hour),
		SMS:                  auth.LogSender{},
		OTPTTL:               5 * time.Minute,
		OTPDevCode:           devOTP,
		Secrets:              testSecrets(t),
		Live:                 live,
		Hub:                  hub,
		ETA:                  eta,
		CheckPushKey:         func(context.Context, []byte) error { return nil },
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
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
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
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", send), 200, "")
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", send), 429, "otp_too_soon")
	// Unknown numbers get the same answer, so registered numbers cannot be discovered.
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9111111111", "app": "driver"}), 200, "")

	verify := map[string]string{"mobile": "9000000001", "app": "driver", "code": "000000"}
	e.expect(e.do("POST", "/api/v1/auth/otp/verify", "", verify), 401, "otp_invalid")
	verify["code"] = devOTP
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
