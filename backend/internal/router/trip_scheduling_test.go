package router_test

// Coverage for "multiple scheduled trips per bus/driver/day" (Phase 8 of the
// feature request): a bus/driver can run several trips a day, on the same
// route or different ones, as long as their scheduled windows don't overlap.

import (
	"context"
	"sync"
	"testing"
)

// TestMultipleRoutesSameBusDriver: the user's own example — the same bus and
// driver run Route A, then (a different) Route A-1, then Route A-2 at three
// different times the same day, each keeping its own stops/riders untouched.
func TestMultipleRoutesSameBusDriver(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	routeA1, _ := e.routeWithStops(f.admin, f.school, "RS-A1", false, "North", "Central")
	routeA2, _ := e.routeWithStops(f.admin, f.school, "RS-A2", false, "East", "West")

	t1 := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15"))
	e.expect(t1, 201, "")
	t2 := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", routeA1, f.bus1, f.driver1, "08:30", "09:15"))
	e.expect(t2, 201, "")
	t3 := e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", routeA2, f.bus1, f.driver1, "09:30", "10:15"))
	e.expect(t3, 201, "")

	// Each route kept its own distinct identity — not silently merged or reused.
	ids := map[string]bool{
		t1.data()["route"].(map[string]any)["id"].(string): true,
		t2.data()["route"].(map[string]any)["id"].(string): true,
		t3.data()["route"].(map[string]any)["id"].(string): true,
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 distinct routes, got %v", ids)
	}

	// The driver's trip list for today shows all three, in scheduled order.
	driverTrips := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil).list()
	if len(driverTrips) != 3 {
		t.Fatalf("driver trips = %d, want 3: %v", len(driverTrips), driverTrips)
	}
	for i, want := range []string{f.route, routeA1, routeA2} {
		if got := driverTrips[i].(map[string]any)["route"].(map[string]any)["id"].(string); got != want {
			t.Errorf("driver trip %d route = %s, want %s (wrong chronological order?)", i, got, want)
		}
	}
}

// TestOverlapRejectedAcrossRoutes: an overlapping trip is rejected even when
// it's on a completely different route — the bus/driver genuinely can't be in
// two places at once, regardless of which routes are involved.
func TestOverlapRejectedAcrossRoutes(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-77", false, "Elsewhere")

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	// Different route, different trip type even, but the same bus, and the
	// window overlaps by 10 minutes.
	r := e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", other, f.bus1, f.driver2, "08:05", "09:00"))
	e.expect(r, 409, "trip_conflict")
	if r.Body["error"].(map[string]any)["fields"].(map[string]any)["bus_id"] == nil {
		t.Errorf("conflict should name bus_id: %v", r.Body)
	}
}

// TestBackToBackTripsAllowed: an end time exactly equal to the next trip's
// start time is accepted — the half-open interval boundary must not reject a
// legitimate back-to-back schedule.
func TestBackToBackTripsAllowed(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-88", false, "Nearby")

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", other, f.bus1, f.driver1, "08:15", "09:00")), 201, "")
}

// TestOvernightTripWindow: an end time at or before the start time is
// understood as the trip running past midnight, not rejected as backwards.
func TestOvernightTripWindow(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	r := e.do("POST", trips, f.tm, f.tripAt(f.today, "evening_drop", f.route, f.bus1, f.driver1, "23:30", "00:15"))
	e.expect(r, 201, "")
	if r.data()["scheduled_start_at"] == nil || r.data()["scheduled_end_at"] == nil {
		t.Fatalf("expected a resolved window: %v", r.data())
	}
}

