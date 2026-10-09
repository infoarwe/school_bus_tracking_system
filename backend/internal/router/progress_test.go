package router_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

// A compact route: stops 222 m apart going north, then the school.
func (e *env) compactRoute(f *tripFixture) (routeID string, stops []string) {
	e.t.Helper()
	r := e.do("POST", f.base+"/routes", f.admin, map[string]any{"name": "Compact", "code": "RS-09"})
	e.expect(r, 201, "")
	routeID = r.data()["id"].(string)
	for i, name := range []string{"Alpha", "Bravo", "Charlie"} {
		s := e.do("POST", f.base+"/routes/"+routeID+"/stops", f.admin, map[string]any{
			"name": name, "latitude": 11.002 + 0.002*float64(i), "longitude": 77.0, "geofence_radius_m": 50,
			"pickup_time": "07:1" + string(rune('0'+i)),
		})
		e.expect(s, 201, "")
		stops = append(stops, s.data()["id"].(string))
	}
	e.expect(e.do("PUT", f.base+"/settings/tracking", f.admin, map[string]any{
		"location_retention_days": 30, "stale_after_seconds": 120, "approach_distance_m": 100,
		"school_latitude": 11.008, "school_longitude": 77.0,
	}), 200, "")
	return routeID, stops
}

// drive posts fixes from latitude `from` to `to` (step ~28 m, one second apart), in batches.
func (e *env) drive(f *tripFixture, tripID string, from, to float64, start time.Time) time.Time {
	e.t.Helper()
	var batch []any
	at := start
	flush := func() {
		if len(batch) == 0 {
			return
		}
		r := e.do("POST", "/api/v1/driver/trips/"+tripID+"/locations", f.driver1Tok, map[string]any{"points": batch})
		e.expect(r, 200, "")
		if n := len(r.data()["rejected"].([]any)); n != 0 {
			e.t.Fatalf("points rejected: %v", r.data()["rejected"])
		}
		batch = nil
	}
	for lat := from; lat <= to+1e-9; lat += 0.00025 {
		at = at.Add(time.Second)
		batch = append(batch, map[string]any{"latitude": lat, "longitude": 77.0, "accuracy_m": 8, "speed_mps": 10,
			"recorded_at": at.UTC().Format(time.RFC3339Nano)})
		if len(batch) == 8 {
			flush()
		}
	}
	flush()
	return at
}

func TestStopDetectionEndToEnd(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	srv := httptest.NewServer(e.srv)
	defer srv.Close()

	route, stops := e.compactRoute(f)
	e.expect(e.do("PUT", f.base+"/students/"+f.student+"/assignment", f.admin,
		map[string]any{"route_id": route, "pickup_stop_id": stops[1], "drop_stop_id": stops[1]}), 200, "")
	e.expect(e.do("POST", f.base+"/parents", f.admin, map[string]any{
		"name": "Meena", "mobile": "9000000002", "children": []map[string]string{{"student_id": f.student}},
	}), 201, "")
	parentTok := e.otpLogin("9000000002", "parent")

	tripID := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", route, f.bus1, f.driver1)).data()["id"].(string)
	live := func() map[string]any {
		r := e.do("GET", "/api/v1/parent/children/"+f.student+"/live", parentTok, nil)
		e.expect(r, 200, "")
		return r.data()
	}
	// Before the trip starts: the trip is known, no bus yet.
	if l := live(); l["trip"].(map[string]any)["status"] != "scheduled" || l["bus"] != nil {
		t.Fatalf("before start: %v", l)
	}

	e.do("POST", "/api/v1/driver/trips/"+tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+tripID+"/start", f.driver1Tok, nil)
	admin, _, err := e.dialWS(srv, f.tm, "")
	if err != nil {
		t.Fatal(err)
	}
	admin.send(map[string]any{"type": "subscribe", "channel": "school"})
	admin.next("subscribed")

	start := time.Now()                              // ~35 one-second fixes: stays within the 2-minute clock-skew allowance
	at := e.drive(f, tripID, 11.000, 11.0015, start) // approaching Alpha

	// The parent sees the bus, and an ETA to their child's stop (Bravo).
	l := live()
	stop := l["child_stop"].(map[string]any)
	if l["bus"].(map[string]any)["state"] != "moving" || stop["name"] != "Bravo" || stop["status"] != "upcoming" || stop["eta_seconds"] == nil {
		t.Fatalf("parent live while approaching: %v", l)
	}
	if strings.Contains(stringify(l), "9000000001") {
		t.Error("parents must not get the driver's mobile")
	}
	if m := admin.next("stop_status"); m["stop"].(map[string]any)["type"] != "approaching" {
		t.Errorf("first stop event: %v", m)
	}

	at = e.drive(f, tripID, 11.00175, 11.0045, at) // through Alpha, into Bravo
	if l := live(); l["child_stop"].(map[string]any)["status"] != "reached" {
		t.Errorf("Bravo should be reached: %v", l["child_stop"])
	}
	e.drive(f, tripID, 11.00475, 11.0085, at) // past Bravo and Charlie, into the school

	// Stored events, in order, decided only by GPS (rule 8).
	prog := e.do("GET", f.base+"/trips/"+tripID+"/progress", f.tm, nil)
	e.expect(prog, 200, "")
	var seq []string
	for _, ev := range prog.data()["events"].([]any) {
		m := ev.(map[string]any)
		name := "school"
		if m["stop_name"] != nil {
			name = m["stop_name"].(string)
		}
		seq = append(seq, m["event_type"].(string)+":"+name)
	}
	want := "approaching:Alpha reached:Alpha crossed:Alpha approaching:Bravo reached:Bravo crossed:Bravo " +
		"approaching:Charlie reached:Charlie crossed:Charlie school_reached:school"
	if got := strings.Join(seq, " "); got != want {
		t.Errorf("events:\n got  %s\n want %s", got, want)
	}
	stopsView := prog.data()["progress"].(map[string]any)["stops"].([]any)
	if last := stopsView[len(stopsView)-1].(map[string]any); last["stop_id"] != "school" || last["status"] != "reached" {
		t.Errorf("school stop = %v", last)
	}
	if strings.Contains(stringify(prog.Body), `"inside"`) {
		t.Error("internal counters must not be sent")
	}

	// After the trip ends, the parent still sees how the child's stop went (from stored events).
	e.expect(e.do("POST", "/api/v1/driver/trips/"+tripID+"/end", f.driver1Tok, nil), 200, "")
	after := live()
	if after["trip"].(map[string]any)["status"] != "completed" || after["child_stop"].(map[string]any)["status"] != "crossed" || after["bus"] != nil {
		t.Errorf("after end: %v", after)
	}
	if e.do("GET", f.base+"/trips/"+tripID+"/progress", f.tm, nil).data()["progress"] != nil {
		t.Error("live progress should be cleared after the trip ends")
	}

	// Nobody else's child.
	other := e.do("POST", f.base+"/students", f.admin, map[string]any{"admission_no": "Z-9", "name": "Zed"}).data()["id"].(string)
	e.expect(e.do("GET", "/api/v1/parent/children/"+other+"/live", parentTok, nil), 404, "not_found")
}

