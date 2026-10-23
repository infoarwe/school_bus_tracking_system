package router_test

import (
	"fmt"
	"testing"
)

// Regression coverage for a reported "unassign ~350, reassign, unassign
// again -> ~450" trip-count growth. Root cause: when re-assigning (Bulk
// Assign/Copy/single create) used a schedule that did not overlap the
// existing unassigned trip's own (old) schedule, the system failed to
// recognize that trip as "the slot being filled in", silently left it
// orphaned (still unassigned, forever), and created a brand-new row next to
// it — one extra trip per mismatched reassignment, compounding every time
// the cycle repeated. Fixed in store.FindUnassignedSlot: when there is
// exactly one unassigned trip for a route+date+type, it is reused
// regardless of whether its own schedule happens to overlap the new one.

func countTrips(t *testing.T, e *env, schoolID string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), `SELECT count(*) FROM trips WHERE school_id = $1`, schoolID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countDuplicateSlots(t *testing.T, e *env, schoolID string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), `
		SELECT count(*) FROM (
			SELECT 1 FROM trips WHERE school_id = $1 AND status <> 'cancelled'
			GROUP BY route_id, trip_date, trip_type HAVING count(*) > 1
		) dup`, schoolID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestReassignWithChangedSchedule_NoDuplicateOrOrphan: the minimal
// reproduction. Unassigning a trip, then re-assigning it with a genuinely
// different (non-overlapping) schedule, must update that same trip — never
// leave the old row behind unassigned while creating a new one alongside it.
func TestReassignWithChangedSchedule_NoDuplicateOrOrphan(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	created := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(created, 201, "")
	tripID := created.data()["id"].(string)
	baseline := countTrips(t, e, f.school)

	e.expect(e.do("POST", trips+"/"+tripID+"/unassign", f.tm, nil), 200, "")
	if n := countTrips(t, e, f.school); n != baseline {
		t.Fatalf("unassign changed trip count: %d -> %d", baseline, n)
	}

	// Reassign with a genuinely disjoint schedule — the school changed its
	// pickup time, or the admin is correcting a mistake.
	reassigned := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "10:00", "10:30"))
	e.expect(reassigned, 201, "")
	if reassigned.data()["id"] != tripID {
		t.Fatalf("reassigning with a new schedule created a NEW trip (%v) instead of filling in %s — this is the orphan/duplicate bug", reassigned.data()["id"], tripID)
	}

	after := countTrips(t, e, f.school)
	if after != baseline {
		t.Fatalf("trip count grew from %d to %d after reassigning with a changed schedule — an orphaned duplicate was created", baseline, after)
	}
	if countDuplicateSlots(t, e, f.school) != 0 {
		t.Fatalf("a route/date/type slot has more than one trip row after reassigning")
	}
}

