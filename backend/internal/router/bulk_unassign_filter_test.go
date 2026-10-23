package router_test

// Coverage for Bulk Unassign's date-range/All Dates filter mode (Trip
// Planner's cross-date flow), which fixes the previous limitation: the
// original Bulk Unassign only ever operated on whatever single day's trips
// happened to be loaded in Daily Trips' table. POST /trips/unassign now also
// accepts {all_dates|from|to, route_id?, bus_id?, driver_id?, trip_type?} as
// an alternative to an explicit trip_ids list, resolved server-side.

import (
	"fmt"
	"testing"
)

// bulkUnassignFilter POSTs a filter-mode bulk-unassign request.
func (e *env) bulkUnassignFilter(base, tok string, body map[string]any) resp {
	return e.do("POST", base+"/trips/unassign", tok, body)
}

func (e *env) bulkUnassignPreview(base, tok string, body map[string]any) resp {
	return e.do("POST", base+"/trips/unassign/preview", tok, body)
}

// TestBulkUnassignDateRange: a from/to range correctly spans multiple dates
// and multiple routes, and never touches a trip outside that range.
func TestBulkUnassignDateRange(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-DR2", false, "Y")
	day3 := e.dateIn(f.school, 3)
	day4 := e.dateIn(f.school, 4)

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "") // before range
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.tomorrow, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.dayFrom, "evening_drop", other, f.bus2, f.driver2, "16:00", "17:00")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day3, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day4, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "") // after range

	// Preview first: range covers tomorrow..day3 (3 trips: tomorrow, dayFrom, day3).
	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"from": f.tomorrow, "to": day3})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(3) || preview.data()["from"] != f.tomorrow || preview.data()["to"] != day3 {
		t.Fatalf("preview = %v", preview.data())
	}

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"from": f.tomorrow, "to": day3})
	e.expect(r, 200, "")
	results := r.data()["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("results = %v, want 3 (tomorrow, dayFrom, day3 only)", results)
	}
	for _, res := range results {
		if res.(map[string]any)["status"] != "unassigned" {
			t.Errorf("expected all 3 in range to succeed: %v", res)
		}
	}

	// Trips outside the range are untouched.
	list := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	if len(list) != 1 || list[0].(map[string]any)["bus"] == nil {
		t.Errorf("today's trip (before the range) must still be assigned: %v", list)
	}
	list4 := e.do("GET", trips+"?date="+day4, f.tm, nil).list()
	if len(list4) != 1 || list4[0].(map[string]any)["bus"] == nil {
		t.Errorf("day4's trip (after the range) must still be assigned: %v", list4)
	}
}

// TestBulkUnassignAllDates: all_dates with no other filters reaches every
// eligible trip regardless of date, including ones far beyond any typical
// planning window.
func TestBulkUnassignAllDates(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	far := e.dateIn(f.school, 85)

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(far, "morning_pickup", f.route, f.bus2, f.driver2, "07:30", "08:15")), 201, "")

	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(2) {
		t.Fatalf("preview = %v, want count 2 (today and 85 days out)", preview.data())
	}

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(r, 200, "")
	results := r.data()["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %v, want 2", results)
	}
	farList := e.do("GET", trips+"?date="+far, f.tm, nil).list()
	if len(farList) != 1 || farList[0].(map[string]any)["bus"] != nil {
		t.Errorf("the far-future trip must be unassigned too: %v", farList)
	}
}

// TestBulkUnassignDriverFilter: filtering by driver_id only unassigns that
// driver's trips across every matching date — a different driver's trips on
// the very same dates are left alone.
func TestBulkUnassignDriverFilter(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-DF2", false, "Y")

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", other, f.bus2, f.driver2, "16:00", "17:00")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.tomorrow, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"all_dates": true, "driver_id": f.driver1})
	e.expect(r, 200, "")
	if len(r.data()["results"].([]any)) != 2 {
		t.Fatalf("results = %v, want exactly driver1's 2 trips", r.data()["results"])
	}

	// Driver2's trip, same date, is untouched.
	list := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	var driver2Trip map[string]any
	for _, x := range list {
		m := x.(map[string]any)
		if m["driver"] != nil && m["driver"].(map[string]any)["id"] == f.driver2 {
			driver2Trip = m
		}
	}
	if driver2Trip == nil || driver2Trip["bus"] == nil {
		t.Errorf("driver2's trip must remain assigned: %v", list)
	}
}

