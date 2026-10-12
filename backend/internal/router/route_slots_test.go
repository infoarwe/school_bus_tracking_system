package router_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

func claimBody(date, tripType, route, bus string) map[string]any {
	return map[string]any{"trip_date": date, "trip_type": tripType, "route_id": route, "bus_id": bus}
}

// staffNotifications counts inbox rows of the given type for a trip.
func (e *env) staffNotifications(typ, tripID string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM notification_recipients r
		JOIN notifications n ON n.id = r.notification_id JOIN users u ON u.id = r.user_id
		WHERE n.type = $1 AND n.trip_id = $2 AND u.role IN ('school_admin', 'transport_manager')`, typ, tripID).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestDriverSelfAssignment(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	ctx := context.Background()
	slots, claim := "/api/v1/driver/route-slots", "/api/v1/driver/route-slots/claim"
	// A parent of the route's student: must not be told about claims or releases.
	e.expect(e.do("POST", f.base+"/parents", f.admin, map[string]any{"name": "Meena", "mobile": "9000000009",
		"children": []map[string]string{{"student_id": f.student}}}), 201, "")

	// 1. Browse: eligible routes for the trip type, active buses only, stops with times.
	s := e.do("GET", slots, f.driver1Tok, nil)
	e.expect(s, 200, "")
	routes := s.data()["routes"].([]any)
	if len(routes) != 2 || s.data()["trip_type"] != "morning_pickup" || s.data()["my_trip"] != nil {
		t.Fatalf("morning slots = %v", s.data())
	}
	first := routes[0].(map[string]any)
	if first["availability"] != "available" || len(first["stops"].([]any)) != 3 || first["student_count"] != float64(1) {
		t.Errorf("RS-01 slot = %v", first)
	}
	if buses := s.data()["buses"].([]any); len(buses) != 2 { // the maintenance bus is not offered
		t.Errorf("buses = %v", buses)
	}
	ev := e.do("GET", slots+"?trip_type=evening_drop", f.driver1Tok, nil).data()
	if len(ev["routes"].([]any)) != 1 { // RS-02 runs Morning Pickup only
		t.Errorf("evening routes = %v", ev["routes"])
	}
	e.expect(e.do("GET", slots+"?date="+e.dateIn(f.school, 7), f.driver1Tok, nil), 400, "validation_failed")

	// 2. Claim: created confirmed, by the driver, audited, staff notified once.
	c := e.do("POST", claim, f.driver1Tok, claimBody(f.today, "morning_pickup", f.route, f.bus1))
	e.expect(c, 201, "")
	tripID := c.data()["id"].(string)
	if c.data()["status"] != "confirmed" || c.data()["assigned_by"] != "driver" || len(c.data()["stops"].([]any)) != 3 {
		t.Fatalf("claimed trip = %v", c.data())
	}
	if n := e.staffNotifications("route_claimed", tripID); n != 2 { // School Admin + Transport Manager
		t.Errorf("staff notified %d times, want 2", n)
	}
	var audits int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'trip.claim' AND entity_id = $1`, tripID).Scan(&audits)
	if audits != 1 {
		t.Errorf("trip.claim audit entries = %d", audits)
	}

	// 3. Retrying the same request returns the same trip and does not notify again.
	again := e.do("POST", claim, f.driver1Tok, claimBody(f.today, "morning_pickup", f.route, f.bus1))
	e.expect(again, 200, "")
	if again.data()["id"] != tripID || e.staffNotifications("route_claimed", tripID) != 2 {
		t.Errorf("retry created a new trip or notification: %v", again.data()["id"])
	}

	// 4. Conflicts: route taken, bus taken, one route per driver per slot.
	e.expect(e.do("POST", claim, f.driver2Tok, claimBody(f.today, "morning_pickup", f.route, f.bus2)), 409, "route_taken")
	e.expect(e.do("POST", claim, f.driver2Tok, claimBody(f.today, "morning_pickup", f.pickupOnlyRoute, f.bus1)), 409, "bus_taken")
	e.expect(e.do("POST", claim, f.driver1Tok, claimBody(f.today, "morning_pickup", f.pickupOnlyRoute, f.bus2)), 409, "already_assigned")
	var notifs int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE type = 'route_claimed'`).Scan(&notifs)
	if notifs != 1 {
		t.Errorf("failed claims must not notify: %d route_claimed notifications", notifs)
	}

	// 5. Availability as each driver sees it (never another driver's name).
	other := e.do("GET", slots, f.driver2Tok, nil).data()
	r1 := other["routes"].([]any)[0].(map[string]any)
	if r1["availability"] != "taken" || strings.Contains(fmt.Sprint(other), "Kumar") {
		t.Errorf("driver 2 view = %v", other)
	}
	for _, b := range other["buses"].([]any) {
		if bm := b.(map[string]any); bm["id"] == f.bus1 && bm["available"] != false {
			t.Errorf("bus 1 should be unavailable to driver 2: %v", bm)
		}
	}
	mine := e.do("GET", slots, f.driver1Tok, nil).data()
	if mine["routes"].([]any)[0].(map[string]any)["availability"] != "mine" || mine["my_trip"] == nil {
		t.Errorf("driver 1 view = %v", mine)
	}

	// 6. Trip types and dates are independent slots for the same permanent route.
	e.expect(e.do("POST", claim, f.driver2Tok, claimBody(f.today, "evening_drop", f.route, f.bus1)), 201, "")
	e.expect(e.do("POST", claim, f.driver2Tok, claimBody(f.tomorrow, "morning_pickup", f.route, f.bus2)), 201, "")

	// 7. Eligibility is checked by the server.
	for name, body := range map[string]map[string]any{
		"route without evening": claimBody(f.dayFrom, "evening_drop", f.pickupOnlyRoute, f.bus2),
		"bus in maintenance":    claimBody(f.dayFrom, "morning_pickup", f.route, f.busMaint),
		"8 days ahead":          claimBody(e.dateIn(f.school, 7), "morning_pickup", f.route, f.bus2),
		"in the past":           claimBody(e.dateIn(f.school, -1), "morning_pickup", f.route, f.bus2),
		"bad trip type":         claimBody(f.dayFrom, "noon", f.route, f.bus2),
	} {
		if r := e.do("POST", claim, f.driver1Tok, body); r.Status != 400 {
			t.Errorf("%s: got %d %v, want 400", name, r.Status, r.Body)
		}
	}
	// Another school's route and bus behave like unknown IDs.
	b := e.school("BBB")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	bAdmin := e.login("admin@b.test")
	bRoute, _ := e.routeWithStops(bAdmin, b, "B-01", false, "Elsewhere")
	foreign := e.do("POST", claim, f.driver1Tok, claimBody(f.dayFrom, "morning_pickup", bRoute, f.bus2))
	random := e.do("POST", claim, f.driver1Tok, claimBody(f.dayFrom, "morning_pickup", "00000000-0000-4000-8000-000000000001", f.bus2))
	if foreign.Status != 400 || fmt.Sprint(foreign.Body) != fmt.Sprint(random.Body) {
		t.Errorf("foreign route: %d %v vs random %v", foreign.Status, foreign.Body, random.Body)
	}
	e.expect(e.do("PATCH", f.base+"/routes/"+f.pickupOnlyRoute+"/status", f.admin, map[string]string{"status": "inactive"}), 200, "")
	if r := e.do("POST", claim, f.driver1Tok, claimBody(f.dayFrom, "morning_pickup", f.pickupOnlyRoute, f.bus2)); r.Status != 400 {
		t.Errorf("inactive route: %d", r.Status)
	}

	// 8. Release: cancelled with history, staff told, parents not; another driver can take it.
	rel := e.do("POST", "/api/v1/driver/trips/"+tripID+"/release", f.driver1Tok, map[string]string{"reason": "Unwell"})
	e.expect(rel, 200, "")
	if rel.data()["status"] != "cancelled" || !strings.Contains(rel.data()["cancel_reason"].(string), "Unwell") {
		t.Errorf("released trip = %v", rel.data())
	}
	if e.staffNotifications("route_released", tripID) != 2 {
		t.Error("staff not told about the release")
	}
	var parentNotes int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM notification_recipients r JOIN notifications n ON n.id = r.notification_id
		JOIN users u ON u.id = r.user_id WHERE u.role = 'parent' AND n.trip_id = $1`, tripID).Scan(&parentNotes)
	if parentNotes != 0 {
		t.Errorf("parents notified %d times about a claim/release", parentNotes)
	}
	e.expect(e.do("POST", "/api/v1/driver/trips/"+tripID+"/release", f.driver1Tok, nil), 409, "invalid_transition")
	taken := e.do("POST", claim, f.driver2Tok, claimBody(f.today, "morning_pickup", f.route, f.bus2))
	e.expect(taken, 201, "")
	hist := e.do("GET", f.base+"/trips/"+tripID, f.admin, nil).data()["history"].([]any)
	if len(hist) != 3 { // scheduled → confirmed → cancelled, all kept
		t.Errorf("history of the released trip = %v", hist)
	}

	// 9. Staff-assigned trips are not released by the driver.
	staffTrip := e.do("POST", f.base+"/trips", f.admin, f.trip(f.dayFrom, "morning_pickup", f.route, f.bus1, f.driver1))
	e.expect(staffTrip, 201, "")
	if staffTrip.data()["assigned_by"] != "staff" {
		t.Errorf("assigned_by = %v", staffTrip.data()["assigned_by"])
	}
	e.expect(e.do("POST", "/api/v1/driver/trips/"+staffTrip.data()["id"].(string)+"/release", f.driver1Tok, nil), 409, "assigned_by_school")
	// ...and another driver's trip is not visible at all.
	e.expect(e.do("POST", "/api/v1/driver/trips/"+taken.data()["id"].(string)+"/release", f.driver1Tok, nil), 404, "not_found")

	// 10. A claimed trip tracks like any trip: start, GPS, then it cannot be released.
	newID := taken.data()["id"].(string)
	e.expect(e.do("POST", "/api/v1/driver/trips/"+newID+"/start", f.driver2Tok, nil), 200, "")
	loc := e.do("POST", "/api/v1/driver/trips/"+newID+"/locations", f.driver2Tok, map[string]any{"points": []any{point(11.01, 76.96, 0)}})
	e.expect(loc, 200, "")
	if loc.data()["accepted"] != float64(1) {
		t.Errorf("GPS on a claimed trip: %v", loc.data())
	}
	e.expect(e.do("POST", "/api/v1/driver/trips/"+newID+"/release", f.driver2Tok, nil), 409, "invalid_transition")

	// 11. Staff inbox: own items, unread count, mark read; not for app users.
	inbox := e.do("GET", "/api/v1/staff/notifications", f.admin, nil)
	e.expect(inbox, 200, "")
	items := inbox.Body["data"].([]any)
	unread := inbox.Body["meta"].(map[string]any)["unread"].(float64)
	if len(items) < 4 || unread != float64(len(items)) {
		t.Fatalf("admin inbox: %d items, %v unread", len(items), unread)
	}
	firstItem := items[0].(map[string]any)
	if !strings.Contains(firstItem["title"].(string), "RS-01") || firstItem["data"].(map[string]any)["bus_number"] == "" {
		t.Errorf("inbox item = %v", firstItem)
	}
	e.expect(e.do("POST", "/api/v1/staff/notifications/read", f.admin, map[string]any{"ids": []string{firstItem["id"].(string)}}), 204, "")
	after := e.do("GET", "/api/v1/staff/notifications", f.admin, nil).Body["meta"].(map[string]any)["unread"].(float64)
	if after != unread-1 {
		t.Errorf("unread after marking one read: %v (was %v)", after, unread)
	}
	e.expect(e.do("GET", "/api/v1/staff/notifications", f.driver1Tok, nil), 403, "forbidden")
}

