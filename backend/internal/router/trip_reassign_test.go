package router_test

import (
	"fmt"
	"testing"
)

// Regression coverage for: after Bulk Unassign clears bus_id/driver_id on a
// block of trips, re-assigning them (single POST /trips, Bulk Assign, or
// Copy) used to be rejected with "route: This route already has an
// overlapping trip at that time." — the pre-existing, now-unassigned trip
// itself was treated as a duplicate-route conflict. The fix: a route-only
// clash that turns out to be exactly that trip (same route+date+type+window,
// no bus, no driver) is filled in (UpdateTripAssignment) instead of
// rejected, while a genuine conflict — a different trip, or a busy bus/
// driver — is still rejected exactly as before.

// TestReassignUnassignedTrip_SingleCreate: unassigning a trip, then POSTing
// a brand-new trip request for the exact same route/date/type/window, must
// succeed and reuse that same trip (never a duplicate row, never a
// self-conflict).
func TestReassignUnassignedTrip_SingleCreate(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	created := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(created, 201, "")
	tripID := created.data()["id"].(string)

	e.expect(e.do("POST", trips+"/"+tripID+"/unassign", f.tm, nil), 200, "")

	reassigned := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(reassigned, 201, "")
	if reassigned.data()["id"] != tripID {
		t.Fatalf("reassign created a NEW trip (%v) instead of filling in the existing one (%s)", reassigned.data()["id"], tripID)
	}
	if reassigned.data()["bus"].(map[string]any)["id"] != f.bus1 || reassigned.data()["driver"].(map[string]any)["id"] != f.driver1 {
		t.Fatalf("reassigned trip missing bus/driver: %v", reassigned.data())
	}

	list := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	n := 0
	for _, x := range list {
		if x.(map[string]any)["route"].(map[string]any)["id"] == f.route {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 trip for this route/date/type after reassigning, got %d: %v", n, list)
	}
}

// TestReassignUnassignedTrips_BulkAssign: the exact reported scenario —
// bulk-create many trips, unassign all of them by filter, then try to
// re-populate driver/bus via Bulk Assign (same route+bus+driver+dates+
// types). Must report everything assigned, nothing skipped, and must not
// have created any duplicate rows.
func TestReassignUnassignedTrips_BulkAssign(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	var dates []string
	for d := 1; d <= 20; d++ {
		dates = append(dates, e.dateIn(f.school, d))
	}
	bulkBody := map[string]any{
		"dates": dates, "trip_types": []string{"morning_pickup", "evening_drop"},
		"route_id": f.route, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "07:30", "morning_end_time": "08:15",
		"evening_start_time": "16:00", "evening_end_time": "17:00",
	}
	first := e.do("POST", trips+"/bulk", f.tm, bulkBody)
	e.expect(first, 200, "")
	if first.data()["created"] != float64(40) {
		t.Fatalf("setup: created = %v, want 40", first.data()["created"])
	}
	// Collect the original 40 trip IDs so we can confirm reassignment reuses
	// them rather than creating new ones.
	before := map[string]bool{}
	for _, d := range dates {
		for _, x := range e.do("GET", trips+"?date="+d, f.tm, nil).list() {
			before[x.(map[string]any)["id"].(string)] = true
		}
	}
	if len(before) != 40 {
		t.Fatalf("setup: found %d trips, want 40", len(before))
	}

	unassign := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"route_id": f.route})
	e.expect(unassign, 200, "")
	if len(unassign.data()["results"].([]any)) != 40 {
		t.Fatalf("unassign results = %v, want 40", unassign.data()["results"])
	}

	again := e.do("POST", trips+"/bulk", f.tm, bulkBody)
	e.expect(again, 200, "")
	if again.data()["created"] != float64(40) || len(again.data()["skipped"].([]any)) != 0 {
		t.Fatalf("reassign bulk create = %v, want 40 created, 0 skipped", again.data())
	}

	after := map[string]bool{}
	assignedCount := 0
	for _, d := range dates {
		for _, x := range e.do("GET", trips+"?date="+d, f.tm, nil).list() {
			m := x.(map[string]any)
			after[m["id"].(string)] = true
			if m["bus"] != nil && m["driver"] != nil {
				assignedCount++
			}
		}
	}
	if len(after) != 40 {
		t.Fatalf("after reassigning: found %d trips, want still 40 (no duplicates created)", len(after))
	}
	for id := range after {
		if !before[id] {
			t.Fatalf("trip %s is a new row, not one of the original 40 — reassignment created a duplicate", id)
		}
	}
	if assignedCount != 40 {
		t.Fatalf("expected all 40 to have both bus and driver after reassigning, got %d", assignedCount)
	}
}