// TestBulkUnassignPaginationNoSilentTruncation: a filter matching more trips
// than any typical default page size must return every one of them in the
// results — never silently limited to a first page.
func TestBulkUnassignPaginationNoSilentTruncation(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	const n = 25 // more than a typical default page_size of 20
	for i := 0; i < n; i++ {
		route, _ := e.routeWithStops(f.admin, f.school, fmt.Sprintf("RS-PG%d", i), false, "Z")
		// Each trip gets its own 15-minute slot (i*20 minutes from midnight) so
		// none genuinely overlap on the shared bus1/driver1 — only the route differs.
		start, end := i*20, i*20+15
		startStr := fmt.Sprintf("%02d:%02d", start/60, start%60)
		endStr := fmt.Sprintf("%02d:%02d", end/60, end%60)
		e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", route, f.bus1, f.driver1, startStr, endStr)), 201, "")
	}
	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(n) {
		t.Fatalf("preview count = %v, want %d", preview.data()["count"], n)
	}
	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(r, 200, "")
	if len(r.data()["results"].([]any)) != n {
		t.Fatalf("results = %d, want all %d (no silent page truncation)", len(r.data()["results"].([]any)), n)
	}
}

// TestBulkUnassignMixedSuccessAndFailure: a filter match that includes a
// started trip reports it skipped with a reason, while the rest still
// succeed — never a blanket failure for the whole batch.
func TestBulkUnassignMixedSuccessAndFailure(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	startedRoute, _ := e.routeWithStops(f.admin, f.school, "RS-MX", false, "Z")

	ok := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(ok, 201, "")
	started := e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", startedRoute, f.bus2, f.driver2, "16:00", "17:00"))
	e.expect(started, 201, "")
	startedID := started.data()["id"].(string)
	e.expect(e.do("POST", trips+"/"+startedID+"/override", f.tm, map[string]string{"status": "started", "reason": "go"}), 200, "")

	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(2) || preview.data()["eligible"] != float64(1) || preview.data()["ineligible"] != float64(1) {
		t.Fatalf("preview = %v, want count 2, eligible 1, ineligible 1 (the started trip)", preview.data())
	}

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(r, 200, "")
	results := r.data()["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %v, want 2", results)
	}
	byID := map[string]map[string]any{}
	for _, res := range results {
		m := res.(map[string]any)
		byID[m["trip_id"].(string)] = m
	}
	if byID[ok.data()["id"].(string)]["status"] != "unassigned" {
		t.Errorf("the open trip should succeed: %v", byID)
	}
	if byID[startedID]["status"] != "skipped" || byID[startedID]["reason"] == "" {
		t.Errorf("the started trip should be skipped with a reason: %v", byID)
	}
}

// TestBulkUnassignFilterPermissionsAndTenant: a driver-app token is refused,
// and the filter only ever resolves the caller's own school's trips.
func TestBulkUnassignFilterPermissionsAndTenant(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", f.base+"/trips/unassign", f.driver1Tok, map[string]any{"all_dates": true}), 403, "")

	// A completely bare filter is rejected (never a silent "all").
	e.expect(e.do("POST", f.base+"/trips/unassign", f.tm, map[string]any{}), 400, "validation_failed")

	// But a route_id alone IS a meaningful scope now — its date span is
	// auto-detected from the matching trips, no explicit date required. (Use
	// preview here, not the real operation, so the trip below is still
	// assigned for the cross-tenant check that follows.)
	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"route_id": f.route})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(1) || preview.data()["from"] != f.today || preview.data()["to"] != f.today {
		t.Fatalf("route_id-only preview = %v, want count 1, from/to = today", preview.data())
	}

	// A second school's identical all_dates request never touches school A's trip.
	otherBase, otherTM, _ := e.otherSchoolTrip("ZBU", "000000092")
	e.expect(e.bulkUnassignFilter(otherBase, otherTM, map[string]any{"all_dates": true}), 200, "")
	list := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	if len(list) != 1 || list[0].(map[string]any)["bus"] == nil {
		t.Errorf("school A's trip must be unaffected by school B's bulk unassign: %v", list)
	}
}

// TestBulkUnassignAutoDetectRoute: selecting just a route — no all_dates, no
// from/to at all — is a meaningful scope on its own. Its applicable period is
// auto-detected purely from that route's own matching trips, which may land
// on widely non-consecutive dates; the detected span is exactly their
// min/max (never inflated to imply every day in between has a trip), and a
// different route on one of the very same dates is left untouched.
func TestBulkUnassignAutoDetectRoute(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-AR2", false, "Y")
	day5 := e.dateIn(f.school, 5)
	day20 := e.dateIn(f.school, 20)

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day5, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day20, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	// Same first date, a different route: must stay untouched.
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", other, f.bus2, f.driver2, "16:00", "17:00")), 201, "")

	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"route_id": f.route})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(3) || preview.data()["eligible"] != float64(3) ||
		preview.data()["from"] != f.today || preview.data()["to"] != day20 {
		t.Fatalf("auto-detected preview = %v, want count/eligible 3, span today..day20 (not every day in between)", preview.data())
	}

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"route_id": f.route})
	e.expect(r, 200, "")
	if len(r.data()["results"].([]any)) != 3 {
		t.Fatalf("results = %v, want exactly the route's 3 trips", r.data()["results"])
	}
	otherList := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	var otherTrip map[string]any
	for _, x := range otherList {
		m := x.(map[string]any)
		if m["route"] != nil && m["route"].(map[string]any)["id"] == other {
			otherTrip = m
		}
	}
	if otherTrip == nil || otherTrip["bus"] == nil {
		t.Errorf("the other route's trip on the same date must remain assigned: %v", otherList)
	}
}

