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

// Path stands in for the Google road path: the points it was given plus a midpoint
// after the first one, so the test can tell it from straight lines.
func (f *fakeETA) Path(_ context.Context, key string, pts []tracking.LatLng) ([]tracking.LatLng, error) {
	f.mu.Lock()
	f.calls++
	f.key = key
	f.mu.Unlock()
	mid := tracking.LatLng{Latitude: (pts[0].Latitude + pts[1].Latitude) / 2, Longitude: pts[0].Longitude + 0.0001}
	out := append([]tracking.LatLng{pts[0], mid}, pts[1:]...)
	return out, nil
}

func TestTripPlannedPath(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	route, _ := e.compactRoute(f)
	morning := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", route, f.bus1, f.driver1)).data()["id"].(string)
	path := func(tripID string) (string, []any) {
		r := e.do("GET", f.base+"/trips/"+tripID+"/planned-path", f.tm, nil)
		e.expect(r, 200, "")
		return r.data()["source"].(string), r.data()["points"].([]any)
	}

	// No server key: stop to stop, Alpha → Bravo → Charlie → School.
	src, pts := path(morning)
	if src != "straight" || len(pts) != 4 {
		t.Fatalf("without a key: %s %v", src, pts)
	}
	if last := pts[3].(map[string]any); last["latitude"] != 11.008 {
		t.Errorf("Morning Pickup should end at the school: %v", last)
	}

	// With the school's key: Google's path, then from the cache.
	e.expect(e.do("PUT", f.base+"/settings/maps", f.admin,
		map[string]any{"browser_key": "", "server_key": "AIza" + strings.Repeat("P", 35)}), 200, "")
	if src, pts := path(morning); src != "google" || len(pts) != 5 {
		t.Fatalf("with a key: %s %v", src, pts)
	}
	path(morning)
	if n, _ := e.eta.count(); n != 1 {
		t.Errorf("google calls = %d, want 1 (second request cached)", n)
	}

	// Evening Drop starts at the school.
	e.expect(e.do("PUT", f.base+"/settings/maps", f.admin, map[string]any{"browser_key": "", "server_key": ""}), 200, "")
	evening := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "evening_drop", route, f.bus1, f.driver1))
	e.expect(evening, 201, "")
	if _, pts := path(evening.data()["id"].(string)); len(pts) == 0 || pts[0].(map[string]any)["latitude"] != 11.008 {
		t.Errorf("Evening Drop should start at the school: %v", pts)
	}
}

// fakeStaticMap stands in for Google Static Maps: a tiny PNG, and the request it got.
type fakeStaticMap struct {
	mu    sync.Mutex
	calls int
	paths []tracking.MapPath
}

func (f *fakeStaticMap) Render(_ context.Context, _ string, paths []tracking.MapPath, _ []tracking.MapMarker) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.paths = paths
	return []byte("\x89PNG\r\n\x1a\nfake"), nil
}

func (f *fakeStaticMap) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRouteSnapshotOnTripEnd(t *testing.T) {
	e := newEnv(t)
	maps := &fakeStaticMap{}
	e.api.StaticMaps = maps
	f := newTripFixture(e)
	route, _ := e.compactRoute(f)
	e.expect(e.do("PUT", f.base+"/settings/maps", f.admin,
		map[string]any{"browser_key": "", "server_key": "AIza" + strings.Repeat("S", 35)}), 200, "")
	tripID := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", route, f.bus1, f.driver1)).data()["id"].(string)
	snapURL := f.base + "/trips/" + tripID + "/route-snapshot"

	// Only completed trips get one.
	e.expect(e.do("GET", snapURL, f.tm, nil), 404, "not_found")
	e.expect(e.do("POST", snapURL, f.tm, nil), 409, "trip_not_completed")

	e.do("POST", "/api/v1/driver/trips/"+tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+tripID+"/start", f.driver1Tok, nil)
	e.drive(f, tripID, 11.000, 11.0085, time.Now())
	e.expect(e.do("POST", "/api/v1/driver/trips/"+tripID+"/end", f.driver1Tok, nil), 200, "")

	// Made in the background after the trip ends.
	var snap map[string]any
	deadline := time.Now().Add(3 * time.Second)
	for {
		r := e.do("GET", snapURL, f.tm, nil)
		if r.Status == 200 {
			snap = r.data()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no snapshot: %v", r.Body)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if snap["status"] != "ready" || snap["has_image"] != true || snap["polyline"] == "" {
		t.Fatalf("snapshot = %v", snap)
	}
	if d := snap["distance_m"].(float64); d < 900 || d > 1000 { // 11.000 → 11.0085 ≈ 945 m
		t.Errorf("distance_m = %v", d)
	}
	if _, ok := snap["image_key"]; ok {
		t.Error("the storage key must not be exposed")
	}
	if len(maps.paths) != 2 {
		t.Errorf("want planned + travelled paths, got %d", len(maps.paths))
	}

	// The image is stored under school_{id}/trips/{trip}.png and served to staff.
	img := httptest.NewRecorder()
	req := httptest.NewRequest("GET", snapURL+"/image", nil)
	req.Header.Set("Authorization", "Bearer "+f.tm)
	e.srv.ServeHTTP(img, req)
	if img.Code != 200 || img.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(img.Body.String(), "\x89PNG") {
		t.Fatalf("image: %d %s", img.Code, img.Header().Get("Content-Type"))
	}
	if rc, err := e.api.Files.Open(context.Background(), "school_"+f.school+"/trips/"+tripID+".png"); err != nil {
		t.Errorf("stored file: %v", err)
	} else {
		rc.Close()
	}

	// Remaking it on request calls Google again.
	before := maps.count()
	e.expect(e.do("POST", snapURL, f.admin, nil), 200, "")
	if maps.count() != before+1 {
		t.Errorf("remake did not render")
	}
}

func TestGoogleETARefreshSetting(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	url := f.base + "/settings/tracking"
	get := func() float64 {
		r := e.do("GET", url, f.admin, nil)
		e.expect(r, 200, "")
		return r.data()["google_eta_refresh_seconds"].(float64)
	}
	if v := get(); v != 300 {
		t.Fatalf("default = %v, want 300", v)
	}
	base := map[string]any{"location_retention_days": 30, "stale_after_seconds": 120, "approach_distance_m": 1000}
	with := func(v any) map[string]any {
		m := map[string]any{"google_eta_refresh_seconds": v}
		for k, x := range base {
			m[k] = x
		}
		return m
	}
	// Omitted (an older web build): the value is kept, Google is not switched off.
	e.expect(e.do("PUT", url, f.admin, base), 200, "")
	if v := get(); v != 300 {
		t.Errorf("after save without the field = %v, want 300", v)
	}
	e.expect(e.do("PUT", url, f.admin, with(0)), 200, "")
	if v := get(); v != 0 {
		t.Errorf("off = %v", v)
	}
	e.expect(e.do("PUT", url, f.admin, with(7)), 200, "")
	for _, bad := range []int{4, 3601, -1} {
		r := e.do("PUT", url, f.admin, with(bad))
		e.expect(r, 400, "validation_failed")
	}
}