// TestReassignUnassignedTrip_Copy: Copy Day onto a target date that already
// has a matching unassigned trip (same route/type/window) fills it in
// instead of skipping it as a duplicate.
func TestReassignUnassignedTrip_Copy(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	target := e.do("POST", trips, f.tm, f.tripAt(f.tomorrow, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(target, 201, "")
	targetID := target.data()["id"].(string)
	e.expect(e.do("POST", trips+"/"+targetID+"/unassign", f.tm, nil), 200, "")

	cp := e.do("POST", trips+"/copy", f.tm, map[string]any{
		"from_date": f.today, "to_dates": []string{f.tomorrow}, "trip_types": []string{"morning_pickup"},
	})
	e.expect(cp, 200, "")
	if cp.data()["created"] != float64(1) || len(cp.data()["skipped"].([]any)) != 0 {
		t.Fatalf("copy onto an unassigned slot = %v, want 1 created, 0 skipped", cp.data())
	}
	list := e.do("GET", trips+"?date="+f.tomorrow, f.tm, nil).list()
	if len(list) != 1 || list[0].(map[string]any)["id"] != targetID {
		t.Fatalf("copy should have filled in the existing trip %s, not created a new one: %v", targetID, list)
	}
	if list[0].(map[string]any)["bus"] == nil || list[0].(map[string]any)["driver"] == nil {
		t.Fatalf("filled-in trip missing bus/driver: %v", list[0])
	}
}

// TestMorningAndEveningBothReassign_NonOverlapping: Morning Pickup and
// Evening Drop are unassigned together, then both re-assigned by Bulk
// Assign in one request — their non-overlapping windows must never be
// confused with each other (trip_type is part of the slot identity).
func TestMorningAndEveningBothReassign_NonOverlapping(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	morning := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(morning, 201, "")
	evening := e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", f.route, f.bus1, f.driver1, "16:00", "17:00"))
	e.expect(evening, 201, "")

	e.expect(e.bulkUnassignFilter(f.base, f.tm, map[string]any{"route_id": f.route}), 200, "")

	bulk := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": []string{f.today}, "trip_types": []string{"morning_pickup", "evening_drop"},
		"route_id": f.route, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "07:30", "morning_end_time": "08:15",
		"evening_start_time": "16:00", "evening_end_time": "17:00",
	})
	e.expect(bulk, 200, "")
	if bulk.data()["created"] != float64(2) || len(bulk.data()["skipped"].([]any)) != 0 {
		t.Fatalf("reassigning both trip types = %v, want 2 created, 0 skipped", bulk.data())
	}
	list := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	if len(list) != 2 {
		t.Fatalf("expected exactly 2 trips (morning + evening), got %d: %v", len(list), list)
	}
	ids := map[string]bool{morning.data()["id"].(string): true, evening.data()["id"].(string): true}
	for _, x := range list {
		m := x.(map[string]any)
		if !ids[m["id"].(string)] {
			t.Fatalf("trip %v is not one of the original two — a duplicate was created", m["id"])
		}
		if m["bus"] == nil || m["driver"] == nil {
			t.Fatalf("trip %v missing bus/driver after reassigning: %v", m["id"], m)
		}
	}
}