// Many drivers tapping "Take this route" at the same moment: exactly one wins.
func TestConcurrentRouteClaims(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	const n = 6
	tokens := make([]string, n)
	buses := make([]string, n)
	for i := range n {
		mobile := fmt.Sprintf("90000001%02d", i)
		e.expect(e.do("POST", f.base+"/drivers", f.tm, map[string]any{"name": fmt.Sprintf("Driver %d", i), "mobile": mobile}), 201, "")
		tokens[i] = e.otpLogin(mobile, "driver")
		r := e.do("POST", f.base+"/buses", f.tm, map[string]any{"vehicle_number": fmt.Sprintf("TN-38-ZZ-%04d", i), "capacity": 40})
		e.expect(r, 201, "")
		buses[i] = r.data()["id"].(string)
	}

	var wg sync.WaitGroup
	statuses := make([]int, n)
	codes := make([]string, n)
	for i := range n {
		wg.Go(func() {
			r := e.do("POST", "/api/v1/driver/route-slots/claim", tokens[i], claimBody(f.dayFrom, "morning_pickup", f.route, buses[i]))
			statuses[i], codes[i] = r.Status, r.errCode()
		})
	}
	wg.Wait()

	won := 0
	for i := range n {
		switch {
		case statuses[i] == 201:
			won++
		case statuses[i] != 409 || codes[i] != "route_taken":
			t.Errorf("claim %d: %d %s, want 201 or 409 route_taken", i, statuses[i], codes[i])
		}
	}
	var trips, notes int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM trips WHERE route_id = $1 AND trip_date = $2::date
		AND trip_type = 'morning_pickup' AND status <> 'cancelled'`, f.route, f.dayFrom).Scan(&trips)
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE type = 'route_claimed'`).Scan(&notes)
	if won != 1 || trips != 1 || notes != 1 {
		t.Errorf("winners %d, trips %d, notifications %d: want exactly 1 each", won, trips, notes)
	}
}
