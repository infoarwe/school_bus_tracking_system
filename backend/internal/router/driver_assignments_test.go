package router_test

import (
	"context"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

func (f *tripFixture) assignment(routeID, tripType, driverID, busID string) map[string]any {
	return map[string]any{"route_id": routeID, "trip_type": tripType, "driver_id": driverID, "bus_id": busID}
}

// tripCountForRoute counts non-cancelled trips rows for a route (used to assert
// materialization creates exactly one row, never a duplicate).
func tripCountForRoute(e *env, routeID string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM trips WHERE route_id = $1 AND status <> 'cancelled'`, routeID).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestDriverRouteAssignments(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	base := f.base + "/driver-assignments"

	// 1. Admin creates Morning + Evening assignment for one driver+bus; 4. same driver+bus
	// for both trip types succeeds (no false conflict - type differs in all three indexes).
	morning := e.do("POST", base, f.admin, f.assignment(f.route, models.TripMorningPickup, f.driver1, f.bus1))
	e.expect(morning, 201, "")
	morningID := morning.data()["id"].(string)
	evening := e.do("POST", base, f.admin, f.assignment(f.route, models.TripEveningDrop, f.driver1, f.bus1))
	e.expect(evening, 201, "")

	// Listing shows both current assignments for this driver.
	list := e.do("GET", base+"?driver_id="+f.driver1, f.admin, nil)
	e.expect(list, 200, "")
	if n := len(list.Body["data"].([]any)); n != 2 {
		t.Fatalf("driver1 assignments = %d, want 2: %v", n, list.Body)
	}

	// A conflicting assignment (same route+type, different driver/bus) is rejected.
	e.expect(e.do("POST", base, f.admin, f.assignment(f.route, models.TripMorningPickup, f.driver2, f.bus2)), 409, "assignment_conflict")

	// 2. Persists and materializes fresh per day: calling EnsureDriverDailyTrips for two
	// different dates produces a trip per assignment per date (driver1 has 2 current
	// assignments on this route - Morning and Evening - so 2 dates × 2 types = 4 rows); the
	// assignment rows themselves are untouched.
	ctx := context.Background()
	if err := store.EnsureDriverDailyTrips(ctx, e.pool, f.school, f.driver1, f.today); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDriverDailyTrips(ctx, e.pool, f.school, f.driver1, f.tomorrow); err != nil {
		t.Fatal(err)
	}
	if n := tripCountForRoute(e, f.route); n != 4 {
		t.Fatalf("trips after materializing 2 distinct dates = %d, want 4 (2 assignments x 2 dates)", n)
	}
	afterList := e.do("GET", base+"?driver_id="+f.driver1, f.admin, nil)
	e.expect(afterList, 200, "")
	if n := len(afterList.Body["data"].([]any)); n != 2 {
		t.Fatalf("assignments after materializing = %d, want 2 (materializing must not touch assignments)", n)
	}

	// 3. Driver A's trips never include driver B's. 7/8. Fetching today's trips (which also
	// triggers materialization for driver1, idempotently) returns exactly the Morning and
	// Evening trips, not duplicated on a repeat call, and an unstarted Morning trip stays
	// scheduled.
	today1 := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil)
	e.expect(today1, 200, "")
	rows1 := today1.Body["data"].([]any)
	if len(rows1) != 2 {
		t.Fatalf("driver1's today trips = %d, want 2 (Morning + Evening): %v", len(rows1), today1.Body)
	}
	var morningTripID string
	for _, raw := range rows1 {
		row := raw.(map[string]any)
		if row["trip_type"] == models.TripMorningPickup {
			morningTripID = row["id"].(string)
			if row["status"] != models.TripScheduled {
				t.Fatalf("morning trip status = %v, want scheduled", row["status"])
			}
		}
	}
	if morningTripID == "" {
		t.Fatal("no morning trip in driver1's today list")
	}

	today2 := e.do("GET", "/api/v1/driver/trips", f.driver2Tok, nil)
	e.expect(today2, 200, "")
	for _, raw := range today2.Body["data"].([]any) {
		if raw.(map[string]any)["id"] == morningTripID {
			t.Fatal("driver2 sees driver1's materialized trip")
		}
	}

	// 8. A second fetch for driver1 does not duplicate the trip (same id, same count).
	today1Again := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil)
	e.expect(today1Again, 200, "")
	if len(today1Again.Body["data"].([]any)) != 2 {
		t.Fatalf("second fetch trip count = %d, want 2 (no duplicates)", len(today1Again.Body["data"].([]any)))
	}
	if n := tripCountForRoute(e, f.route); n != 4 { // unchanged from the direct-date test above: today's slots were already filled
		t.Fatalf("trip rows for route after repeated GET /driver/trips = %d, want 4 (no duplicate row)", n)
	}

	// 5/6. Starting then completing the Morning trip leaves the Evening trip untouched.
	e.expect(e.do("POST", "/api/v1/driver/trips/"+morningTripID+"/confirm", f.driver1Tok, nil), 200, "")
	e.expect(e.do("POST", "/api/v1/driver/trips/"+morningTripID+"/start", f.driver1Tok, nil), 200, "")
	eveningStatusAfterStart := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil)
	for _, raw := range eveningStatusAfterStart.Body["data"].([]any) {
		row := raw.(map[string]any)
		if row["trip_type"] == models.TripEveningDrop && row["status"] != models.TripScheduled {
			t.Fatalf("evening trip status after starting morning = %v, want unchanged (scheduled)", row["status"])
		}
	}
	e.expect(e.do("POST", "/api/v1/driver/trips/"+morningTripID+"/end", f.driver1Tok, nil), 200, "")
	eveningStatusAfterEnd := e.do("GET", "/api/v1/driver/trips", f.driver1Tok, nil)
	for _, raw := range eveningStatusAfterEnd.Body["data"].([]any) {
		row := raw.(map[string]any)
		switch row["trip_type"] {
		case models.TripEveningDrop:
			if row["status"] != models.TripScheduled {
				t.Fatalf("evening trip status after completing morning = %v, want still scheduled (available)", row["status"])
			}
		case models.TripMorningPickup:
			if row["status"] != models.TripCompleted {
				t.Fatalf("morning trip status = %v, want completed", row["status"])
			}
		}
	}

	// 9. driver2 cannot operate driver1's trip.
	e.expect(e.do("POST", "/api/v1/driver/trips/"+morningTripID+"/confirm", f.driver2Tok, nil), 404, "not_found")

	// 10. Changing the permanent assignment (new bus) does not alter the already-materialized,
	// now-completed Morning trip.
	changed := e.do("PUT", base+"/"+morningID, f.admin, f.assignment(f.route, models.TripMorningPickup, f.driver1, f.bus2))
	e.expect(changed, 200, "")
	morningAfterChange := e.do("GET", f.base+"/trips/"+morningTripID, f.admin, nil)
	e.expect(morningAfterChange, 200, "")
	if morningAfterChange.data()["bus"].(map[string]any)["id"] != f.bus1 {
		t.Fatalf("historical morning trip's bus changed; want it to stay %s", f.bus1)
	}

	// 11. An exceptional one-off override of today's evening trip (admin swaps the bus for
	// just today) does not change the permanent assignment: materializing a FUTURE date still
	// uses the assignment's original bus.
	var eveningTripID string
	for _, raw := range today1Again.Body["data"].([]any) {
		row := raw.(map[string]any)
		if row["trip_type"] == models.TripEveningDrop {
			eveningTripID = row["id"].(string)
		}
	}
	override := e.do("PUT", f.base+"/trips/"+eveningTripID, f.admin,
		map[string]any{"trip_date": f.today, "trip_type": models.TripEveningDrop, "route_id": f.route, "bus_id": f.bus2, "driver_id": f.driver1})
	e.expect(override, 200, "")
	dayAfterTomorrow := e.dateIn(f.school, 3)
	if err := store.EnsureDriverDailyTrips(ctx, e.pool, f.school, f.driver1, dayAfterTomorrow); err != nil {
		t.Fatal(err)
	}
	var futureBus string
	if err := e.pool.QueryRow(ctx, `SELECT bus_id::text FROM trips WHERE route_id = $1 AND trip_date = $2::date AND trip_type = $3`,
		f.route, dayAfterTomorrow, models.TripEveningDrop).Scan(&futureBus); err != nil {
		t.Fatal(err)
	}
	if futureBus != f.bus1 {
		t.Fatalf("future evening trip's bus = %s, want the assignment's original bus %s (today's one-off override must not leak)", futureBus, f.bus1)
	}

	// Deactivating ends the assignment; it no longer appears in the current list.
	e.expect(e.do("POST", base+"/"+evening.data()["id"].(string)+"/deactivate", f.admin, nil), 204, "")
	afterDeactivate := e.do("GET", base+"?driver_id="+f.driver1, f.admin, nil)
	e.expect(afterDeactivate, 200, "")
	if n := len(afterDeactivate.Body["data"].([]any)); n != 1 {
		t.Fatalf("assignments after deactivating evening = %d, want 1 (morning only)", n)
	}
}
