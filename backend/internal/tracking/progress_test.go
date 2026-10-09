package tracking

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A straight route north from (11.000, 77.000): one stop every ~1.1 km
// (0.01° latitude), then the school.
func testRoute() []StopInput {
	var stops []StopInput
	for i, name := range []string{"A", "B", "C", "D"} {
		stops = append(stops, StopInput{ID: name, Sequence: i + 1, Name: name,
			Latitude: 11.0 + 0.01*float64(i+1), Longitude: 77.0, RadiusM: 100})
	}
	return append(stops, StopInput{ID: SchoolStopID, Name: "School", Latitude: 11.06, Longitude: 77.0, RadiusM: SchoolRadiusM})
}

type driver struct {
	t   *testing.T
	p   *TripProgress
	cfg GeofenceConfig
	at  time.Time
	all []StopEvent
}

func newDriver(t *testing.T) *driver {
	return &driver{t: t, p: NewProgress("trip", "morning_pickup", testRoute()),
		cfg: DefaultGeofence(500), at: time.Date(2026, 10, 7, 1, 30, 0, 0, time.UTC)}
}

// at sends one fix at latitude lat (longitude fixed) with the given accuracy.
func (d *driver) fix(lat, acc float64) []StopEvent {
	d.at = d.at.Add(10 * time.Second)
	ev := d.p.Apply(Point{Latitude: lat, Longitude: 77.0, AccuracyM: acc, RecordedAt: d.at}, d.cfg)
	d.all = append(d.all, ev...)
	return ev
}

func (d *driver) status(id string) string { return d.p.Stop(id).Status }

func eventList(evs []StopEvent) string {
	var s []string
	for _, e := range evs {
		x := e.Type + ":" + e.StopID
		if e.Missed {
			x += "(missed)"
		}
		s = append(s, x)
	}
	return strings.Join(s, " ")
}

func TestStopsInOrder(t *testing.T) {
	d := newDriver(t)
	d.fix(11.000, 10) // start, 1.1 km from A
	if d.status("A") != StopUpcoming {
		t.Fatal("A should still be upcoming")
	}
	d.fix(11.006, 10) // ~450 m from A
	if d.status("A") != StopApproaching {
		t.Fatalf("A = %s, want approaching", d.status("A"))
	}
	// One fix inside the circle is not enough.
	d.fix(11.0100, 10)
	if d.status("A") == StopReached {
		t.Fatal("one fix must not mark reached")
	}
	d.fix(11.0101, 10)
	if d.status("A") != StopReached {
		t.Fatalf("A = %s, want reached", d.status("A"))
	}
	// Leaving: needs 2 fixes beyond radius + buffer.
	d.fix(11.0120, 10) // ~220 m away
	d.fix(11.0125, 10)
	if d.status("A") != StopCrossed || d.p.Stop("A").Missed {
		t.Fatalf("A = %s missed=%v", d.status("A"), d.p.Stop("A").Missed)
	}
	for _, lat := range []float64{11.016, 11.020, 11.0201, 11.024, 11.026, 11.030, 11.0301, 11.034, 11.036, 11.040, 11.0401, 11.044, 11.050, 11.058, 11.0600, 11.0601} {
		d.fix(lat, 10)
	}
	got := eventList(d.all)
	want := "approaching:A reached:A crossed:A approaching:B reached:B crossed:B approaching:C reached:C crossed:C approaching:D reached:D crossed:D school_reached:school"
	if got != want {
		t.Errorf("events:\n got  %s\n want %s", got, want)
	}
	if d.p.current() != -1 {
		t.Error("trip should be complete after reaching school")
	}
	if ev := d.fix(11.0601, 10); len(ev) != 0 {
		t.Errorf("no events after school: %v", ev)
	}
}

func TestJitterAtEdgeIsNotReached(t *testing.T) {
	d := newDriver(t)
	// Hovering at the edge of A's circle: in, out, in, out... never two in a row.
	for i := 0; i < 10; i++ {
		lat := 11.0085 // ~165 m short: outside
		if i%2 == 0 {
			lat = 11.0093 // ~78 m: inside
		}
		d.fix(lat, 10)
	}
	if d.status("A") == StopReached {
		t.Fatal("GPS jitter must not produce reached")
	}
}

func TestInaccurateFixesIgnored(t *testing.T) {
	d := newDriver(t)
	d.fix(11.0100, 90) // 90 m is still precise enough for a 100 m circle: counts as 1 inside
	// 300 m fixes are ignored: they neither count nor reset the counter
	d.fix(11.0100, 300)
	d.fix(11.0100, 300)
	if d.status("A") == StopReached {
		t.Fatal("fixes with 300 m accuracy must not mark reached")
	}
	d.fix(11.0100, 20)
	if d.status("A") != StopReached {
		t.Fatalf("A = %s: a reliable fix after a reliable one should reach", d.status("A"))
	}
}

