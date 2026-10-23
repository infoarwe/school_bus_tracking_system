package router_test

// Coverage for GET /schools/{id}/routes/{routeID}/schedule: what a route's
// Morning Pickup/Evening Drop actually run as, derived live from its stops'
// own pickup_time/drop_time — the source Daily Trips and Trip Planner's Bulk
// Assign both prefill their scheduled-time fields from.

import "testing"

type schedWindow struct {
	Supported bool
	Start     any
	End       any
}

func (e *env) routeSchedule(schoolID, routeID, tok string) (morning, evening schedWindow) {
	e.t.Helper()
	r := e.do("GET", "/api/v1/schools/"+schoolID+"/routes/"+routeID+"/schedule", tok, nil)
	e.expect(r, 200, "")
	m := r.data()["morning_pickup"].(map[string]any)
	ev := r.data()["evening_drop"].(map[string]any)
	return schedWindow{m["supported"].(bool), m["start_time"], m["end_time"]},
		schedWindow{ev["supported"].(bool), ev["start_time"], ev["end_time"]}
}

// stopWithTimes creates a stop with an explicit pickup_time and/or drop_time
// (nil = omit that field), unlike the shared routeWithStops helper which
// never sets either. Returns the new stop's ID.
func (e *env) stopWithTimes(tok, schoolID, routeID, name string, pickup, drop *string) string {
	e.t.Helper()
	body := map[string]any{"name": name, "latitude": 11.01, "longitude": 76.96}
	if pickup != nil {
		body["pickup_time"] = *pickup
	}
	if drop != nil {
		body["drop_time"] = *drop
	}
	r := e.do("POST", "/api/v1/schools/"+schoolID+"/routes/"+routeID+"/stops", tok, body)
	e.expect(r, 201, "")
	return r.data()["id"].(string)
}

// setDropOrder configures the Evening Drop leg's travel order (every stop in
// dropStopIDs, in the given order) — required before any of those stops'
// drop_time counts toward the route's derived Evening Drop window (a stop's
// drop_sequence is null, meaning "not part of the evening leg", until this
// is called at least once; see CLAUDE.md and TestIndependentEveningDropOrder).
func (e *env) setDropOrder(tok, schoolID, routeID string, dropStopIDs []string) {
	e.t.Helper()
	e.expect(e.do("PUT", "/api/v1/schools/"+schoolID+"/routes/"+routeID+"/stops/drop-order", tok,
		map[string]any{"drop_stop_ids": dropStopIDs}), 200, "")
}

func ptr(s string) *string { return &s }

// TestRouteScheduleMorningOnly: a route that only runs Morning Pickup reports
// a derived window for it and supported=false for Evening Drop.
func TestRouteScheduleMorningOnly(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	route, _ := e.routeWithStops(f.admin, f.school, "RS-M1", true) // pickupOnly, no stops yet
	e.stopWithTimes(f.admin, f.school, route, "A", ptr("07:00"), nil)
	e.stopWithTimes(f.admin, f.school, route, "B", ptr("07:30"), nil)

	morning, evening := e.routeSchedule(f.school, route, f.admin)
	if !morning.Supported || morning.Start != "07:00" || morning.End != "07:45" {
		t.Errorf("morning = %+v, want start 07:00 end 07:45 (max + 15 min buffer)", morning)
	}
	if evening.Supported {
		t.Errorf("evening = %+v, want supported=false (route is pickup-only)", evening)
	}
}

// TestRouteScheduleEveningOnly: the Evening Drop counterpart.
func TestRouteScheduleEveningOnly(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	r := e.do("POST", f.base+"/routes", f.admin, map[string]any{"name": "RS-E1", "code": "RS-E1", "supports_pickup": false, "supports_drop": true})
	e.expect(r, 201, "")
	route := r.data()["id"].(string)
	a := e.stopWithTimes(f.admin, f.school, route, "A", nil, ptr("16:00"))
	b := e.stopWithTimes(f.admin, f.school, route, "B", nil, ptr("16:30"))
	e.setDropOrder(f.admin, f.school, route, []string{a, b})

	morning, evening := e.routeSchedule(f.school, route, f.admin)
	if morning.Supported {
		t.Errorf("morning = %+v, want supported=false (route is drop-only)", morning)
	}
	if !evening.Supported || evening.Start != "16:00" || evening.End != "16:45" {
		t.Errorf("evening = %+v, want start 16:00 end 16:45", evening)
	}
}

// TestRouteScheduleBoth: a route running both trip types gets both windows
// independently — one is never derived from the other.
func TestRouteScheduleBoth(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	r := e.do("POST", f.base+"/routes", f.admin, map[string]any{"name": "RS-B1", "code": "RS-B1", "supports_pickup": true, "supports_drop": true})
	e.expect(r, 201, "")
	route := r.data()["id"].(string)
	a := e.stopWithTimes(f.admin, f.school, route, "A", ptr("07:10"), ptr("16:05"))
	b := e.stopWithTimes(f.admin, f.school, route, "B", ptr("07:45"), ptr("16:40"))
	e.setDropOrder(f.admin, f.school, route, []string{a, b})

	morning, evening := e.routeSchedule(f.school, route, f.admin)
	if !morning.Supported || morning.Start != "07:10" || morning.End != "08:00" {
		t.Errorf("morning = %+v, want start 07:10 end 08:00", morning)
	}
	if !evening.Supported || evening.Start != "16:05" || evening.End != "16:55" {
		t.Errorf("evening = %+v, want start 16:05 end 16:55", evening)
	}
}

// TestRouteScheduleMissingConfiguration: a route that supports both types but
// whose stops carry no time at all reports supported=true with null times —
// never an invented fallback.
func TestRouteScheduleMissingConfiguration(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	r := e.do("POST", f.base+"/routes", f.admin, map[string]any{"name": "RS-N1", "code": "RS-N1", "supports_pickup": true, "supports_drop": true})
	e.expect(r, 201, "")
	route := r.data()["id"].(string)
	e.expect(e.do("POST", f.base+"/routes/"+route+"/stops", f.admin, map[string]any{"name": "A", "latitude": 11.01, "longitude": 76.96}), 201, "")

	morning, evening := e.routeSchedule(f.school, route, f.admin)
	if !morning.Supported || morning.Start != nil || morning.End != nil {
		t.Errorf("morning = %+v, want supported=true, start/end nil (nothing to derive)", morning)
	}
	if !evening.Supported || evening.Start != nil || evening.End != nil {
		t.Errorf("evening = %+v, want supported=true, start/end nil", evening)
	}
}
