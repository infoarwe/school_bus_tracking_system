package router_test

import "testing"

// Regression: a driver assigned more than one trip of the same trip type on the
// same day (e.g. Morning: RS-7 and RS-8) must see every one of them in
// GET /driver/trips, each keeping its own trip ID, route and schedule — not
// just the first one for that type.
func TestDriverTripsShowsMultipleRoutesPerType(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	other, _ := e.routeWithStops(f.admin, f.school, "RS-R2", false, "Y")

	morning1 := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:00", "07:45")).data()["id"].(string)
	morning2 := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", other, f.bus2, f.driver1, "07:50", "08:30")).data()["id"].(string)
	evening1 := e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", f.route, f.bus1, f.driver1, "16:00", "16:45")).data()["id"].(string)
	evening2 := e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", other, f.bus2, f.driver1, "16:50", "17:30")).data()["id"].(string)

	resp := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil)
	e.expect(resp, 200, "")
	list := resp.list()
	if len(list) != 4 {
		t.Fatalf("driver app sees %d trips, want 4: %v", len(list), list)
	}

	seen := map[string]string{} // trip id -> trip_type
	for _, x := range list {
		m := x.(map[string]any)
		seen[m["id"].(string)] = m["trip_type"].(string)
	}
	for _, id := range []string{morning1, morning2} {
		if seen[id] != "morning_pickup" {
			t.Errorf("trip %s missing or wrong type: %v", id, seen)
		}
	}
	for _, id := range []string{evening1, evening2} {
		if seen[id] != "evening_drop" {
			t.Errorf("trip %s missing or wrong type: %v", id, seen)
		}
	}

	// Morning Pickup is listed before Evening Drop (existing ordering contract).
	if list[0].(map[string]any)["trip_type"] != "morning_pickup" {
		t.Errorf("first trip type = %v, want morning_pickup", list[0].(map[string]any)["trip_type"])
	}
}

// Regression: a driver's permanent route assignment must still be materialized
// into today's trips even when the driver already has an unrelated, explicitly
// scheduled trip for the same date/trip type (different route and bus) — the
// two must not be confused with each other.
func TestDriverTripsPermanentAssignmentNotSuppressedByUnrelatedTrip(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	e.expect(e.do("POST", f.base+"/driver-assignments", f.admin, f.assignment(f.route, "morning_pickup", f.driver1, f.bus1)), 201, "")

	other, _ := e.routeWithStops(f.admin, f.school, "RS-R3", false, "Y")
	explicit := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", other, f.bus2, f.driver1, "07:00", "07:45")).data()["id"].(string)

	resp := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil)
	e.expect(resp, 200, "")
	list := resp.list()
	if len(list) != 2 {
		t.Fatalf("driver app sees %d trips, want 2 (permanent assignment + explicit trip): %v", len(list), list)
	}
	routes := map[string]bool{}
	for _, x := range list {
		m := x.(map[string]any)
		routes[m["route"].(map[string]any)["code"].(string)] = true
		if m["id"] == explicit && m["trip_type"] != "morning_pickup" {
			t.Errorf("explicit trip has wrong type: %v", m)
		}
	}
	if !routes["RS-01"] || !routes["RS-R3"] {
		t.Errorf("want both the permanent assignment's route (RS-01) and the explicit trip's route (RS-R3), got %v", routes)
	}
}
