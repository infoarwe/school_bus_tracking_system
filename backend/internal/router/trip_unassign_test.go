package router_test

// Coverage for Single/Bulk Unassign and Replace Driver/Bus (the trip-planner
// "Complete Assignment Management" feature): a trip can now exist with no
// bus and/or driver (nullable, never invented — same pattern as routeless
// Special Trips), and its driver or bus can be swapped in one atomic step
// without a separate unassign-then-reassign.

import (
	"context"
	"sync"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// TestUnassignTrip: single unassign clears bus+driver, preserves everything
// else, is idempotent, and is refused outside scheduled/confirmed.
func TestUnassignTrip(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	created := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(created, 201, "")
	id := created.data()["id"].(string)

	r := e.do("POST", trips+"/"+id+"/unassign", f.tm, nil)
	e.expect(r, 200, "")
	d := r.data()
	if d["bus"] != nil || d["driver"] != nil {
		t.Fatalf("bus/driver should be null after unassign: %v", d)
	}
	// Route, schedule, trip_date, trip_type, student_count, status: untouched.
	if d["route"].(map[string]any)["id"] != f.route || d["trip_date"] != f.today ||
		d["trip_type"] != "morning_pickup" || d["status"] != "scheduled" ||
		d["scheduled_start_at"] == nil || d["scheduled_end_at"] == nil || d["student_count"] != float64(1) {
		t.Fatalf("unassign must not touch anything else: %v", d)
	}
	// Stops still reachable via the detail endpoint (route unaffected).
	detail := e.do("GET", trips+"/"+id, f.tm, nil)
	e.expect(detail, 200, "")
	if stops := detail.data()["stops"].([]any); len(stops) != 3 {
		t.Errorf("stops = %v, want 3 (route untouched)", stops)
	}

	// Idempotent: calling it again succeeds, not an error.
	again := e.do("POST", trips+"/"+id+"/unassign", f.tm, nil)
	e.expect(again, 200, "")

	// A confirmed trip resets to scheduled on unassign (it can't stay
	// "confirmed" by a driver it no longer has).
	assigned := e.do("POST", trips, f.tm, f.tripAt(f.tomorrow, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(assigned, 201, "")
	id2 := assigned.data()["id"].(string)
	e.expect(e.do("POST", "/api/v1/driver/trips/"+id2+"/confirm", f.driver1Tok, nil), 200, "")
	confirmed := e.do("POST", trips+"/"+id2+"/unassign", f.tm, nil)
	e.expect(confirmed, 200, "")
	if confirmed.data()["status"] != "scheduled" {
		t.Errorf("confirmed trip should reset to scheduled on unassign: %v", confirmed.data())
	}

	// Started/completed/cancelled trips cannot be unassigned. Must be today's
	// date — OverrideTrip's "started" transition only runs on the trip's own day.
	startedRoute, _ := e.routeWithStops(f.admin, f.school, "RS-STARTED", false, "Z")
	started := e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", startedRoute, f.bus2, f.driver2, "16:00", "17:00"))
	e.expect(started, 201, "")
	id3 := started.data()["id"].(string)
	e.expect(e.do("POST", trips+"/"+id3+"/override", f.tm, map[string]string{"status": "started", "reason": "go"}), 200, "")
	e.expect(e.do("POST", trips+"/"+id3+"/unassign", f.tm, nil), 409, "invalid_transition")
}

// TestReassignUnassignedTrip: Part G — after unassigning, the trip stays
// visible and can be given a bus+driver again through the ordinary update
// endpoint; schedule/route/stops/students are preserved throughout.
func TestReassignUnassignedTrip(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	created := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	id := created.data()["id"].(string)
	e.expect(e.do("POST", trips+"/"+id+"/unassign", f.tm, nil), 200, "")

	// Still visible in the day's list, clearly unassigned.
	list := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	if len(list) != 1 || list[0].(map[string]any)["bus"] != nil {
		t.Fatalf("unassigned trip should stay listed with bus null: %v", list)
	}

	reassigned := e.do("PUT", trips+"/"+id, f.tm,
		f.tripAt(f.today, "morning_pickup", f.route, f.bus2, f.driver2, "07:30", "08:15"))
	e.expect(reassigned, 200, "")
	d := reassigned.data()
	if d["bus"].(map[string]any)["id"] != f.bus2 || d["driver"].(map[string]any)["id"] != f.driver2 {
		t.Fatalf("reassign should set the new bus/driver: %v", d)
	}
	if d["route"].(map[string]any)["id"] != f.route || d["scheduled_start_at"] == nil {
		t.Fatalf("route/schedule must survive reassignment: %v", d)
	}
}

// TestReplaceDriver: Part D/E — one atomic swap; bus/route/schedule
// untouched; other dates for the old driver are unaffected; the old driver
// is freed and the new driver's own conflicts are enforced; inactive/
// not-found drivers are rejected.
func TestReplaceDriver(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-RD", false, "Elsewhere")

	// Driver1 runs trips on today AND tomorrow.
	today := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(today, 201, "")
	todayID := today.data()["id"].(string)
	tomorrow := e.do("POST", trips, f.tm, f.tripAt(f.tomorrow, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(tomorrow, 201, "")
	tomorrowID := tomorrow.data()["id"].(string)

	// Replace today's driver only.
	replaced := e.do("PATCH", trips+"/"+todayID+"/driver", f.tm, map[string]any{"driver_id": f.driver2})
	e.expect(replaced, 200, "")
	d := replaced.data()
	if d["driver"].(map[string]any)["id"] != f.driver2 {
		t.Fatalf("driver should now be driver2: %v", d)
	}
	if d["bus"].(map[string]any)["id"] != f.bus1 || d["route"].(map[string]any)["id"] != f.route ||
		d["scheduled_start_at"] == nil || d["trip_date"] != f.today {
		t.Fatalf("bus/route/schedule/date must be unchanged: %v", d)
	}

	// Tomorrow's trip is still driver1 — untouched by the single-trip replace.
	tomorrowAfter := e.do("GET", trips+"/"+tomorrowID, f.tm, nil)
	e.expect(tomorrowAfter, 200, "")
	if tomorrowAfter.data()["driver"].(map[string]any)["id"] != f.driver1 {
		t.Fatalf("tomorrow's trip must keep driver1: %v", tomorrowAfter.data())
	}

	// Driver1 is now free at that exact slot today — a new trip for driver1
	// at the same time on a different route must succeed.
	freed := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", other, f.bus2, f.driver1, "07:30", "08:15"))
	e.expect(freed, 201, "")

	// Driver2 already has a trip overlapping the target window elsewhere —
	// replacing another trip's driver with driver2 at an overlapping time
	// must be rejected, and the original driver preserved.
	busyRoute, _ := e.routeWithStops(f.admin, f.school, "RS-RD2", false, "X")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.dayFrom, "morning_pickup", busyRoute, f.bus2, f.driver2, "09:00", "10:00")), 201, "")
	conflictTarget := e.do("POST", trips, f.tm, f.tripAt(f.dayFrom, "evening_drop", f.route, f.bus1, f.driver1, "09:15", "10:15"))
	e.expect(conflictTarget, 201, "")
	conflictID := conflictTarget.data()["id"].(string)
	clash := e.do("PATCH", trips+"/"+conflictID+"/driver", f.tm, map[string]any{"driver_id": f.driver2})
	e.expect(clash, 409, "trip_conflict")
	stillOriginal := e.do("GET", trips+"/"+conflictID, f.tm, nil)
	if stillOriginal.data()["driver"].(map[string]any)["id"] != f.driver1 {
		t.Fatalf("a failed replace must preserve the original driver: %v", stillOriginal.data())
	}

	// Inactive driver rejected.
	e.expect(e.do("PATCH", f.base+"/drivers/"+f.driver2+"/status", f.tm, map[string]string{"status": "inactive"}), 200, "")
	e.expect(e.do("PATCH", trips+"/"+todayID+"/driver", f.tm, map[string]any{"driver_id": f.driver2}), 400, "validation_failed")

	// Not-found driver rejected.
	e.expect(e.do("PATCH", trips+"/"+todayID+"/driver", f.tm, map[string]any{"driver_id": "00000000-0000-0000-0000-000000000000"}), 400, "validation_failed")
}

// TestReplaceBus: Part F — same pattern as ReplaceDriver, driver untouched.
func TestReplaceBus(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	created := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(created, 201, "")
	id := created.data()["id"].(string)

	replaced := e.do("PATCH", trips+"/"+id+"/bus", f.tm, map[string]any{"bus_id": f.bus2})
	e.expect(replaced, 200, "")
	d := replaced.data()
	if d["bus"].(map[string]any)["id"] != f.bus2 || d["driver"].(map[string]any)["id"] != f.driver1 {
		t.Fatalf("bus should change, driver must not: %v", d)
	}

	// Bus1 is now free at that slot.
	other, _ := e.routeWithStops(f.admin, f.school, "RS-RB", false, "Y")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", other, f.bus1, f.driver2, "16:00", "17:00")), 201, "")

	// Bus under maintenance rejected.
	e.expect(e.do("PATCH", trips+"/"+id+"/bus", f.tm, map[string]any{"bus_id": f.busMaint}), 400, "validation_failed")
}

// TestBulkUnassign: Part C — per-trip result, mixed success/failure, never a
// blanket pass/fail, and cross-school trip IDs are refused (tenant isolation).
func TestBulkUnassign(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	routeB, _ := e.routeWithStops(f.admin, f.school, "RS-BULKU", false, "Z")
	a := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	b := e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", routeB, f.bus2, f.driver2, "16:00", "17:00"))
	e.expect(a, 201, "")
	e.expect(b, 201, "")
	idA, idB := a.data()["id"].(string), b.data()["id"].(string)

	// Make idB ineligible (started) so the batch has a genuine mixed result.
	e.expect(e.do("POST", trips+"/"+idB+"/override", f.tm, map[string]string{"status": "started", "reason": "go"}), 200, "")

	// Another school's trip, to prove no cross-tenant effect.
	otherBase, otherTM, otherID := e.otherSchoolTrip("ZZB", "000000091")

	r := e.do("POST", trips+"/unassign", f.tm, map[string]any{"trip_ids": []string{idA, idB, otherID}})
	e.expect(r, 200, "")
	results := r.data()["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("results = %v, want 3", results)
	}
	byID := map[string]map[string]any{}
	for _, res := range results {
		m := res.(map[string]any)
		byID[m["trip_id"].(string)] = m
	}
	if byID[idA]["status"] != "unassigned" {
		t.Errorf("idA should succeed: %v", byID[idA])
	}
	if byID[idB]["status"] != "skipped" || byID[idB]["reason"] == "" {
		t.Errorf("idB (started) should be skipped with a reason: %v", byID[idB])
	}
	if byID[otherID]["status"] != "skipped" || byID[otherID]["reason"] != "not found" {
		t.Errorf("a cross-school trip ID must come back not-found, never acted on: %v", byID[otherID])
	}
	// The other school's trip is provably untouched.
	otherAfter := e.do("GET", otherBase+"/trips/"+otherID, otherTM, nil)
	if otherAfter.data()["bus"] == nil {
		t.Errorf("cross-school trip must be unaffected by another school's bulk unassign: %v", otherAfter.data())
	}
}

// otherSchoolTrip creates a second, independent school (distinct code,
// emails and mobiles so it's safe alongside a tripFixture already built in
// the same env) with one assigned trip, for cross-tenant isolation checks.
// mobileSuffix must be a distinct 9-digit numeric string (e.g. "000000091").
// Returns that school's API base, a School Admin token, and the trip ID.
func (e *env) otherSchoolTrip(code, mobileSuffix string) (base, tok, tripID string) {
	e.t.Helper()
	school := e.school(code)
	email := "admin-" + code + "@z.test"
	e.user(&school, models.RoleSchoolAdmin, email, "")
	tok = e.login(email)
	base = "/api/v1/schools/" + school
	route, _ := e.routeWithStops(tok, school, "RS-"+code, false, "Stop")
	bus := e.do("POST", base+"/buses", tok, map[string]any{"vehicle_number": "KA-01-" + code, "capacity": 40})
	e.expect(bus, 201, "")
	driver := e.do("POST", base+"/drivers", tok, map[string]any{"name": "D-" + code, "mobile": "9" + mobileSuffix})
	e.expect(driver, 201, "")
	today := e.dateIn(school, 0)
	trip := e.do("POST", base+"/trips", tok, map[string]any{
		"trip_date": today, "trip_type": "morning_pickup", "route_id": route,
		"bus_id": bus.data()["id"].(string), "driver_id": driver.data()["id"].(string),
	})
	e.expect(trip, 201, "")
	return base, tok, trip.data()["id"].(string)
}

// TestUnassignAuthorizationAndTenant: a driver-app caller is refused (role),
// and a trip ID from another school is treated as not-found (tenant
// isolation) — never revealing it exists.
func TestUnassignAuthorizationAndTenant(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	created := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	id := created.data()["id"].(string)

	e.expect(e.do("POST", trips+"/"+id+"/unassign", f.driver1Tok, nil), 403, "")

	otherSchool := e.school("ZZA")
	e.user(&otherSchool, models.RoleSchoolAdmin, "admin-zza@z.test", "")
	otherTok := e.login("admin-zza@z.test")
	otherBase := "/api/v1/schools/" + otherSchool
	e.expect(e.do("POST", otherBase+"/trips/"+id+"/unassign", otherTok, nil), 404, "")
	e.expect(e.do("PATCH", otherBase+"/trips/"+id+"/driver", otherTok, map[string]any{"driver_id": f.driver1}), 404, "")
}

// TestUnassignConcurrentRequests: two simultaneous unassign requests for the
// same trip must not error or corrupt state — idempotent by design, so both
// succeed and the end state is simply "unassigned".
func TestUnassignConcurrentRequests(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	created := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	id := created.data()["id"].(string)

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	wg.Add(2)
	for i := range statuses {
		go func(i int) {
			defer wg.Done()
			statuses[i] = e.do("POST", trips+"/"+id+"/unassign", f.tm, nil).Status
		}(i)
	}
	wg.Wait()
	for _, s := range statuses {
		if s != 200 {
			t.Errorf("concurrent unassign status = %d, want 200 (idempotent)", s)
		}
	}
	var busID, driverID *string
	if err := e.pool.QueryRow(context.Background(), `SELECT bus_id, driver_id FROM trips WHERE id = $1`, id).Scan(&busID, &driverID); err != nil {
		t.Fatal(err)
	}
	if busID != nil || driverID != nil {
		t.Errorf("bus_id/driver_id should both be null after concurrent unassign")
	}
}

// TestUnassignedTripNotStaleForDriverOrParent: Part H — the old driver no
// longer sees the trip as theirs, and the parent's live tracking does not
// show a stale/blank "current" trip for an unassigned one.
func TestUnassignedTripNotStaleForDriverOrParent(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	created := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	id := created.data()["id"].(string)

	parentsReq := e.do("POST", f.base+"/parents", f.admin, map[string]any{
		"name": "Meena", "mobile": "9000000002", "children": []map[string]string{{"student_id": f.student, "relationship": "mother"}},
	})
	e.expect(parentsReq, 201, "")

	e.expect(e.do("POST", trips+"/"+id+"/unassign", f.tm, nil), 200, "")

	// Old driver no longer sees this trip in their own list.
	driverTrips := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil).list()
	for _, dt := range driverTrips {
		if dt.(map[string]any)["id"] == id {
			t.Fatalf("unassigned trip must not still appear in the old driver's trip list")
		}
	}
	e.expect(e.do("GET", "/api/v1/driver/trips/"+id, f.driver1Tok, nil), 404, "")

	// Parent's live tracking for the child shows no current trip (not a
	// blank-bus/blank-driver one) while the route's only trip is unassigned.
	parentTok := e.otpLogin("9000000002", "parent")
	live := e.do("GET", "/api/v1/parent/children/"+f.student+"/live", parentTok, nil)
	e.expect(live, 200, "")
	if live.data()["trip"] != nil {
		t.Errorf("an unassigned trip must not be shown as the child's current trip: %v", live.data())
	}
}