// TestGenuineRouteConflictStillRejected_Single: an existing, STILL ASSIGNED
// trip with a genuinely overlapping window is not something to "fill in" —
// it must still be rejected, both for a single create and for Bulk Assign.
func TestGenuineRouteConflictStillRejected_Single(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")

	// A second trip on the same route, overlapping window, different bus/driver.
	r := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus2, f.driver2, "08:00", "08:30"))
	e.expect(r, 409, "trip_conflict")
	if r.Body["error"].(map[string]any)["fields"].(map[string]any)["route_id"] == nil {
		t.Errorf("conflict should name route_id: %v", r.Body)
	}
}

func TestGenuineRouteConflictStillRejected_BulkAssign(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")

	bulk := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": []string{f.today}, "trip_types": []string{"morning_pickup"},
		"route_id": f.route, "bus_id": f.bus2, "driver_id": f.driver2,
		"morning_start_time": "08:00", "morning_end_time": "08:30",
	})
	e.expect(bulk, 200, "")
	if bulk.data()["created"] != float64(0) || len(bulk.data()["skipped"].([]any)) != 1 {
		t.Fatalf("genuine overlap must still be skipped: %v", bulk.data())
	}
	reason := bulk.data()["skipped"].([]any)[0].(map[string]any)["reason"].(string)
	if reason == "" {
		t.Fatalf("skip reason must not be empty: %v", bulk.data())
	}
}

// TestBusConflictStillDetectedWhenReassigning_Single / _BulkAssign: the
// route's own slot is free (unassigned), but the requested bus is genuinely
// busy elsewhere at an overlapping time — that must still be rejected, not
// silently approved just because the route-dimension check now has a
// reassignment escape hatch.
func TestBusConflictStillDetectedWhenReassigning_Single(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-BC1", false, "Y")

	target := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(target, 201, "")
	e.expect(e.do("POST", trips+"/"+target.data()["id"].(string)+"/unassign", f.tm, nil), 200, "")

	// bus1 is busy on a different route at an overlapping time.
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", other, f.bus1, f.driver2, "07:45", "08:30")), 201, "")

	r := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(r, 409, "trip_conflict")
	if r.Body["error"].(map[string]any)["fields"].(map[string]any)["bus_id"] == nil {
		t.Errorf("conflict should name bus_id, not be silently reassigned: %v", r.Body)
	}
}

func TestBusConflictStillDetectedWhenReassigning_BulkAssign(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-BC2", false, "Y")

	target := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(target, 201, "")
	e.expect(e.do("POST", trips+"/"+target.data()["id"].(string)+"/unassign", f.tm, nil), 200, "")

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", other, f.bus1, f.driver2, "07:45", "08:30")), 201, "")

	bulk := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": []string{f.today}, "trip_types": []string{"morning_pickup"},
		"route_id": f.route, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "07:30", "morning_end_time": "08:15",
	})
	e.expect(bulk, 200, "")
	if bulk.data()["created"] != float64(0) || len(bulk.data()["skipped"].([]any)) != 1 {
		t.Fatalf("bus conflict must still be skipped, not silently reassigned: %v", bulk.data())
	}
	if fmt.Sprint(bulk.data()["skipped"].([]any)[0].(map[string]any)["reason"]) == "" {
		t.Fatalf("skip reason must not be empty: %v", bulk.data())
	}
}

// TestDriverConflictStillDetectedWhenReassigning_BulkAssign: same as the bus
// case above, for the driver dimension.
func TestDriverConflictStillDetectedWhenReassigning_BulkAssign(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-DC1", false, "Y")

	target := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(target, 201, "")
	e.expect(e.do("POST", trips+"/"+target.data()["id"].(string)+"/unassign", f.tm, nil), 200, "")

	// driver1 is busy on a different route at an overlapping time.
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", other, f.bus2, f.driver1, "07:45", "08:30")), 201, "")

	bulk := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": []string{f.today}, "trip_types": []string{"morning_pickup"},
		"route_id": f.route, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "07:30", "morning_end_time": "08:15",
	})
	e.expect(bulk, 200, "")
	if bulk.data()["created"] != float64(0) || len(bulk.data()["skipped"].([]any)) != 1 {
		t.Fatalf("driver conflict must still be skipped, not silently reassigned: %v", bulk.data())
	}
}
