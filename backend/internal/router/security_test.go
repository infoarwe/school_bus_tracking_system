package router_test

import (
	"context"
	"strings"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// S9-04 security review fixes.
func TestSecurityReviewFixes(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	adminID := e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	tmID := e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	admin := e.login("admin@a.test")
	users := "/api/v1/schools/" + a + "/users"

	// bcrypt refuses more than 72 bytes: that is invalid input (400), never a 500.
	long := strings.Repeat("p", 73)
	r := e.do("POST", users, admin, map[string]any{"name": "X", "email": "x@a.test", "role": "transport_manager",
		"password": long})
	e.expect(r, 400, "validation_failed")
	if f := r.Body["error"].(map[string]any)["fields"].(map[string]any); !strings.Contains(f["password"].(string), "72") {
		t.Errorf("fields = %v", f)
	}
	e.expect(e.do("POST", users+"/"+tmID+"/password", admin, map[string]any{"password": long}), 400, "validation_failed")
	e.expect(e.do("POST", users, admin, map[string]any{"name": "X", "email": "x@a.test", "role": "transport_manager",
		"password": strings.Repeat("p", 72)}), 201, "")
	// A long password at login is just wrong, not a server error.
	e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"email": "admin@a.test", "password": strings.Repeat("x", 200)}),
		401, "invalid_credentials")

	// Ending one of your sessions is audited (device/session control).
	other := e.login("admin@a.test")
	var sessionID string
	if err := e.pool.QueryRow(context.Background(), `SELECT id FROM user_sessions WHERE user_id = $1
		ORDER BY created_at DESC LIMIT 1`, adminID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do("DELETE", "/api/v1/auth/sessions/"+sessionID, admin, nil), 204, "")
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = 'auth.session_revoke'
		AND entity_id = $1 AND after->>'session_id' = $2`, adminID, sessionID).Scan(&n)
	if n != 1 {
		t.Errorf("session revoke audit entries = %d, want 1", n)
	}
	e.expect(e.do("GET", "/api/v1/auth/me", other, nil), 401, "session_revoked")
}