// TestReassignCycleAtScale_NoCountGrowth: the exact reported scenario at a
// representative scale — several routes, many dates, both trip types,
// unassign everything, reassign everything with a DIFFERENT schedule than
// originally used, then unassign everything again. The trip count, and the
// number of trips bulk-unassign resolves, must be identical at every step.
func TestReassignCycleAtScale_NoCountGrowth(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	ctx := t.Context()

	const nRoutes, nDates = 3, 15
	var dates []string
	for d := 1; d <= nDates; d++ {
		dates = append(dates, e.dateIn(f.school, d))
	}
	type setup struct{ routeID, busID, driverID string }
	setups := make([]setup, nRoutes)
	for i := 0; i < nRoutes; i++ {
		routeID, _ := e.routeWithStops(f.admin, f.school, fmt.Sprintf("RS-CG%d", i), false, "Y")
		bus := e.do("POST", f.base+"/buses", f.tm, map[string]any{"vehicle_number": fmt.Sprintf("TN-CG-%d", i), "capacity": 40})
		e.expect(bus, 201, "")
		driver := e.do("POST", f.base+"/drivers", f.tm, map[string]any{"name": fmt.Sprintf("Driver CG%d", i), "mobile": fmt.Sprintf("90000004%02d", i)})
		e.expect(driver, 201, "")
		setups[i] = setup{routeID, bus.data()["id"].(string), driver.data()["id"].(string)}
	}
	createBulk := func(s setup, morningStart, morningEnd, eveningStart, eveningEnd string) int {
		r := e.do("POST", trips+"/bulk", f.tm, map[string]any{
			"dates": dates, "trip_types": []string{"morning_pickup", "evening_drop"},
			"route_id": s.routeID, "bus_id": s.busID, "driver_id": s.driverID,
			"morning_start_time": morningStart, "morning_end_time": morningEnd,
			"evening_start_time": eveningStart, "evening_end_time": eveningEnd,
		})
		e.expect(r, 200, "")
		if len(r.data()["skipped"].([]any)) != 0 {
			t.Fatalf("setup: unexpected skips: %v", r.data())
		}
		return int(r.data()["created"].(float64))
	}

	total := 0
	for _, s := range setups {
		total += createBulk(s, "07:30", "08:15", "16:00", "17:00")
	}
	want := nRoutes * nDates * 2
	if total != want {
		t.Fatalf("setup created %d, want %d", total, want)
	}
	baseline := countTrips(t, e, f.school)

	unassign1 := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(unassign1, 200, "")
	n1 := len(unassign1.data()["results"].([]any))
	if n1 != want {
		t.Fatalf("first unassign resolved %d trips, want %d", n1, want)
	}
	if n := countTrips(t, e, f.school); n != baseline {
		t.Fatalf("first unassign changed trip count: %d -> %d", baseline, n)
	}

	// Reassign with a DIFFERENT, non-overlapping schedule than originally used.
	reassignedTotal := 0
	for _, s := range setups {
		reassignedTotal += createBulk(s, "10:00", "10:30", "20:00", "20:30")
	}
	if reassignedTotal != want {
		t.Fatalf("reassign created/filled %d, want %d (some were skipped or duplicated)", reassignedTotal, want)
	}
	if n := countTrips(t, e, f.school); n != baseline {
		t.Fatalf("CONFIRMED GROWTH BUG: trip count after reassigning with a changed schedule: %d, want %d (baseline)", n, baseline)
	}
	if countDuplicateSlots(t, e, f.school) != 0 {
		t.Fatalf("a route/date/type slot has more than one trip row after reassigning")
	}

	// Unassign again: must resolve exactly the same number as the first
	// time — never more.
	unassign2 := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(unassign2, 200, "")
	n2 := len(unassign2.data()["results"].([]any))
	if n2 != n1 {
		t.Fatalf("CONFIRMED GROWTH BUG: second unassign resolved %d trips, first resolved %d", n2, n1)
	}
	if n := countTrips(t, e, f.school); n != baseline {
		t.Fatalf("trip count after second unassign: %d, want %d (baseline)", n, baseline)
	}

	// The preview's count must agree exactly with the raw row count for the
	// same filter (UI and database must never disagree).
	var rawCount int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM trips WHERE school_id = $1 AND status <> 'cancelled'`, f.school).Scan(&rawCount); err != nil {
		t.Fatal(err)
	}
	preview := e.bulkUnassignPreview(f.base, f.tm, map[string]any{"all_dates": true})
	e.expect(preview, 200, "")
	if int(preview.data()["count"].(float64)) != rawCount {
		t.Fatalf("preview count (%v) does not match the raw database count (%d)", preview.data()["count"], rawCount)
	}
}

// TestRepeatedUnassignIsIdempotent_NoGrowth: unassigning the same set twice
// in a row (without any reassignment in between) must be a pure no-op the
// second time — never more rows, never a different result count.
func TestRepeatedUnassignIsIdempotent_NoGrowth(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	var dates []string
	for d := 1; d <= 10; d++ {
		dates = append(dates, e.dateIn(f.school, d))
	}
	r := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": dates, "trip_types": []string{"morning_pickup", "evening_drop"},
		"route_id": f.route, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "07:30", "morning_end_time": "08:15",
		"evening_start_time": "16:00", "evening_end_time": "17:00",
	})
	e.expect(r, 200, "")
	baseline := countTrips(t, e, f.school)

	for i := 0; i < 3; i++ {
		u := e.bulkUnassignFilter(f.base, f.tm, map[string]any{"route_id": f.route})
		e.expect(u, 200, "")
		results := u.data()["results"].([]any)
		if len(results) != 20 {
			t.Fatalf("round %d: results = %d, want 20", i, len(results))
		}
		for _, res := range results {
			if res.(map[string]any)["status"] != "unassigned" {
				t.Fatalf("round %d: expected every (already-unassigned) trip to still report success: %v", i, res)
			}
		}
		if n := countTrips(t, e, f.school); n != baseline {
			t.Fatalf("round %d: trip count changed: %d -> %d", i, baseline, n)
		}
	}
}
