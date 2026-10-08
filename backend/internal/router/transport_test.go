package router_test

import (
	"strings"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

func (r resp) list() []any { l, _ := r.Body["data"].([]any); return l }

func stopNames(r resp) string {
	var names []string
	for _, s := range r.data()["stops"].([]any) {
		names = append(names, s.(map[string]any)["name"].(string))
	}
	return strings.Join(names, ",")
}

func TestTransportTenantIsolation(t *testing.T) {
	e := newEnv(t)
	a, b := e.school("AAA"), e.school("BBB")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	tm := e.login("tm@a.test")
	other := e.login("admin@b.test")
	base := "/api/v1/schools/" + a

	bus := e.do("POST", base+"/buses", tm, map[string]any{"vehicle_number": "TN-38-AB-1234", "capacity": 40})
	e.expect(bus, 201, "")
	route := e.do("POST", base+"/routes", tm, map[string]any{"name": "Route 1", "code": "rs-01"})
	e.expect(route, 201, "")
	driver := e.do("POST", base+"/drivers", tm, map[string]any{"name": "Kumar", "mobile": "9000000001"})
	e.expect(driver, 201, "")
	busID, routeID, driverID := bus.data()["id"].(string), route.data()["id"].(string), driver.data()["id"].(string)

	// The other school cannot reach these through either school's path.
	for _, path := range []string{
		base + "/buses", base + "/buses/" + busID, base + "/routes/" + routeID, base + "/drivers/" + driverID,
		"/api/v1/schools/" + b + "/buses/" + busID,
		"/api/v1/schools/" + b + "/routes/" + routeID,
		"/api/v1/schools/" + b + "/drivers/" + driverID,
	} {
		e.expect(e.do("GET", path, other, nil), 404, "not_found")
	}
	e.expect(e.do("POST", "/api/v1/schools/"+b+"/routes/"+routeID+"/stops", other,
		map[string]any{"name": "X", "latitude": 11, "longitude": 77}), 404, "not_found")

	// The other school's list does not include them.
	e.expect(e.do("GET", "/api/v1/schools/"+b+"/buses", other, nil), 200, "")
	if n := len(e.do("GET", "/api/v1/schools/"+b+"/buses", other, nil).list()); n != 0 {
		t.Errorf("school B sees %d buses", n)
	}

	// Drivers and parents have no access to admin master data, even in their own school.
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9000000001", "app": "driver"}), 200, "")
	login := e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": "9000000001", "app": "driver", "code": devOTP})
	e.expect(login, 200, "")
	e.expect(e.do("GET", base+"/buses", login.data()["access_token"].(string), nil), 403, "forbidden")
}

func TestDriverLoginLifecycle(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	admin := e.login("admin@a.test")
	base := "/api/v1/schools/" + a + "/drivers"

	created := e.do("POST", base, admin, map[string]any{
		"name": "Kumar", "mobile": "+91 90000 00001", "license_number": "tn3820190001234", "license_expiry": "2030-01-31",
	})
	e.expect(created, 201, "")
	d := created.data()
	if d["mobile"] != "+919000000001" || d["license_number"] != "TN3820190001234" || d["license_expiry"] != "2030-01-31" {
		t.Errorf("unexpected driver: %v", d)
	}
	id := d["id"].(string)

	e.expect(e.do("POST", base, admin, map[string]any{"name": "Dup", "mobile": "9000000001"}), 409, "conflict")
	e.expect(e.do("POST", base, admin, map[string]any{"name": "", "mobile": "123", "license_expiry": "31-01-2030"}), 400, "validation_failed")

	otpLogin := func() resp {
		e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": "9000000001", "app": "driver"})
		_, _ = e.pool.Exec(t.Context(), `UPDATE otp_codes SET created_at = created_at - interval '1 minute'`) // skip the 30 s wait
		return e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": "9000000001", "app": "driver", "code": devOTP})
	}
	first := otpLogin()
	e.expect(first, 200, "")
	tok := first.data()["access_token"].(string)

	// Suspending the driver ends their sessions and blocks new logins.
	e.expect(e.do("PATCH", base+"/"+id+"/status", admin, map[string]string{"status": "suspended"}), 200, "")
	e.expect(e.do("GET", "/api/v1/auth/me", tok, nil), 401, "session_revoked")
	e.expect(otpLogin(), 401, "otp_invalid")

	// Reactivating restores login. Editing the mobile changes the login number.
	e.expect(e.do("PATCH", base+"/"+id+"/status", admin, map[string]string{"status": "active"}), 200, "")
	e.expect(otpLogin(), 200, "")
	upd := e.do("PUT", base+"/"+id, admin, map[string]any{"name": "Kumar R", "mobile": "9000000009"})
	e.expect(upd, 200, "")
	if upd.data()["name"] != "Kumar R" || upd.data()["mobile"] != "+919000000009" {
		t.Errorf("update not applied: %v", upd.data())
	}
}

func TestBusValidation(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	tm := e.login("tm@a.test")
	base := "/api/v1/schools/" + a + "/buses"

	created := e.do("POST", base, tm, map[string]any{"vehicle_number": " tn-38-ab-1234 ", "capacity": 40})
	e.expect(created, 201, "")
	if created.data()["vehicle_number"] != "TN-38-AB-1234" {
		t.Errorf("vehicle number = %v", created.data()["vehicle_number"])
	}
	// Same plate, different formatting.
	dup := e.do("POST", base, tm, map[string]any{"vehicle_number": "TN 38 AB 1234", "capacity": 40})
	e.expect(dup, 409, "conflict")
	if f := dup.Body["error"].(map[string]any)["fields"].(map[string]any); f["vehicle_number"] == nil {
		t.Errorf("conflict should name vehicle_number: %v", f)
	}
	e.expect(e.do("POST", base, tm, map[string]any{"vehicle_number": "TN-01-X-1", "capacity": 0}), 400, "validation_failed")

	id := created.data()["id"].(string)
	e.expect(e.do("PATCH", base+"/"+id+"/status", tm, map[string]string{"status": "maintenance"}), 200, "")
	e.expect(e.do("PATCH", base+"/"+id+"/status", tm, map[string]string{"status": "scrapped"}), 400, "validation_failed")
}

func TestStopOrdering(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	tm := e.login("tm@a.test")
	routes := "/api/v1/schools/" + a + "/routes"

	route := e.do("POST", routes, tm, map[string]any{"name": "Route 1", "code": "RS-01", "supports_drop": false})
	e.expect(route, 201, "")
	e.expect(e.do("POST", routes, tm, map[string]any{"name": "Dup", "code": "rs-01"}), 409, "conflict")
	e.expect(e.do("POST", routes, tm, map[string]any{"name": "None", "code": "RS-09", "supports_pickup": false, "supports_drop": false}), 400, "validation_failed")
	routeID := route.data()["id"].(string)
	stops := routes + "/" + routeID + "/stops"

	ids := map[string]string{}
	add := func(name string, position any) {
		body := map[string]any{"name": name, "latitude": 11.0168, "longitude": 76.9558, "pickup_time": "07:15"}
		if position != nil {
			body["position"] = position
		}
		r := e.do("POST", stops, tm, body)
		e.expect(r, 201, "")
		ids[name] = r.data()["id"].(string)
	}
	add("Gandhipuram", nil)
	add("Peelamedu", nil)
	add("Singanallur", nil)
	add("Hope College", 3) // insert before Singanallur

	get := func() resp { return e.do("GET", routes+"/"+routeID, tm, nil) }
	if got := stopNames(get()); got != "Gandhipuram,Peelamedu,Hope College,Singanallur" {
		t.Fatalf("after insert: %s", got)
	}

	e.expect(e.do("DELETE", stops+"/"+ids["Peelamedu"], tm, nil), 204, "")
	r := get()
	if got := stopNames(r); got != "Gandhipuram,Hope College,Singanallur" {
		t.Fatalf("after delete: %s", got)
	}
	for i, s := range r.data()["stops"].([]any) {
		if seq := s.(map[string]any)["sequence"]; seq != float64(i+1) {
			t.Errorf("stop %d has sequence %v", i, seq)
		}
	}

	order := []string{ids["Singanallur"], ids["Gandhipuram"], ids["Hope College"]}
	e.expect(e.do("PUT", stops+"/order", tm, map[string]any{"stop_ids": order}), 200, "")
	if got := stopNames(get()); got != "Singanallur,Gandhipuram,Hope College" {
		t.Fatalf("after reorder: %s", got)
	}
	// Missing or repeated stops are rejected.
	e.expect(e.do("PUT", stops+"/order", tm, map[string]any{"stop_ids": order[:2]}), 400, "validation_failed")
	e.expect(e.do("PUT", stops+"/order", tm, map[string]any{"stop_ids": []string{order[0], order[0], order[1]}}), 400, "validation_failed")

	// Validation: location and times.
	e.expect(e.do("POST", stops, tm, map[string]any{"name": "Bad", "latitude": 95, "longitude": 77}), 400, "validation_failed")
	e.expect(e.do("POST", stops, tm, map[string]any{"name": "Bad", "latitude": 11, "longitude": 77, "pickup_time": "7:15am"}), 400, "validation_failed")

	// A stop can only be changed through its own route.
	other := e.do("POST", routes, tm, map[string]any{"name": "Route 2", "code": "RS-02"})
	e.expect(e.do("PUT", routes+"/"+other.data()["id"].(string)+"/stops/"+ids["Gandhipuram"], tm,
		map[string]any{"name": "Moved", "latitude": 11, "longitude": 77}), 404, "not_found")
}
