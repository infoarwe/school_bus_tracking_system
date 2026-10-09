package router_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

func TestMapsSettingsPerSchool(t *testing.T) {
	e := newEnv(t)
	a, b := e.school("AAA"), e.school("BBB")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	admin, tm, other := e.login("admin@a.test"), e.login("tm@a.test"), e.login("admin@b.test")
	url := "/api/v1/schools/" + a + "/settings/maps"

	browserKey := "AIza" + strings.Repeat("B", 35)
	serverKey := "AIza" + strings.Repeat("S", 31) + "Wxyz"

	e.expect(e.do("PUT", url, admin, map[string]any{"browser_key": "not-a-key"}), 400, "validation_failed")
	set := e.do("PUT", url, admin, map[string]any{"browser_key": browserKey, "server_key": serverKey})
	e.expect(set, 200, "")

	// Staff (including Transport Managers) get the browser key; nobody gets the server key back.
	got := e.do("GET", url, tm, nil)
	e.expect(got, 200, "")
	raw, _ := json.Marshal(got.Body)
	if got.data()["browser_key"] != browserKey || got.data()["server_key_set"] != true || got.data()["server_key_hint"] != "…Wxyz" {
		t.Errorf("maps settings = %v", got.data())
	}
	if strings.Contains(string(raw), serverKey) {
		t.Fatal("server key must never be returned")
	}

	// Stored encrypted; the audit log records only what changed.
	var stored []byte
	_ = e.pool.QueryRow(context.Background(), `SELECT maps_server_key_enc FROM schools WHERE id = $1`, a).Scan(&stored)
	if len(stored) == 0 || strings.Contains(string(stored), "AIza") {
		t.Fatal("server key must be stored encrypted")
	}
	var audit string
	_ = e.pool.QueryRow(context.Background(), `SELECT after::text FROM audit_logs WHERE action = 'settings.maps_update'`).Scan(&audit)
	if strings.Contains(audit, "AIza") {
		t.Fatalf("audit log contains a key: %s", audit)
	}

	// Omitting server_key keeps it; "" removes it.
	e.do("PUT", url, admin, map[string]any{"browser_key": ""})
	if g := e.do("GET", url, admin, nil).data(); g["browser_key"] != "" || g["server_key_set"] != true {
		t.Errorf("after clearing browser key: %v", g)
	}
	e.do("PUT", url, admin, map[string]any{"browser_key": "", "server_key": ""})
	if g := e.do("GET", url, admin, nil).data(); g["server_key_set"] != false {
		t.Errorf("server key should be removed: %v", g)
	}

	// Keys are per school: Transport Managers cannot change them, other schools cannot see them.
	e.expect(e.do("PUT", url, tm, map[string]any{"browser_key": browserKey}), 403, "forbidden")
	e.expect(e.do("GET", url, other, nil), 404, "not_found")
	if g := e.do("GET", "/api/v1/schools/"+b+"/settings/maps", other, nil).data(); g["browser_key"] != "" {
		t.Errorf("school B should have its own (empty) key: %v", g)
	}
}