func TestSkippedStopIsMissed(t *testing.T) {
	d := newDriver(t)
	d.fix(11.000, 10)
	// The bus goes straight to B without entering A's circle (detour).
	d.p.Stops[0].Latitude, d.p.Stops[0].Longitude = 11.01, 77.01 // A is ~1 km east of the road
	d.fix(11.0200, 10)
	d.fix(11.0201, 10)
	if d.status("A") != StopCrossed || !d.p.Stop("A").Missed || d.status("B") != StopReached {
		t.Fatalf("A=%s missed=%v B=%s", d.status("A"), d.p.Stop("A").Missed, d.status("B"))
	}
	if got := eventList(d.all); !strings.Contains(got, "crossed:A(missed) reached:B") {
		t.Errorf("events = %s", got)
	}
}

func TestLookAheadIsLimited(t *testing.T) {
	d := newDriver(t)
	// A loop route: stop D sits next to the start. Being near D at the start must not skip A-C.
	d.p.Stops[3].Latitude = 11.000
	d.fix(11.000, 10)
	d.fix(11.0001, 10)
	if d.status("D") == StopReached || d.status("A") == StopCrossed {
		t.Fatalf("stops beyond the look-ahead must not be reached: A=%s D=%s", d.status("A"), d.status("D"))
	}
}

func TestETAEstimate(t *testing.T) {
	d := newDriver(t)
	speed := 8.0
	d.p.Apply(Point{Latitude: 11.000, Longitude: 77.0, AccuracyM: 10, SpeedMPS: &speed, RecordedAt: d.at}, d.cfg)
	now := d.at
	d.p.UpdateETAs(now, nil)
	if d.p.ETASource != ETASourceEstimate {
		t.Fatalf("source = %s", d.p.ETASource)
	}
	a, b := d.p.Stop("A"), d.p.Stop("B")
	// A is 1.11 km away × 1.3 road factor at 8 m/s ≈ 180 s.
	if *a.ETASeconds < 150 || *a.ETASeconds > 210 || *a.DistanceM < 1400 || *a.DistanceM > 1500 {
		t.Errorf("A eta=%d dist=%d", *a.ETASeconds, *a.DistanceM)
	}
	if *b.ETASeconds <= *a.ETASeconds+45 {
		t.Errorf("B should be later than A plus dwell: %d vs %d", *b.ETASeconds, *a.ETASeconds)
	}

	// A fresh Google result for the same stops wins, minus the time since it was computed.
	pending := d.p.Pending()
	g := &GoogleETA{ComputedAt: now.Add(-30 * time.Second)}
	for i, s := range pending {
		g.StopIDs = append(g.StopIDs, s.StopID)
		g.CumSeconds = append(g.CumSeconds, 300*(i+1))
		g.CumMeters = append(g.CumMeters, 2000*(i+1))
	}
	if d.p.NeedsGoogleRefresh(now, g) {
		t.Error("a 30 s old result for the same stops is fresh enough")
	}
	d.p.UpdateETAs(now, g)
	if d.p.ETASource != ETASourceGoogle || *d.p.Stop("A").ETASeconds != 270 {
		t.Errorf("google eta = %s %d", d.p.ETASource, *d.p.Stop("A").ETASeconds)
	}
	// Too old: back to the estimate.
	d.p.UpdateETAs(now.Add(5*time.Minute), g)
	if d.p.ETASource != ETASourceEstimate {
		t.Error("stale Google result must not be used")
	}

	// Reached stops have no ETA.
	d.fix(11.0100, 10)
	d.fix(11.0100, 10)
	d.p.UpdateETAs(d.at, nil)
	if d.p.Stop("A").ETASeconds != nil || d.p.Stop("B").ETASeconds == nil {
		t.Error("reached stop should have no ETA; next stop should")
	}
	if !d.p.NeedsGoogleRefresh(d.at, g) {
		t.Error("stops ahead changed: Google result must be refreshed")
	}
}

func TestGoogleRoutesClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Goog-Api-Key") != "AIzaTEST" || !strings.Contains(r.Header.Get("X-Goog-FieldMask"), "routes.legs.duration") {
			http.Error(w, "bad request", http.StatusForbidden)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if n := len(body["intermediates"].([]any)); n != 1 {
			t.Errorf("intermediates = %d, want 1", n)
		}
		_, _ = w.Write([]byte(`{"routes":[{"legs":[{"duration":"300s","distanceMeters":1500},{"duration":"240s","distanceMeters":1200}]}]}`))
	}))
	defer srv.Close()

	g := &GoogleRoutes{HTTP: srv.Client(), Endpoint: srv.URL}
	stops := NewProgress("t", "morning_pickup", testRoute()[:2]).Stops
	eta, err := g.ETAs(context.Background(), "AIzaTEST", Point{Latitude: 11, Longitude: 77}, stops, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Second stop: 300 + 240 driving + 45 s dwell at the first.
	if eta.CumSeconds[0] != 300 || eta.CumSeconds[1] != 585 || eta.CumMeters[1] != 2700 {
		t.Errorf("eta = %+v", eta)
	}
	if _, err := g.ETAs(context.Background(), "wrong", Point{Latitude: 11, Longitude: 77}, stops, time.Now()); err == nil {
		t.Error("a Google error must be returned so the caller falls back")
	}
}