// fakeETA stands in for Google Routes: 100 s to the first stop ahead, +100 s per stop.
type fakeETA struct {
	mu    sync.Mutex
	calls int
	key   string
}

func (f *fakeETA) ETAs(_ context.Context, key string, _ tracking.Point, pending []tracking.StopProgress, now time.Time) (*tracking.GoogleETA, error) {
	f.mu.Lock()
	f.calls++
	f.key = key
	f.mu.Unlock()
	g := &tracking.GoogleETA{ComputedAt: now}
	for i, s := range pending {
		g.StopIDs = append(g.StopIDs, s.StopID)
		g.CumSeconds = append(g.CumSeconds, 100*(i+1))
		g.CumMeters = append(g.CumMeters, 500*(i+1))
	}
	return g, nil
}

func (f *fakeETA) count() (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.key
}

func TestGoogleETAWithSchoolKey(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	route, stops := e.compactRoute(f)
	e.expect(e.do("PUT", f.base+"/students/"+f.student+"/assignment", f.admin,
		map[string]any{"route_id": route, "pickup_stop_id": stops[2], "drop_stop_id": stops[2]}), 200, "")
	tripID := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", route, f.bus1, f.driver1)).data()["id"].(string)
	e.do("POST", "/api/v1/driver/trips/"+tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+tripID+"/start", f.driver1Tok, nil)
	progress := func() map[string]any {
		return e.do("GET", f.base+"/trips/"+tripID+"/progress", f.tm, nil).data()["progress"].(map[string]any)
	}

	// No server key: Google is never called; the estimate is used.
	at := e.drive(f, tripID, 11.000, 11.0005, time.Now())
	time.Sleep(100 * time.Millisecond)
	if n, _ := e.eta.count(); n != 0 || progress()["eta_source"] != "estimate" {
		t.Fatalf("without a key: calls=%d source=%v", n, progress()["eta_source"])
	}

	// With the school's server key, Google is asked (in the background) and used on the next update.
	serverKey := "AIza" + strings.Repeat("K", 35)
	e.expect(e.do("PUT", f.base+"/settings/maps", f.admin, map[string]any{"browser_key": "", "server_key": serverKey}), 200, "")
	at = e.drive(f, tripID, 11.00075, 11.001, at)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if n, _ := e.eta.count(); n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n, key := e.eta.count(); n != 1 || key != serverKey {
		t.Fatalf("google calls=%d key ok=%v", n, key == serverKey)
	}
	e.drive(f, tripID, 11.00125, 11.0013, at)
	p := progress()
	first := p["stops"].([]any)[0].(map[string]any)
	if p["eta_source"] != "google" || first["eta_seconds"].(float64) > 100 {
		t.Errorf("google eta not used: source=%v first=%v", p["eta_source"], first)
	}
}
