package router_test

import (
	"context"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

func TestClearSchoolData(t *testing.T) {
	e := newEnv(t)
	a, b := newTenant(e, "AAA", 1), newTenant(e, "BBB", 2)
	e.user(nil, models.RoleSuperAdmin, "root@sbts.test", "")
	root := e.login("root@sbts.test")
	setup := e.do("POST", "/api/v1/auth/2fa/setup", root, nil)
	code, _ := totp.GenerateCode(setup.data()["secret"].(string), time.Now())
	e.expect(e.do("POST", "/api/v1/auth/2fa/enable", root, map[string]string{"code": code}), 204, "")
	e.registerDevice(a.tokens[models.RoleParent], "fcm-token-parent-AAA-0000000001")
	clear := a.base + "/clear-data"
	all := []string{"trips", "notifications", "students", "routes", "buses", "drivers"}
	before := e.snapshot(b.id)

	// Super Admin only; school staff get 403 on their own school.
	for _, role := range []models.Role{models.RoleSchoolAdmin, models.RoleTransportManager} {
		e.expect(e.do("POST", clear, a.tokens[role], map[string]any{"scopes": all, "dry_run": true}), 403, "")
	}
	// Validation: unknown part, missing dependency, wrong confirmation code.
	e.expect(e.do("POST", clear, root, map[string]any{"scopes": []string{"everything"}, "dry_run": true}), 400, "validation_failed")
	e.expect(e.do("POST", clear, root, map[string]any{"scopes": []string{"routes"}, "dry_run": true}), 400, "validation_failed")
	e.expect(e.do("POST", clear, root, map[string]any{"scopes": []string{"students"}, "confirm_code": "BBB"}), 400, "validation_failed")

	// Students and parents alone: no dependency, trips stay.
	r := e.do("POST", clear, root, map[string]any{"scopes": []string{"students"}, "dry_run": true})
	e.expect(r, 200, "")
	if d := r.data()["deleted"].(map[string]any); d["students"] != float64(1) || d["parents"] != float64(1) || d["logins"] != float64(1) {
		t.Errorf("dry run = %v", d)
	}
	if n := e.count(`SELECT count(*) FROM students WHERE school_id = $1`, a.id); n != 1 {
		t.Fatalf("dry run deleted students: %d left", n)
	}

	// A started trip blocks clearing trips.
	e.expect(e.do("POST", clear, root, map[string]any{"scopes": all, "confirm_code": "aaa"}), 409, "trips_in_progress")
	e.expect(e.do("POST", "/api/v1/driver/trips/"+a.ids["tripID"]+"/end", a.tokens[models.RoleDriver], nil), 200, "")

	r = e.do("POST", clear, root, map[string]any{"scopes": all, "confirm_code": "aaa"})
	e.expect(r, 200, "")
	d := r.data()["deleted"].(map[string]any)
	for _, k := range []string{"trips", "routes", "buses", "drivers", "students", "parents", "announcements", "emergencies"} {
		if d[k] != float64(1) {
			t.Errorf("deleted[%s] = %v, want 1 (%v)", k, d[k], d)
		}
	}
	if d["logins"] != float64(2) { // driver + parent
		t.Errorf("logins = %v", d["logins"])
	}

	for _, table := range []string{"trips", "routes", "stops", "buses", "drivers", "students", "parents", "notifications",
		"announcements", "driver_route_assignments", "student_assignments", "emergencies", "trip_status_history"} {
		if n := e.count(`SELECT count(*) FROM `+table+` WHERE school_id = $1`, a.id); n != 0 {
			t.Errorf("%s: %d rows left", table, n)
		}
	}
	// The school, its staff and the audit log stay; the action itself is audited.
	if n := e.count(`SELECT count(*) FROM users WHERE school_id = $1`, a.id); n != 2 {
		t.Errorf("users left = %d, want the 2 staff", n)
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'school.clear_data'`, a.id); n != 1 {
		t.Errorf("clear_data audit entries = %d", n)
	}
	e.expect(e.do("GET", a.base+"/students", a.tokens[models.RoleSchoolAdmin], nil), 200, "")
	// The driver's and parent's phones are logged out with their records.
	e.expect(e.do("GET", "/api/v1/driver/trips", a.tokens[models.RoleDriver], nil), 401, "")

	// School B is untouched.
	after := e.snapshot(b.id)
	for tbl, sum := range before {
		if after[tbl] != sum {
			t.Errorf("school B's %s changed", tbl)
		}
	}
	// The same mobile numbers can be registered again.
	e.expect(e.do("POST", a.base+"/drivers", a.tokens[models.RoleSchoolAdmin], map[string]any{"name": "New", "mobile": "9100000001"}), 201, "")
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
