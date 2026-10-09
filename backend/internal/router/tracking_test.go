package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

func point(lat, lng float64, ago time.Duration) map[string]any {
	return map[string]any{"latitude": lat, "longitude": lng, "accuracy_m": 8,
		"recorded_at": time.Now().Add(-ago).UTC().Format(time.RFC3339Nano)}
}

// startedTrip creates today's Morning Pickup for driver1 and starts it.
func (e *env) startedTrip(f *tripFixture) string {
	e.t.Helper()
	id := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1)).data()["id"].(string)
	e.expect(e.do("POST", "/api/v1/driver/trips/"+id+"/confirm", f.driver1Tok, nil), 200, "")
	e.expect(e.do("POST", "/api/v1/driver/trips/"+id+"/start", f.driver1Tok, nil), 200, "")
	return id
}

func TestLocationIngest(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	tripID := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1)).data()["id"].(string)
	url := "/api/v1/driver/trips/" + tripID + "/locations"
	batch := map[string]any{"points": []any{point(11.0168, 76.9663, 20*time.Second), point(11.0175, 76.9670, 10*time.Second)}}

	// GPS is accepted only while the trip is in progress (rule 7).
	e.expect(e.do("POST", url, f.driver1Tok, batch), 409, "trip_not_started")
	e.do("POST", "/api/v1/driver/trips/"+tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+tripID+"/start", f.driver1Tok, nil)
	// Another driver cannot post to this trip.
	e.expect(e.do("POST", url, f.driver2Tok, batch), 404, "not_found")
	e.expect(e.do("POST", url, f.driver1Tok, map[string]any{"points": []any{}}), 400, "validation_failed")

	bad := map[string]any{"latitude": 11.02, "longitude": 76.97, "accuracy_m": 900, "recorded_at": time.Now().UTC().Format(time.RFC3339)}
	r := e.do("POST", url, f.driver1Tok, map[string]any{"points": []any{point(11.0168, 76.9663, 20*time.Second), bad, point(11.0175, 76.9670, 10*time.Second)}})
	e.expect(r, 200, "")
	if r.data()["accepted"] != float64(2) || len(r.data()["rejected"].([]any)) != 1 {
		t.Fatalf("ingest = %v", r.data())
	}
	// Re-sending the same (offline-queued) points is harmless.
	if again := e.do("POST", url, f.driver1Tok, batch).data(); again["accepted"] != float64(0) {
		t.Errorf("duplicates accepted: %v", again)
	}

	snap := e.do("GET", f.base+"/live", f.tm, nil)
	if len(snap.list()) != 1 {
		t.Fatalf("live snapshot = %v", snap.list())
	}
	bus := snap.list()[0].(map[string]any)
	if bus["bus_number"] != "TN-38-AB-1234" || bus["latitude"] != 11.0175 || bus["route_code"] != "RS-01" {
		t.Errorf("live bus = %v", bus)
	}
	var stored int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM trip_locations WHERE trip_id = $1`, tripID).Scan(&stored)
	if stored != 2 {
		t.Errorf("history rows = %d, want 2", stored)
	}

	// Retention 0: live tracking still works, but no history is kept.
	e.expect(e.do("PUT", f.base+"/settings/tracking", f.admin, map[string]any{"location_retention_days": 0, "stale_after_seconds": 120, "approach_distance_m": 1000}), 200, "")
	e.expect(e.do("POST", url, f.driver1Tok, map[string]any{"points": []any{point(11.0180, 76.9675, time.Second)}}), 200, "")
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM trip_locations WHERE trip_id = $1`, tripID).Scan(&stored)
	if stored != 2 {
		t.Errorf("history grew with retention 0: %d", stored)
	}
	e.expect(e.do("PUT", f.base+"/settings/tracking", f.tm, map[string]any{"location_retention_days": 5, "stale_after_seconds": 120, "approach_distance_m": 1000}), 403, "forbidden")
	e.expect(e.do("PUT", f.base+"/settings/tracking", f.admin, map[string]any{"location_retention_days": 999, "stale_after_seconds": 5}), 400, "validation_failed")

	// Ending the trip removes the bus from the live map and stops ingest.
	e.expect(e.do("POST", "/api/v1/driver/trips/"+tripID+"/end", f.driver1Tok, nil), 200, "")
	if n := len(e.do("GET", f.base+"/live", f.tm, nil).list()); n != 0 {
		t.Errorf("ended trip still live: %d", n)
	}
	e.expect(e.do("POST", url, f.driver1Tok, batch), 409, "trip_not_started")
}

// wsConn is a test WebSocket client.
type wsConn struct {
	t *testing.T
	c *websocket.Conn
}

func (e *env) dialWS(srv *httptest.Server, token, extra string) (*wsConn, *http.Response, error) {
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws?access_token=" + token + extra
	c, res, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		return nil, res, err
	}
	e.t.Cleanup(func() { c.CloseNow() })
	return &wsConn{t: e.t, c: c}, res, nil
}

func (w *wsConn) send(msg map[string]any) {
	w.t.Helper()
	raw, _ := json.Marshal(msg)
	if err := w.c.Write(context.Background(), websocket.MessageText, raw); err != nil {
		w.t.Fatal(err)
	}
}

// next reads messages until one of the given type arrives (or fails after 3 s).
func (w *wsConn) next(msgType string) map[string]any {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, data, err := w.c.Read(ctx)
		if err != nil {
			w.t.Fatalf("waiting for %q: %v", msgType, err)
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m["type"] == msgType {
			return m
		}
	}
}

