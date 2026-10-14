package tracking

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecodePolyline(t *testing.T) {
	// The example from Google's polyline algorithm documentation.
	got, err := DecodePolyline("_p~iF~ps|U_ulLnnqC_mqNvxq`@")
	if err != nil {
		t.Fatal(err)
	}
	want := []LatLng{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if len(got) != len(want) {
		t.Fatalf("got %d points, want %d", len(got), len(want))
	}
	for i := range want {
		if math.Abs(got[i].Latitude-want[i].Latitude) > 1e-9 || math.Abs(got[i].Longitude-want[i].Longitude) > 1e-9 {
			t.Errorf("point %d = %v, want %v", i, got[i], want[i])
		}
	}
	for _, bad := range []string{"_p~iF", "_p~iF~ps|U_ulLnnqC_mqNvxq`", " "} {
		if _, err := DecodePolyline(bad); err == nil {
			t.Errorf("DecodePolyline(%q): want an error", bad)
		}
	}
}

func TestPathChunks(t *testing.T) {
	var requests []int // waypoints per request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Intermediates []any `json:"intermediates"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requests = append(requests, len(body.Intermediates)+2)
		if r.Header.Get("X-Goog-Api-Key") != "k" {
			t.Errorf("api key header missing")
		}
		_, _ = w.Write([]byte(`{"routes":[{"polyline":{"encodedPolyline":"_p~iF~ps|U_ulLnnqC"}}]}`))
	}))
	defer srv.Close()
	g := &GoogleRoutes{HTTP: srv.Client(), Endpoint: srv.URL}

	pts := make([]LatLng, 40)
	got, err := g.Path(context.Background(), "k", pts)
	if err != nil {
		t.Fatal(err)
	}
	// 40 points → 27 + 14 (sharing point 27), the shared point appears once.
	if len(requests) != 2 || requests[0] != 27 || requests[1] != 14 {
		t.Errorf("requests = %v, want [27 14]", requests)
	}
	if len(got) != 3 {
		t.Errorf("got %d points, want 3", len(got))
	}
}

func TestEncodePolylineRoundTrip(t *testing.T) {
	want := []LatLng{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if got := EncodePolyline(want); got != "_p~iF~ps|U_ulLnnqC_mqNvxq`@" {
		t.Errorf("EncodePolyline = %q", got)
	}
}

func TestSimplifyAndEncodeWithin(t *testing.T) {
	// A straight line north with 1 m of noise: everything between the ends goes.
	var pts []LatLng
	for i := 0; i <= 1000; i++ {
		pts = append(pts, LatLng{11 + float64(i)*0.0001, 77 + float64(i%2)*0.00001})
	}
	if s := Simplify(pts, 5); len(s) != 2 {
		t.Errorf("Simplify kept %d points, want 2", len(s))
	}
	// A real corner is kept.
	corner := []LatLng{{11, 77}, {11.01, 77}, {11.01, 77.01}}
	if s := Simplify(corner, 5); len(s) != 3 {
		t.Errorf("corner lost: %v", s)
	}
	// A zig-zag too long for a URL is shrunk to fit.
	var zig []LatLng
	for i := 0; i < 5000; i++ {
		zig = append(zig, LatLng{11 + float64(i)*0.001, 77 + float64(i%2)*0.01})
	}
	if enc := EncodeWithin(zig, 6000); len(enc) > 6000 {
		t.Errorf("encoded length %d > 6000", len(enc))
	}
}
