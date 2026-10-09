package router_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

type tripFixture struct {
	school, base             string
	admin, tm                string // tokens
	route, pickupOnlyRoute   string
	stops                    []string
	bus1, bus2, busMaint     string
	driver1, driver2         string
	driver1Tok, driver2Tok   string
	student                  string
	today, tomorrow, dayFrom string
}

func (e *env) dateIn(schoolID string, days int) string {
	e.t.Helper()
	var d string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT ((now() AT TIME ZONE timezone)::date + $2::int)::text FROM schools WHERE id = $1`, schoolID, days).Scan(&d); err != nil {
		e.t.Fatal(err)
	}
	return d
}

func newTripFixture(e *env) *tripFixture {
	e.t.Helper()
	f := &tripFixture{school: e.school("AAA")}
	f.base = "/api/v1/schools/" + f.school
	e.user(&f.school, models.RoleSchoolAdmin, "admin@a.test", "")
	e.user(&f.school, models.RoleTransportManager, "tm@a.test", "")
	f.admin, f.tm = e.login("admin@a.test"), e.login("tm@a.test")
	f.today, f.tomorrow, f.dayFrom = e.dateIn(f.school, 0), e.dateIn(f.school, 1), e.dateIn(f.school, 2)

	f.route, f.stops = e.routeWithStops(f.admin, f.school, "RS-01", false, "Gandhipuram", "Peelamedu", "Singanallur")
	f.pickupOnlyRoute, _ = e.routeWithStops(f.admin, f.school, "RS-02", true, "Ukkadam")
	bus := func(n string) string {
		r := e.do("POST", f.base+"/buses", f.tm, map[string]any{"vehicle_number": n, "capacity": 40})
		e.expect(r, 201, "")
		return r.data()["id"].(string)
	}
	f.bus1, f.bus2, f.busMaint = bus("TN-38-AB-1234"), bus("TN-38-CD-5678"), bus("TN-38-EF-9999")
	e.expect(e.do("PATCH", f.base+"/buses/"+f.busMaint+"/status", f.tm, map[string]string{"status": "maintenance"}), 200, "")
	driver := func(name, mobile string) string {
		r := e.do("POST", f.base+"/drivers", f.tm, map[string]any{"name": name, "mobile": mobile})
		e.expect(r, 201, "")
		return r.data()["id"].(string)
	}
	f.driver1, f.driver2 = driver("Kumar", "9000000001"), driver("Ravi", "9000000005")
	f.driver1Tok, f.driver2Tok = e.otpLogin("9000000001", "driver"), e.otpLogin("9000000005", "driver")

	f.student = e.do("POST", f.base+"/students", f.admin, map[string]any{"admission_no": "A-1", "name": "Asha"}).data()["id"].(string)
	e.expect(e.do("PUT", f.base+"/students/"+f.student+"/assignment", f.admin,
		map[string]any{"route_id": f.route, "pickup_stop_id": f.stops[1], "drop_stop_id": f.stops[1]}), 200, "")
	return f
}

func (f *tripFixture) trip(date, tripType, route, bus, driver string) map[string]any {
	return map[string]any{"trip_date": date, "trip_type": tripType, "route_id": route, "bus_id": bus, "driver_id": driver}
}

func TestTripAssignmentRules(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	morning := e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1))
	e.expect(morning, 201, "")
	m := morning.data()
	if m["status"] != "scheduled" || m["student_count"] != float64(1) || m["route"].(map[string]any)["code"] != "RS-01" {
		t.Fatalf("trip = %v", m)
	}

	// One route, bus and driver per date + trip type (rule 3).
	for _, c := range []struct {
		body  map[string]any
		field string
	}{
		{f.trip(f.today, "morning_pickup", f.route, f.bus2, f.driver2), "route_id"},
		{f.trip(f.today, "morning_pickup", f.pickupOnlyRoute, f.bus1, f.driver2), "bus_id"},
		{f.trip(f.today, "morning_pickup", f.pickupOnlyRoute, f.bus2, f.driver1), "driver_id"},
	} {
		r := e.do("POST", trips, f.tm, c.body)
		e.expect(r, 409, "trip_conflict")
		if r.Body["error"].(map[string]any)["fields"].(map[string]any)[c.field] == nil {
			t.Errorf("conflict should name %s: %v", c.field, r.Body)
		}
	}
	// Evening Drop is a separate trip (rule 4): same bus and driver are fine.
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "evening_drop", f.route, f.bus1, f.driver1)), 201, "")

	// Validation.
	yesterday := e.dateIn(f.school, -1)
	e.expect(e.do("POST", trips, f.tm, f.trip(yesterday, "morning_pickup", f.pickupOnlyRoute, f.bus2, f.driver2)), 400, "validation_failed")
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", f.pickupOnlyRoute, f.busMaint, f.driver2)), 400, "validation_failed")
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "evening_drop", f.pickupOnlyRoute, f.bus2, f.driver2)), 400, "validation_failed")
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "afternoon", f.pickupOnlyRoute, f.bus2, f.driver2)), 400, "validation_failed")

	// Cancelling frees the slot.
	id := m["id"].(string)
	e.expect(e.do("POST", trips+"/"+id+"/cancel", f.tm, map[string]string{"reason": ""}), 400, "validation_failed")
	e.expect(e.do("POST", trips+"/"+id+"/cancel", f.tm, map[string]string{"reason": "Holiday"}), 200, "")
	e.expect(e.do("POST", trips+"/"+id+"/cancel", f.tm, map[string]string{"reason": "again"}), 409, "invalid_transition")
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus2, f.driver2)), 201, "")

	list := e.do("GET", trips+"?date="+f.today, f.tm, nil)
	if n := len(list.list()); n != 3 {
		t.Errorf("trips today = %d, want 3 (incl. cancelled)", n)
	}
	if n := len(e.do("GET", trips+"?date="+f.today+"&status=cancelled", f.tm, nil).list()); n != 1 {
		t.Errorf("cancelled filter = %d", n)
	}

	// Other schools cannot see trips.
	b := e.school("BBB")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	other := e.login("admin@b.test")
	e.expect(e.do("GET", trips+"/"+id, other, nil), 404, "not_found")
	e.expect(e.do("GET", "/api/v1/schools/"+b+"/trips/"+id, other, nil), 404, "not_found")
}

func TestDriverTripFlow(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	morning := e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1)).data()["id"].(string)
	evening := e.do("POST", trips, f.tm, f.trip(f.today, "evening_drop", f.route, f.bus1, f.driver1)).data()["id"].(string)
	future := e.do("POST", trips, f.tm, f.trip(f.tomorrow, "morning_pickup", f.route, f.bus1, f.driver1)).data()["id"].(string)

	// Today's trips: own only, Morning Pickup first, with stops and per-stop counts.
	mine := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil)
	e.expect(mine, 200, "")
	if len(mine.list()) != 2 || mine.list()[0].(map[string]any)["trip_type"] != "morning_pickup" {
		t.Fatalf("driver trips = %v", mine.list())
	}
	stops := mine.list()[0].(map[string]any)["stops"].([]any)
	if len(stops) != 3 || stops[1].(map[string]any)["student_count"] != float64(1) {
		t.Errorf("stops = %v", stops)
	}
	if raw := strings.ToLower(stringify(mine.Body)); strings.Contains(raw, "asha") {
		t.Error("drivers must not see student names")
	}
	if n := len(e.do("GET", "/api/v1/driver/trips", f.driver2Tok, nil).list()); n != 0 {
		t.Errorf("driver 2 sees %d trips", n)
	}
	// Another driver's trip does not exist for them (rule 7).
	e.expect(e.do("GET", "/api/v1/driver/trips/"+morning, f.driver2Tok, nil), 404, "not_found")
	e.expect(e.do("POST", "/api/v1/driver/trips/"+morning+"/confirm", f.driver2Tok, nil), 404, "not_found")

	act := func(id, action string) resp {
		return e.do("POST", "/api/v1/driver/trips/"+id+"/"+action, f.driver1Tok, nil)
	}
	e.expect(act(morning, "start"), 409, "invalid_transition") // must confirm first
	e.expect(act(morning, "confirm"), 200, "")
	started := act(morning, "start")
	e.expect(started, 200, "")
	if started.data()["status"] != "started" || started.data()["started_at"] == nil {
		t.Errorf("after start: %v", started.data())
	}
	// Only one trip in progress per driver.
	e.expect(act(evening, "confirm"), 200, "")
	e.expect(act(evening, "start"), 409, "another_trip_in_progress")
	e.expect(act(morning, "end"), 200, "")
	e.expect(act(morning, "end"), 409, "invalid_transition")
	e.expect(act(evening, "start"), 200, "")

	// Tomorrow's trip can be confirmed but not started today.
	e.expect(act(future, "confirm"), 200, "")
	e.expect(act(future, "start"), 409, "not_trip_day")

	// Changing the driver of a confirmed trip resets it to scheduled for the new driver.
	upd := e.do("PUT", trips+"/"+future, f.tm, f.trip(f.tomorrow, "morning_pickup", f.route, f.bus1, f.driver2))
	e.expect(upd, 200, "")
	if upd.data()["status"] != "scheduled" || upd.data()["driver"].(map[string]any)["name"] != "Ravi" {
		t.Errorf("after driver change: %v", upd.data())
	}
	// Started trips cannot be edited.
	e.expect(e.do("PUT", trips+"/"+evening, f.tm, f.trip(f.today, "evening_drop", f.route, f.bus2, f.driver1)), 409, "invalid_transition")

	detail := e.do("GET", trips+"/"+morning, f.tm, nil)
	var seq []string
	for _, h := range detail.data()["history"].([]any) {
		seq = append(seq, h.(map[string]any)["to_status"].(string))
	}
	if got := strings.Join(seq, ","); got != "scheduled,confirmed,started,completed" {
		t.Errorf("history = %s", got)
	}
}

func TestTripOverrideAndCopy(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	id := e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1)).data()["id"].(string)
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "evening_drop", f.route, f.bus2, f.driver2)), 201, "")

	// Override needs a reason and is audited.
	e.expect(e.do("POST", trips+"/"+id+"/override", f.tm, map[string]string{"status": "started"}), 400, "validation_failed")
	e.expect(e.do("POST", trips+"/"+id+"/override", f.tm, map[string]string{"status": "started", "reason": "Driver phone not working"}), 200, "")
	e.expect(e.do("POST", trips+"/"+id+"/override", f.tm, map[string]string{"status": "completed", "reason": "Reached school"}), 200, "")
	e.expect(e.do("POST", trips+"/"+id+"/override", f.tm, map[string]string{"status": "cancelled", "reason": "x"}), 409, "invalid_transition")
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = 'trip.override'`).Scan(&n)
	if n != 2 {
		t.Errorf("override audit entries = %d", n)
	}

	// Copy today to the next two days; a clash on one day is skipped, not fatal.
	e.expect(e.do("POST", trips, f.tm, f.trip(f.tomorrow, "morning_pickup", f.pickupOnlyRoute, f.bus1, f.driver2)), 201, "")
	cp := e.do("POST", trips+"/copy", f.tm, map[string]any{"from_date": f.today, "to_dates": []string{f.tomorrow, f.dayFrom}})
	e.expect(cp, 200, "")
	// Tomorrow's morning copy clashes (bus1 already used); the other three copies are created.
	if cp.data()["created"] != float64(3) || len(cp.data()["skipped"].([]any)) != 1 {
		t.Fatalf("copy = %v", cp.data())
	}
	again := e.do("POST", trips+"/copy", f.tm, map[string]any{"from_date": f.today, "to_dates": []string{f.dayFrom}})
	if again.data()["created"] != float64(0) {
		t.Errorf("copying twice must not duplicate: %v", again.data())
	}
	e.expect(e.do("POST", trips+"/copy", f.tm, map[string]any{"from_date": f.today, "to_dates": []string{f.today}}), 400, "validation_failed")
}