// TestConcurrentOverlappingClaims: two requests for the same bus at
// genuinely overlapping times, fired together — exactly one must succeed,
// proving the exclusion constraint (not just the Go-level pre-check) is what
// actually prevents a double-booking race.
func TestConcurrentOverlappingClaims(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	routeA, _ := e.routeWithStops(f.admin, f.school, "RS-C1", false, "A")
	routeB, _ := e.routeWithStops(f.admin, f.school, "RS-C2", false, "B")

	var wg sync.WaitGroup
	results := make([]int, 2)
	bodies := []map[string]any{
		f.tripAt(f.today, "morning_pickup", routeA, f.bus2, f.driver2, "10:00", "10:45"),
		f.tripAt(f.today, "morning_pickup", routeB, f.bus2, f.driver2, "10:20", "11:00"), // overlaps the first
	}
	wg.Add(2)
	for i := range bodies {
		go func(i int) {
			defer wg.Done()
			results[i] = e.do("POST", trips, f.tm, bodies[i]).Status
		}(i)
	}
	wg.Wait()

	success, conflict := 0, 0
	for _, s := range results {
		switch s {
		case 201:
			success++
		case 409:
			conflict++
		default:
			t.Errorf("unexpected status %d", s)
		}
	}
	if success != 1 || conflict != 1 {
		t.Errorf("results = %v, want exactly one 201 and one 409", results)
	}

	var count int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM trips WHERE bus_id = $1 AND trip_date = $2::date AND status <> 'cancelled'`,
		f.bus2, f.today).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("bus2 has %d non-cancelled trips today, want exactly 1 (the race must not double-book it)", count)
	}
}

// TestLegacyNullScheduleTripsUnaffected: a trip with no scheduled window
// (created the same way every trip was before this feature) is never
// retroactively given an invented time, and keeps exactly its old protection:
// one such trip per route/bus/driver/date/type (there is no window to tell
// two of them apart, so a second one on the very same slot is still
// rejected, same as before this feature existed), while a genuinely
// different route/bus/driver on the same day works normally.
func TestLegacyNullScheduleTripsUnaffected(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-99", false, "Faraway")

	// No scheduled_start_time/end_time at all — exactly the pre-feature shape.
	noSchedule := e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1))
	e.expect(noSchedule, 201, "")
	if noSchedule.data()["scheduled_start_at"] != nil || noSchedule.data()["scheduled_end_at"] != nil {
		t.Errorf("a trip given no schedule must not have one invented: %v", noSchedule.data())
	}
	// A second trip on the very same bus/driver/route/day, also with no
	// schedule, IS rejected — a schedule-less trip has no window to compare,
	// so it keeps the original one-per-slot rule instead of the new overlap
	// check (this is exactly the regression TestDriverSelfAssignment and
	// TestConcurrentRouteClaims caught: driver route-slot claims never set a
	// schedule, and used to rely on this same exclusivity).
	e.expect(e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1)), 409, "trip_conflict")

	// A second schedule-less trip on a genuinely different route/bus/driver
	// the same day works exactly as it always did.
	third := e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", other, f.bus2, f.driver2))
	e.expect(third, 201, "")

	// Still visible and usable through the ordinary list/detail/driver paths.
	if n := len(e.do("GET", trips+"?date="+f.today, f.tm, nil).list()); n != 2 {
		t.Errorf("trips today = %d, want 2", n)
	}
	driverTrips := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil).list()
	if len(driverTrips) != 1 {
		t.Errorf("driver1 trips = %d, want 1", len(driverTrips))
	}
}

// TestScheduleLessCannotBypassScheduledConflict: the restored
// trips_{route,bus,driver}_slot_key indexes only cover schedule-less rows
// against OTHER schedule-less rows (a partial index can't see rows outside
// its own WHERE clause) — so a schedule-less request must still be checked
// against an existing *scheduled* trip at the application layer
// (TripSlotClash), or it could slip onto an already-booked bus/driver/route
// undetected by either guard.
func TestScheduleLessCannotBypassScheduledConflict(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"

	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")

	// A later request for the SAME route/bus/driver/slot but with no schedule
	// at all (e.g. a copy that intentionally carries a legacy trip's lack of
	// schedule forward) must still be rejected, not silently double-book it.
	bypass := e.do("POST", trips, f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1))
	e.expect(bypass, 409, "trip_conflict")
}

// TestBulkAssignSameBusDriverDifferentRouteNonOverlapping reproduces a reported
// bug report almost exactly: a bus/driver already runs one route (here f.route,
// 07:00-08:00, standing in for "RS-7G"), and the admin bulk-assigns a second,
// different route (standing in for "RS-7G-2") to the SAME bus/driver across
// several dates at a later, non-overlapping time. Every slot must be created,
// not skipped — bulk assignment must compare actual schedules, not just
// "does this bus/driver have any trip that date and type".
func TestBulkAssignSameBusDriverDifferentRouteNonOverlapping(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-7G-2", false, "Somewhere")

	// "RS-7G": bus1/driver1 already run this every one of the three dates below.
	for _, d := range []string{f.today, f.tomorrow, f.dayFrom} {
		e.expect(e.do("POST", trips, f.tm, f.tripAt(d, "morning_pickup", f.route, f.bus1, f.driver1, "07:00", "08:00")), 201, "")
	}

	// Bulk-assign "RS-7G-2" to the SAME bus1/driver1, same three dates, a
	// later and clearly non-overlapping window (11:30-12:30, as in the bug
	// report) — every slot must be created, none skipped.
	bulk := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": []string{f.today, f.tomorrow, f.dayFrom}, "trip_types": []string{"morning_pickup"},
		"route_id": other, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "11:30", "morning_end_time": "12:30",
	})
	e.expect(bulk, 200, "")
	if bulk.data()["created"] != float64(3) || len(bulk.data()["skipped"].([]any)) != 0 {
		t.Fatalf("bulk assignment of a different route to the same already-busy bus/driver at a non-overlapping "+
			"time should create all 3, skip none: %v", bulk.data())
	}

	// A second bulk request for the same route/bus/driver, same dates, but an
	// OVERLAPPING window (07:30-08:30, clashing with "RS-7G"'s 07:00-08:00 on
	// every one of those dates) must be skipped on every date — the opposite
	// outcome from a window that doesn't actually overlap.
	clash := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": []string{f.today, f.tomorrow, f.dayFrom}, "trip_types": []string{"morning_pickup"},
		"route_id": other, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "07:30", "morning_end_time": "08:30",
	})
	e.expect(clash, 200, "")
	if clash.data()["created"] != float64(0) || len(clash.data()["skipped"].([]any)) != 3 {
		t.Fatalf("an overlapping window on every date should create none, skip all 3: %v", clash.data())
	}
	for _, s := range clash.data()["skipped"].([]any) {
		reason := s.(map[string]any)["reason"].(string)
		if reason == "" {
			t.Errorf("skip reason must name what clashed: %v", s)
		}
	}
}

// TestBulkAssignMonthAheadNotBlockedByYesterdaysTrip: a bus/driver already ran
// a trip "yesterday" (today, in test terms); bulk-assigning a different route
// to the same bus/driver for an entire month starting tomorrow must not skip
// a single one of those dates — a trip's date is a hard boundary, not just a
// label, for both the EXCLUDE constraints (which compare real tstzrange
// instants) and the pre-check (scoped by trip_date). Reproduces, almost
// literally, a reported bug: "22 Oct already has a trip; bulk-assigning from
// 23 Oct for about a month reports 0 assigned, all skipped" — that turned out
// to be leftover schedule-less trips from an earlier, since-fixed attempt
// occupying those same future dates already, not a date-boundary leak; this
// test proves the boundary itself was never the problem.
func TestBulkAssignMonthAheadNotBlockedByYesterdaysTrip(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-MA1", false, "Somewhere")

	// "Yesterday" (today, in test terms): bus1/driver1 already run f.route.
	e.expect(e.do("POST", trips, f.tm, f.tripAt(f.today, "morning_pickup", f.route, f.bus1, f.driver1, "07:00", "08:00")), 201, "")

	var dates []string
	for d := 1; d <= 28; d++ {
		dates = append(dates, e.dateIn(f.school, d))
	}
	bulk := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": dates, "trip_types": []string{"morning_pickup", "evening_drop"},
		"route_id": other, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "11:30", "morning_end_time": "12:30",
		"evening_start_time": "16:00", "evening_end_time": "17:00",
	})
	e.expect(bulk, 200, "")
	if bulk.data()["created"] != float64(len(dates)*2) || len(bulk.data()["skipped"].([]any)) != 0 {
		t.Fatalf("a month of a different route on the same already-busy bus/driver, at non-overlapping times, "+
			"every date starting after the existing trip's date: want all %d created, none skipped: %v",
			len(dates)*2, bulk.data())
	}
}

// TestRouteScheduleExcludesStopsNotInEveningLeg: a stop that still carries a
// drop_time value but has been taken out of the Evening Drop leg (its
// drop_sequence is null — CLAUDE.md: "null if this stop is not part of the
// evening leg") must not widen the derived Evening Drop window. Verifies a
// domain detail the migration's one-time backfill glossed over (it took
// min/max drop_time across every stop regardless of drop_sequence) rather
// than assuming that was correct for GetRouteSchedule too.
func TestRouteScheduleExcludesStopsNotInEveningLeg(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	r := e.do("POST", f.base+"/routes", f.admin, map[string]any{"name": "RS-DS1", "code": "RS-DS1", "supports_pickup": true, "supports_drop": true})
	e.expect(r, 201, "")
	route := r.data()["id"].(string)
	// In the evening leg, drop_sequence 1 and 2.
	e.expect(e.do("POST", f.base+"/routes/"+route+"/stops", f.admin,
		map[string]any{"name": "A", "latitude": 11.01, "longitude": 76.96, "drop_time": "16:00"}), 201, "")
	e.expect(e.do("POST", f.base+"/routes/"+route+"/stops", f.admin,
		map[string]any{"name": "B", "latitude": 11.01, "longitude": 76.96, "drop_time": "16:30"}), 201, "")
	order := e.do("PUT", f.base+"/routes/"+route+"/stops/drop-order", f.admin, map[string]any{"drop_stop_ids": func() []string {
		detail := e.do("GET", f.base+"/routes/"+route, f.admin, nil)
		var ids []string
		for _, s := range detail.data()["stops"].([]any) {
			ids = append(ids, s.(map[string]any)["id"].(string))
		}
		return ids
	}()})
	e.expect(order, 200, "")
	// A third stop carries a drop_time far outside the real window, but is
	// NOT part of the evening leg (never included in the drop-order above),
	// so its drop_sequence stays null.
	e.expect(e.do("POST", f.base+"/routes/"+route+"/stops", f.admin,
		map[string]any{"name": "C (not on evening leg)", "latitude": 11.01, "longitude": 76.96, "drop_time": "23:45"}), 201, "")

	sched := e.do("GET", f.base+"/routes/"+route+"/schedule", f.admin, nil)
	e.expect(sched, 200, "")
	evening := sched.data()["evening_drop"].(map[string]any)
	if evening["start_time"] != "16:00" || evening["end_time"] != "16:45" {
		t.Errorf("evening = %v, want start 16:00 end 16:45 — stop C's 23:45 drop_time must not count (drop_sequence null)", evening)
	}
}

// TestBulkAssignExactReportedScenario reproduces, record for record, a
// reported "0 assigned, N skipped" bug: two routes already share a bus and
// driver across several dates (one with a genuinely overlapping schedule,
// several with legacy NULL schedules, one with an exact duplicate of the new
// request), and a bulk assignment to a second route is attempted across all
// of those dates plus one genuinely free one. Every skip must be a real,
// identifiable conflict — never a cross-date leak, mislabeled resource, or a
// stale date+type-only rule — and the one free date must succeed.
func TestBulkAssignExactReportedScenario(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	routeA, _ := e.routeWithStops(f.admin, f.school, "RS-SC-A", false, "A") // stands in for "RS7"
	routeB, _ := e.routeWithStops(f.admin, f.school, "RS-SC-B", false, "B") // stands in for "RS7-2"
	day1, day2, day3, day4 := f.today, f.tomorrow, f.dayFrom, e.dateIn(f.school, 3)

	// day1 ("22 Oct"): routeB already has a morning trip that will genuinely
	// overlap the new request (08:35-09:35 vs the new 08:30-08:55), and an
	// evening trip with no schedule at all (legacy).
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day1, "morning_pickup", routeB, f.bus1, f.driver1, "08:35", "09:35")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.trip(day1, "evening_drop", routeB, f.bus1, f.driver1)), 201, "")
	// day2 ("23 Oct"): routeA (a DIFFERENT route) has both types with no
	// schedule, same bus/driver — this is the cross-route "bus already has a
	// trip" case: a schedule-less trip has no window to compare, so it
	// blocks the bus for that exact date+type regardless of route.
	e.expect(e.do("POST", trips, f.tm, f.trip(day2, "morning_pickup", routeA, f.bus1, f.driver1)), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.trip(day2, "evening_drop", routeA, f.bus1, f.driver1)), 201, "")
	// day3 ("24 Oct"): routeB's morning trip is an EXACT duplicate of the new
	// request's window (identical-time route conflict); routeA's evening
	// trip genuinely overlaps the new request in real time, cross-route, by
	// bus (04:20-05:20 vs the new 04:30-05:45).
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day3, "morning_pickup", routeB, f.bus1, f.driver1, "08:30", "08:55")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day3, "evening_drop", routeA, f.bus1, f.driver1, "04:20", "05:20")), 201, "")
	// day4 ("25 Oct"): nothing at all for this bus/driver — genuinely free.

	bulk := e.do("POST", trips+"/bulk", f.tm, map[string]any{
		"dates": []string{day1, day2, day3, day4}, "trip_types": []string{"morning_pickup", "evening_drop"},
		"route_id": routeB, "bus_id": f.bus1, "driver_id": f.driver1,
		"morning_start_time": "08:30", "morning_end_time": "08:55",
		"evening_start_time": "04:30", "evening_end_time": "05:45",
	})
	e.expect(bulk, 200, "")
	if bulk.data()["created"] != float64(2) || len(bulk.data()["skipped"].([]any)) != 6 {
		t.Fatalf("want 2 created (day4 only) and 6 skipped (day1-3, both types): %v", bulk.data())
	}
	reason := map[string]string{}
	for _, s := range bulk.data()["skipped"].([]any) {
		m := s.(map[string]any)
		reason[m["trip_date"].(string)+" "+m["trip_type"].(string)] = m["reason"].(string)
	}
	want := map[string]string{
		day1 + " morning_pickup": "route", // genuine same-route overlap
		day1 + " evening_drop":   "route", // legacy NULL on the same route
		day2 + " morning_pickup": "bus",   // legacy NULL, different route, same bus
		day2 + " evening_drop":   "bus",
		day3 + " morning_pickup": "route", // identical-time duplicate, same route
		day3 + " evening_drop":   "bus",   // genuine overlap, different route, same bus
	}
	for k, prefix := range want {
		if got := reason[k]; got == "" {
			t.Errorf("%s: expected a skip reason, got none (bulk = %v)", k, bulk.data())
		} else if got[:len(prefix)] != prefix {
			t.Errorf("%s: reason = %q, want it to start with %q (wrong resource blamed)", k, got, prefix)
		}
	}
	// day4 must have been created, not skipped, for both types.
	created := e.do("GET", trips+"?date="+day4, f.tm, nil).list()
	if len(created) != 2 {
		t.Errorf("day4 (genuinely free) trips = %d, want 2", len(created))
	}
}

// TestSequentialSingleDayAssignmentsAcrossDates: a plain single-day
// assignment (not bulk) on one date, followed by a second plain single-day
// assignment on the very next date for the same bus and driver but a
// different route — both must succeed. Calendar adjacency alone is never a
// reason to conflict; only an actual overlapping instant is.
func TestSequentialSingleDayAssignmentsAcrossDates(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	trips := f.base + "/trips"
	other, _ := e.routeWithStops(f.admin, f.school, "RS-SEQ", false, "Elsewhere")

	day1 := f.today
	day2 := f.tomorrow
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day1, "morning_pickup", f.route, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
	e.expect(e.do("POST", trips, f.tm, f.tripAt(day2, "morning_pickup", other, f.bus1, f.driver1, "07:30", "08:15")), 201, "")
}
