package bootstrap_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/bootstrap"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/database"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	if err := database.Migrate(url, "up"); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), `TRUNCATE audit_logs, otp_codes, user_sessions, users, schools CASCADE`); err != nil {
		t.Fatal(err)
	}
	return store.New(pool)
}

func TestCreateFirstSuperAdmin(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	// Same input rules as the API.
	_, err := bootstrap.CreateFirstSuperAdmin(ctx, st, "", "not-an-email", "short")
	var inv *bootstrap.InvalidInput
	if !errors.As(err, &inv) || len(inv.Fields) != 3 {
		t.Fatalf("want 3 field errors, got %v", err)
	}

	if _, err := bootstrap.CreateFirstSuperAdmin(ctx, st, "Owner", "owner@school.test", strings.Repeat("p", 73)); !errors.As(err, &inv) {
		t.Fatalf("73-byte password must be invalid input, got %v", err)
	}

	// The email must be free (any role, any case).
	school, err := store.CreateSchool(ctx, st.Pool, store.SchoolInput{Name: "A School", Code: "AAA",
		WorkingDays: []string{"mon"}, Timezone: "Asia/Kolkata", TransportConfig: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(ctx, st.Pool, store.UserInput{SchoolID: &school.ID, Role: models.RoleSchoolAdmin,
		Name: "x", Email: ptr("taken@school.test")}); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.CreateFirstSuperAdmin(ctx, st, "Owner", "TAKEN@school.test", "Password@123"); !errors.Is(err, bootstrap.ErrEmailTaken) {
		t.Fatalf("taken email: %v", err)
	}

	u, err := bootstrap.CreateFirstSuperAdmin(ctx, st, " Owner ", " Owner@School.test ", "Password@123")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != models.RoleSuperAdmin || u.SchoolID != nil || *u.Email != "owner@school.test" || u.Name != "Owner" ||
		u.TOTPEnabled {
		t.Errorf("created %+v", u)
	}
	got, err := store.GetUserByEmail(ctx, st.Pool, "owner@school.test")
	if err != nil || !auth.CheckPassword(got.PasswordHash, "Password@123") {
		t.Fatalf("password not stored correctly: %v", err)
	}

	// Audited as a system action, without the password or its hash.
	var actor, after string
	if err := st.Pool.QueryRow(ctx, `SELECT actor_role, after::text FROM audit_logs
		WHERE action = 'user.bootstrap_super_admin' AND entity_id = $1`, u.ID).Scan(&actor, &after); err != nil {
		t.Fatal(err)
	}
	if actor != "system" || strings.Contains(after, "Password@123") || strings.Contains(after, "$2a$") {
		t.Errorf("audit: actor=%q after=%s", actor, after)
	}

	// Bootstrap only: a second Super Admin cannot be created this way.
	if _, err := bootstrap.CreateFirstSuperAdmin(ctx, st, "Other", "other@school.test", "Password@123"); !errors.Is(err, bootstrap.ErrSuperAdminExists) {
		t.Fatalf("second run: %v", err)
	}
	// ...even when the first one is suspended.
	if _, err := st.Pool.Exec(ctx, `UPDATE users SET status = 'suspended' WHERE id = $1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.CreateFirstSuperAdmin(ctx, st, "Other", "other@school.test", "Password@123"); !errors.Is(err, bootstrap.ErrSuperAdminExists) {
		t.Fatalf("with a suspended Super Admin: %v", err)
	}
}

func ptr(s string) *string { return &s }