func TestParentSeesChildTrips(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	e.expect(e.do("POST", f.base+"/parents", f.admin, map[string]any{
		"name": "Meena", "mobile": "9000000002", "children": []map[string]string{{"student_id": f.student, "relationship": "mother"}},
	}), 201, "")
	other := e.do("POST", f.base+"/students", f.admin, map[string]any{"admission_no": "A-2", "name": "Bala"}).data()["id"].(string)
	e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1))
	e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "evening_drop", f.route, f.bus2, f.driver2))

	ptok := e.otpLogin("9000000002", "parent")
	r := e.do("GET", "/api/v1/parent/children/"+f.student+"/trips", ptok, nil)
	e.expect(r, 200, "")
	if len(r.list()) != 2 {
		t.Fatalf("parent trips = %v", r.list())
	}
	first := r.list()[0].(map[string]any)
	if first["trip_type"] != "morning_pickup" || first["bus_number"] != "TN-38-AB-1234" || first["child_stop_name"] != "Peelamedu" {
		t.Errorf("trip = %v", first)
	}
	if strings.Contains(stringify(r.Body), "9000000001") {
		t.Error("parents must not get the driver's mobile")
	}
	e.expect(e.do("GET", "/api/v1/parent/children/"+other+"/trips", ptok, nil), 404, "not_found")
	e.expect(e.do("GET", "/api/v1/driver/trips", ptok, nil), 403, "forbidden")
}

// stringify flattens a response body so tests can check what it does not contain.
func stringify(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