// TestBulkUnassignAutoDetectDriver: a driver filter alone, with no date
// signal, auto-detects that driver's own period spanning from the earliest
// possible trip date (today — trip creation itself rejects a past
// trip_date, so "today" is as historical as a matching trip can ever be) out
// to a far-future date — a different driver's trips in that same span, even
// on the same dates, are never touched.
func TestBulkUnassignAutoDetectDriver(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-AD2", false, "Y")
	far := e.dateIn(f.school, 85)

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(far, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	// Driver2, same dates: must stay untouched.
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", other, f.bus2, f.driver2, "16:00", "17:00")), 201, "")

	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"driver_id": f.driver1})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(2) || preview.data()["from"] != f.today || preview.data()["to"] != far {
		t.Fatalf("driver-only preview = %v, want count 2, span today..85 days out", preview.data())
	}

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"driver_id": f.driver1})
	e.expect(r, 200, "")
	if len(r.data()["results"].([]any)) != 2 {
		t.Fatalf("results = %v, want exactly driver1's 2 trips", r.data()["results"])
	}
	list := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	var d2Trip map[string]any
	for _, x := range list {
		m := x.(map[string]any)
		if m["driver"] != nil && m["driver"].(map[string]any)["id"] == f.driver2 {
			d2Trip = m
		}
	}
	if d2Trip == nil || d2Trip["bus"] == nil {
		t.Errorf("driver2's trip must remain assigned: %v", list)
	}
}

// TestBulkUnassignAutoDetectBus: same auto-detection, this time for a bus
// filter alone.
func TestBulkUnassignAutoDetectBus(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	day5 := e.dateIn(f.school, 5)

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day5, "morning_pickup", f.route, f.bus1, f.driver2, "07:30", "08:15")), 201, "")

	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"bus_id": f.bus1})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(2) || preview.data()["from"] != f.today || preview.data()["to"] != day5 {
		t.Fatalf("bus-only preview = %v, want count 2, span today..day5", preview.data())
	}
	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"bus_id": f.bus1})
	e.expect(r, 200, "")
	if len(r.data()["results"].([]any)) != 2 {
		t.Fatalf("results = %v, want both of bus1's trips", r.data()["results"])
	}
}

// TestBulkUnassignCombinedFiltersIntersection: route_id AND driver_id
// together must match their intersection only — a trip matching just one of
// the two dimensions is left alone.
func TestBulkUnassignCombinedFiltersIntersection(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-CF2", false, "Y")

	// route(f.route) + driver1: matches both filters.
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	// route(f.route) + driver2: matches route only.
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", f.route, f.bus2, f.driver2, "16:00", "17:00")), 201, "")
	// other route + driver1: matches driver only (different time to avoid a driver1 clash).
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.tomorrow, "morning_pickup", other, f.bus2, f.driver1, "09:00", "10:00")), 201, "")

	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"route_id": f.route, "driver_id": f.driver1})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(1) || preview.data()["from"] != f.today || preview.data()["to"] != f.today {
		t.Fatalf("intersection preview = %v, want exactly 1 (route+driver both match)", preview.data())
	}

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"route_id": f.route, "driver_id": f.driver1})
	e.expect(r, 200, "")
	if len(r.data()["results"].([]any)) != 1 {
		t.Fatalf("results = %v, want exactly 1 (the intersection trip)", r.data()["results"])
	}
	// The other two (route-only match, driver-only match) remain assigned.
	today := e.do("GET", trips+"?date="+f.today, f.tm, nil).list()
	for _, x := range today {
		m := x.(map[string]any)
		if m["driver"] != nil && m["driver"].(map[string]any)["id"] == f.driver2 && m["bus"] == nil {
			t.Errorf("route-only match (driver2) must remain assigned: %v", m)
		}
	}
	tomorrowList := e.do("GET", trips+"?date="+f.tomorrow, f.tm, nil).list()
	if len(tomorrowList) != 1 || tomorrowList[0].(map[string]any)["bus"] == nil {
		t.Errorf("driver-only match (other route) must remain assigned: %v", tomorrowList)
	}
}

