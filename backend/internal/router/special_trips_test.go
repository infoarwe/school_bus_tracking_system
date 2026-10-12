package router_test

import (
	"context"
	"testing"
)

// specialTrip builds a POST /trips body for a special_trip. routeID == "" means
// a routeless outing (title and waypoints then apply); notify == nil omits
// notify_parents (server default: true).
func (f *tripFixture) specialTrip(date, routeID, title string, waypoints []map[string]any, bus, driver string, notify *bool) map[string]any {
	body := map[string]any{"trip_date": date, "trip_type": "special_trip", "bus_id": bus, "driver_id": driver}
	if routeID != "" {
		body["route_id"] = routeID
	}
	if title != "" {
		body["title"] = title
	}
	if waypoints != nil {
		body["waypoints"] = waypoints
	}
	if notify != nil {
		body["notify_parents"] = *notify
	}
	return body
}

func wp(name string, lat, lng float64) map[string]any {
	return map[string]any{"name": name, "latitude": lat, "longitude": lng}
}

func (e *env) parentNotifyCount(tripID string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM notification_recipients r
		JOIN notifications n ON n.id = r.notification_id JOIN users u ON u.id = r.user_id
		WHERE u.role = 'parent' AND n.trip_id = $1`, tripID).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Requirement #1/#2: a route can support Morning Pickup, Evening Drop and
// Special Trip/Outing together, and is reusable across all three.
func TestRouteSupportsAllThreeTripTypes(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	routes := f.base + "/routes"
	r := e.do("POST", routes, f.admin, map[string]any{"name": "Outing Route", "code": "RS-03",
		"supports_pickup": true, "supports_drop": true, "supports_outing": true})
	e.expect(r, 201, "")
	route := r.data()["id"].(string)
	e.expect(e.do("POST", routes+"/"+route+"/stops", f.admin,
		map[string]any{"name": "Stop A", "latitude": 11.01, "longitude": 76.96}), 201, "")

	trips := f.base + "/trips"
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", route, f.bus1, f.driver1)), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "evening_drop", route, f.bus2, f.driver2)), 201, "")
	out := e.do("POST", trips, f.tm, f.specialTrip(f.today, route, "", nil, f.bus1, f.driver1, nil))
	// bus1/driver1 already used for morning_pickup today, but special_trip is its
	// own slot (rule 4 extended to the third type): must still succeed.
	e.expect(out, 201, "")
	if out.data()["route"].(map[string]any)["code"] != "RS-03" {
		t.Errorf("outing route = %v", out.data())
	}

	// A route not marked supports_outing cannot be used as a special trip's route.
	bad := e.do("POST", trips, f.tm, f.specialTrip(f.tomorrow, f.route, "", nil, f.bus1, f.driver1, nil))
	e.expect(bad, 400, "validation_failed")
}

// Requirement #4: Evening Drop's stop order is independently configurable, not
// assumed to be the Morning Pickup order reversed.
func TestIndependentEveningDropOrder(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	routes := f.base + "/routes"
	r := e.do("POST", routes, f.admin, map[string]any{"name": "Both Ways", "code": "RS-04"})
	e.expect(r, 201, "")
	route := r.data()["id"].(string)
	stop := func(name string) string {
		s := e.do("POST", routes+"/"+route+"/stops", f.admin, map[string]any{
			"name": name, "latitude": 11.01, "longitude": 76.96, "pickup_time": "07:10", "drop_time": "16:40",
		})
		e.expect(s, 201, "")
		return s.data()["id"].(string)
	}
	a, b, c := stop("A"), stop("B"), stop("C")
	trips := f.base + "/trips"

	// Evening Drop cannot run until the drop order is configured, even though
	// every stop already has a drop_time.
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "evening_drop", route, f.bus1, f.driver1)), 400, "validation_failed")

	// Configure the evening order as C, A, B — not the reverse of A, B, C.
	order := e.do("PUT", routes+"/"+route+"/stops/drop-order", f.admin, map[string]any{"drop_stop_ids": []string{c, a, b}})
	e.expect(order, 200, "")

	ev := e.do("POST", trips, f.tm, f.trip(f.today, "evening_drop", route, f.bus1, f.driver1))
	e.expect(ev, 201, "")
	id := ev.data()["id"].(string)

	detail := e.do("GET", trips+"/"+id, f.tm, nil)
	e.expect(detail, 200, "")
	stops := detail.data()["stops"].([]any)
	if len(stops) != 3 {
		t.Fatalf("stops = %v", stops)
	}
	var gotOrder []string
	for _, s := range stops {
		gotOrder = append(gotOrder, s.(map[string]any)["id"].(string))
	}
	if gotOrder[0] != c || gotOrder[1] != a || gotOrder[2] != b {
		t.Errorf("evening order = %v, want [C A B]", gotOrder)
	}

	// Sending a mismatched list (not every drop stop, exactly once) is rejected.
	bad := e.do("PUT", routes+"/"+route+"/stops/drop-order", f.admin, map[string]any{"drop_stop_ids": []string{a, b}})
	e.expect(bad, 400, "validation_failed")
}

// Requirement #5/#6/#8: a routeless Special Trip/Outing with multiple ad-hoc
// destinations, no parent notification, but full staff tracking.
func TestSpecialTripWithoutRoute(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	falseV := false

	waypoints := []map[string]any{wp("Science Museum", 11.02, 76.97), wp("City Park", 11.03, 76.98)}
	out := e.do("POST", trips, f.tm, f.specialTrip(f.today, "", "Museum Trip", waypoints, f.bus1, f.driver1, &falseV))
	e.expect(out, 201, "")
	outID := out.data()["id"].(string)
	if out.data()["title"] != "Museum Trip" || out.data()["route"] != nil || out.data()["notify_parents"] != false {
		t.Fatalf("outing = %v", out.data())
	}

	// Title is required when there is no route.
	e.expect(e.do("POST", trips, f.tm, f.specialTrip(f.tomorrow, "", "", waypoints, f.bus2, f.driver2, nil)), 400, "validation_failed")
	// At least one waypoint is required when there is no route.
	e.expect(e.do("POST", trips, f.tm, f.specialTrip(f.tomorrow, "", "No stops", nil, f.bus2, f.driver2, nil)), 400, "validation_failed")

	// Delay reporting works on a routeless outing (regression: the alerts
	// lookup must not require a route).
	delay := e.do("POST", trips+"/"+outID+"/delays", f.tm, map[string]any{"minutes": 10, "reason": "traffic"})
	e.expect(delay, 201, "")
	if delay.data()["route_code"] != "" || delay.data()["title"] != "Museum Trip" {
		t.Errorf("delay on outing = %v", delay.data())
	}

	// Starting and completing the outing must not notify parents, but staff
	// read paths (detail + live progress) must work exactly as for any trip.
	e.expect(e.do("POST", trips+"/"+outID+"/override", f.tm, map[string]string{"status": "started", "reason": "go"}), 200, "")
	e.expect(e.do("POST", trips+"/"+outID+"/override", f.tm, map[string]string{"status": "completed", "reason": "done"}), 200, "")
	if n := e.parentNotifyCount(outID); n != 0 {
		t.Errorf("notify_parents=false but parents notified %d times", n)
	}
	detail := e.do("GET", trips+"/"+outID, f.tm, nil)
	e.expect(detail, 200, "")
	if stops := detail.data()["stops"].([]any); len(stops) != 2 {
		t.Errorf("outing stops = %v", stops)
	}
	e.expect(e.do("GET", trips+"/"+outID+"/progress", f.tm, nil), 200, "")

	// Bus/driver conflict: one special_trip per bus (and per driver) per date,
	// just like the other two trip types.
	e.expect(e.do("POST", trips, f.tm, f.specialTrip(f.dayFrom, "", "Trip A", waypoints, f.bus1, f.driver1, nil)), 201, "")
	busClash := e.do("POST", trips, f.tm, f.specialTrip(f.dayFrom, "", "Trip B", waypoints, f.bus1, f.driver2, nil))
	e.expect(busClash, 409, "trip_conflict")
	driverClash := e.do("POST", trips, f.tm, f.specialTrip(f.dayFrom, "", "Trip C", waypoints, f.bus2, f.driver1, nil))
	e.expect(driverClash, 409, "trip_conflict")
}

// Requirement #5/#6: a Special Trip/Outing based on a template route notifies
// that route's usual parents when enabled.
func TestSpecialTripWithRouteNotifies(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	routes := f.base + "/routes"
	r := e.do("POST", routes, f.admin, map[string]any{"name": "Outing Route", "code": "RS-05",
		"supports_pickup": true, "supports_drop": false, "supports_outing": true})
	e.expect(r, 201, "")
	route := r.data()["id"].(string)
	stopReq := e.do("POST", routes+"/"+route+"/stops", f.admin,
		map[string]any{"name": "Stop A", "latitude": 11.01, "longitude": 76.96})
	e.expect(stopReq, 201, "")
	routeStop := stopReq.data()["id"].(string)
	e.expect(e.do("PUT", f.base+"/students/"+f.student+"/assignment", f.admin,
		map[string]any{"route_id": route, "pickup_stop_id": routeStop}), 200, "")
	e.expect(e.do("POST", f.base+"/parents", f.admin, map[string]any{
		"name": "Meena", "mobile": "9000000002", "children": []map[string]string{{"student_id": f.student, "relationship": "mother"}},
	}), 201, "")

	trueV := true
	trips := f.base + "/trips"
	out := e.do("POST", trips, f.tm, f.specialTrip(f.today, route, "", nil, f.bus1, f.driver1, &trueV))
	e.expect(out, 201, "")
	outID := out.data()["id"].(string)
	e.expect(e.do("POST", trips+"/"+outID+"/override", f.tm, map[string]string{"status": "started", "reason": "go"}), 200, "")
	if n := e.parentNotifyCount(outID); n == 0 {
		t.Errorf("notify_parents=true with a template route should have notified parents")
	}
}

// Requirement #9: tenant isolation and the existing route/trip behaviour are
// unaffected by the new trip type (full coverage lives in isolation_test.go
// and the other trips_test.go cases, which already run unmodified against the
// new schema — this is a focused smoke check for the new fields specifically).
func TestExistingRouteBehaviourUnaffected(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	r := e.do("GET", f.base+"/routes/"+f.route, f.tm, nil)
	e.expect(r, 200, "")
	if r.data()["supports_outing"] != false {
		t.Errorf("existing route supports_outing = %v, want false", r.data()["supports_outing"])
	}
	trip := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1))
	e.expect(trip, 201, "")
	if trip.data()["title"] != "" || trip.data()["notify_parents"] != true {
		t.Errorf("ordinary trip = %v", trip.data())
	}
}