func TestLiveWebSocket(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	srv := httptest.NewServer(e.srv)
	defer srv.Close()

	tripID := e.startedTrip(f)
	// A second trip on a route the parent's child does not ride.
	otherTrip := e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", f.pickupOnlyRoute, f.bus2, f.driver2)).data()["id"].(string)
	e.expect(e.do("POST", f.base+"/parents", f.admin, map[string]any{
		"name": "Meena", "mobile": "9000000002", "children": []map[string]string{{"student_id": f.student}},
	}), 201, "")
	parentTok := e.otpLogin("9000000002", "parent")

	// Bad tokens are refused at the handshake.
	if _, res, err := e.dialWS(srv, "nope", ""); err == nil || res.StatusCode != 401 {
		t.Fatalf("bad token: err=%v status=%v", err, res)
	}

	admin, _, err := e.dialWS(srv, f.tm, "")
	if err != nil {
		t.Fatal(err)
	}
	admin.send(map[string]any{"type": "subscribe", "channel": "school"})
	admin.next("subscribed")

	parent, _, err := e.dialWS(srv, parentTok, "")
	if err != nil {
		t.Fatal(err)
	}
	// Parents cannot watch the whole school or another route's trip (rule 6).
	parent.send(map[string]any{"type": "subscribe", "channel": "school"})
	if m := parent.next("error"); m["code"] != "forbidden" {
		t.Errorf("parent school subscribe: %v", m)
	}
	parent.send(map[string]any{"type": "subscribe", "channel": "trip", "trip_id": otherTrip})
	if m := parent.next("error"); m["code"] != "not_found" {
		t.Errorf("parent other trip: %v", m)
	}
	parent.send(map[string]any{"type": "subscribe", "channel": "trip", "trip_id": tripID})
	if m := parent.next("subscribed"); m["status"] != "started" {
		t.Errorf("parent subscribed: %v", m)
	}

	// A location reaches both the admin (school channel) and the parent (trip channel).
	e.expect(e.do("POST", "/api/v1/driver/trips/"+tripID+"/locations", f.driver1Tok,
		map[string]any{"points": []any{point(11.0168, 76.9663, time.Second)}}), 200, "")
	for _, c := range []*wsConn{admin, parent} {
		m := c.next("location")
		if m["trip_id"] != tripID || m["location"].(map[string]any)["latitude"] != 11.0168 {
			t.Errorf("location event: %v", m)
		}
	}
	// The other trip's location reaches the admin but not the parent.
	e.do("POST", "/api/v1/driver/trips/"+otherTrip+"/confirm", f.driver2Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+otherTrip+"/start", f.driver2Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+otherTrip+"/locations", f.driver2Tok, map[string]any{"points": []any{point(11.0, 76.95, time.Second)}})
	admin.next("trip_status")
	if m := admin.next("location"); m["trip_id"] != otherTrip {
		t.Errorf("admin should see other trip: %v", m)
	}

	// Trip end is pushed live.
	e.expect(e.do("POST", "/api/v1/driver/trips/"+tripID+"/end", f.driver1Tok, nil), 200, "")
	if m := parent.next("trip_status"); m["status"] != "completed" || m["trip_id"] != tripID {
		t.Errorf("parent trip_status: %v", m)
	}

	// Staff of another school cannot watch this school's trips.
	b := e.school("BBB")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	other, _, err := e.dialWS(srv, e.login("admin@b.test"), "")
	if err != nil {
		t.Fatal(err)
	}
	other.send(map[string]any{"type": "subscribe", "channel": "trip", "trip_id": otherTrip})
	if m := other.next("error"); m["code"] != "not_found" {
		t.Errorf("cross-school subscribe: %v", m)
	}
	other.send(map[string]any{"type": "ping"})
	other.next("pong")
}

func TestStaleBus(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	srv := httptest.NewServer(e.srv)
	defer srv.Close()
	tripID := e.startedTrip(f)
	e.do("POST", "/api/v1/driver/trips/"+tripID+"/locations", f.driver1Tok, map[string]any{"points": []any{point(11.0168, 76.9663, time.Second)}})

	admin, _, err := e.dialWS(srv, f.tm, "")
	if err != nil {
		t.Fatal(err)
	}
	admin.send(map[string]any{"type": "subscribe", "channel": "school"})
	admin.next("subscribed")

	// Pretend 5 minutes passed with no GPS.
	if err := e.live.SweepStale(context.Background(), time.Now().Add(5*time.Minute), func(string) time.Duration { return 2 * time.Minute }); err != nil {
		t.Fatal(err)
	}
	if m := admin.next("bus_stale"); m["trip_id"] != tripID {
		t.Errorf("bus_stale: %v", m)
	}
	if bus := e.do("GET", f.base+"/live", f.tm, nil).list()[0].(map[string]any); bus["stale"] != true {
		t.Errorf("snapshot should be stale: %v", bus)
	}
	// Sweeping again does not repeat the event; a new fix clears it.
	_ = e.live.SweepStale(context.Background(), time.Now().Add(6*time.Minute), func(string) time.Duration { return 2 * time.Minute })
	e.do("POST", "/api/v1/driver/trips/"+tripID+"/locations", f.driver1Tok, map[string]any{"points": []any{point(11.0170, 76.9665, 0)}})
	if m := admin.next("location"); m["location"].(map[string]any)["stale"] != false {
		t.Errorf("a new fix should clear stale: %v", m)
	}
}