// TestBulkUnassignEmptyMatchingScope: a filter matching zero trips previews
// as count 0 with a null date span, and executing it returns an empty
// results list rather than an error.
func TestBulkUnassignEmptyMatchingScope(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	unused, _ := e.routeWithStops(f.admin, f.school, "RS-EM1", false, "Y")

	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"route_id": unused})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(0) || preview.data()["from"] != nil || preview.data()["to"] != nil {
		t.Fatalf("empty-scope preview = %v, want count 0, from/to null", preview.data())
	}
	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"route_id": unused})
	e.expect(r, 200, "")
	if len(r.data()["results"].([]any)) != 0 {
		t.Fatalf("results = %v, want empty", r.data()["results"])
	}
}

// TestBulkUnassignOverLimit: a scope matching more than the 500-trip cap is
// refused outright (400) with an actionable message, both at preview time
// (over_limit) and at confirm time — never silently truncated to a subset.
func TestBulkUnassignOverLimit(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	route, _ := e.routeWithStops(f.admin, f.school, "RS-OL1", false, "Y")

	// New trip creation only ever allows dates from today to 90 days ahead
	// (validateTripDate) — an absolute window, not one that slides per bulk
	// call — so stacking past 500 trips on ONE route within that window needs
	// several (bus, driver) pairs sharing it, each at its own non-overlapping
	// daily time slot (same pattern TestTripAssignmentRules proves: a shared
	// route with non-overlapping windows never conflicts). 3 pairs x 90 dates
	// x 2 trip types = 540 trips total, well over the 500 cap, using only 3
	// bulk-create calls instead of hundreds of individual ones.
	var dates []string
	for d := 1; d <= 90; d++ {
		dates = append(dates, e.dateIn(f.school, d))
	}
	windows := []struct{ morningStart, morningEnd, eveningStart, eveningEnd string }{
		{"06:00", "06:30", "14:00", "14:30"},
		{"07:00", "07:30", "15:00", "15:30"},
		{"08:00", "08:30", "16:00", "16:30"},
	}
	for i, w := range windows {
		busResp := e.do("POST", f.base+"/buses", f.tm, map[string]any{
			"vehicle_number": fmt.Sprintf("TN-OL-%d", i), "capacity": 40})
		e.expect(busResp, 201, "")
		busID := busResp.data()["id"].(string)
		driverResp := e.do("POST", f.base+"/drivers", f.tm, map[string]any{
			"name": fmt.Sprintf("Driver OL%d", i), "mobile": fmt.Sprintf("90000002%02d", i)})
		e.expect(driverResp, 201, "")
		driverID := driverResp.data()["id"].(string)
		bulk := e.do("POST", trips+"/bulk", f.tm, map[string]any{
			"dates": dates, "trip_types": []string{"morning_pickup", "evening_drop"},
			"route_id": route, "bus_id": busID, "driver_id": driverID,
			"morning_start_time": w.morningStart, "morning_end_time": w.morningEnd,
			"evening_start_time": w.eveningStart, "evening_end_time": w.eveningEnd,
		})
		e.expect(bulk, 200, "")
		if bulk.data()["created"] != float64(len(dates)*2) {
			t.Fatalf("pair %d: created = %v, want %d: %v", i, bulk.data()["created"], len(dates)*2, bulk.data())
		}
	}

	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"route_id": route})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(540) || preview.data()["over_limit"] != true {
		t.Fatalf("preview = %v, want count 540, over_limit true", preview.data())
	}

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"route_id": route})
	e.expect(r, 400, "validation_failed")
}

// TestBulkUnassignScopeReflectsLiveStateAtExecution: the real operation
// always re-resolves its filter against the live database at confirm time —
// it never replays a frozen list of IDs captured at preview time. A trip
// created after the preview, but before confirming, is still included if it
// matches the same filter: this is why a changed/added matching trip is
// never missed, and also why the frontend treats an old preview as stale and
// re-fetches it before trusting the count shown to the user.
func TestBulkUnassignScopeReflectsLiveStateAtExecution(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"route_id": f.route})
	e.expect(preview, 200, "")
	if preview.data()["count"] != float64(1) {
		t.Fatalf("preview = %v, want count 1", preview.data())
	}

	// A second matching trip appears after the preview, before confirming.
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.tomorrow, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")

	r := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"route_id": f.route})
	e.expect(r, 200, "")
	if len(r.data()["results"].([]any)) != 2 {
		t.Fatalf("results = %v, want 2 (execution re-resolves live, not a stale preview snapshot)", r.data()["results"])
	}
}
