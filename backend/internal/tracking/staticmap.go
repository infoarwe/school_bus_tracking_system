package tracking

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// EncodePolyline encodes points in Google's polyline format (precision 5).
func EncodePolyline(pts []LatLng) string {
	var b strings.Builder
	var pLat, pLng int
	enc := func(v int) {
		u := v << 1
		if v < 0 {
			u = ^u
		}
		for u >= 0x20 {
			b.WriteByte(byte((0x20 | (u & 0x1f)) + 63))
			u >>= 5
		}
		b.WriteByte(byte(u + 63))
	}
	for _, p := range pts {
		lat := int(math.Round(p.Latitude * 1e5))
		lng := int(math.Round(p.Longitude * 1e5))
		enc(lat - pLat)
		enc(lng - pLng)
		pLat, pLng = lat, lng
	}
	return b.String()
}

// Simplify drops points that lie within tolM metres of the line through their
// neighbours (Douglas–Peucker), keeping the first and last point.
func Simplify(pts []LatLng, tolM float64) []LatLng {
	if len(pts) < 3 {
		return pts
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	type span struct{ a, b int }
	stack := []span{{0, len(pts) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		best, at := 0.0, -1
		for i := s.a + 1; i < s.b; i++ {
			if d := segmentDistanceM(pts[i], pts[s.a], pts[s.b]); d > best {
				best, at = d, i
			}
		}
		if at >= 0 && best > tolM {
			keep[at] = true
			stack = append(stack, span{s.a, at}, span{at, s.b})
		}
	}
	out := make([]LatLng, 0, len(pts))
	for i, k := range keep {
		if k {
			out = append(out, pts[i])
		}
	}
	return out
}

// segmentDistanceM: distance from p to segment ab, on a flat projection around p.
func segmentDistanceM(p, a, b LatLng) float64 {
	const mPerDeg = 111320.0
	kx := mPerDeg * math.Cos(p.Latitude*math.Pi/180)
	ax, ay := (a.Longitude-p.Longitude)*kx, (a.Latitude-p.Latitude)*mPerDeg
	bx, by := (b.Longitude-p.Longitude)*kx, (b.Latitude-p.Latitude)*mPerDeg
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l2 := dx*dx + dy*dy; l2 > 0 {
		t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l2))
	}
	return math.Hypot(ax+t*dx, ay+t*dy)
}

// EncodeWithin simplifies pts with growing tolerance until the encoded polyline
// is at most maxLen characters (Static Maps URLs are limited to 16 384).
func EncodeWithin(pts []LatLng, maxLen int) string {
	enc := EncodePolyline(pts)
	for tol := 5.0; len(enc) > maxLen && tol < 5000; tol *= 1.6 {
		enc = EncodePolyline(Simplify(pts, tol))
	}
	return enc
}

// MapMarker is one labelled pin on a static map.
type MapMarker struct {
	At    LatLng
	Color string // e.g. "0x1d4ed8" or "green"
	Label string // one character A-Z or 0-9; "" for none
	Size  string // "", "mid", "small" or "tiny"
}

// MapPath is one line on a static map (already encoded).
type MapPath struct {
	Encoded string
	Color   string // 0xRRGGBBAA
	Weight  int
}

// StaticMap renders route snapshot images with the Google Maps Static API.
type StaticMap struct {
	HTTP     *http.Client
	Endpoint string // overridable in tests
}

func NewStaticMap() *StaticMap {
	return &StaticMap{
		HTTP:     &http.Client{Timeout: 20 * time.Second},
		Endpoint: "https://maps.googleapis.com/maps/api/staticmap",
	}
}

// StaticMapURL builds the request (without the key) for a 1280×960 PNG that fits
// every path and marker.
func StaticMapURL(endpoint string, paths []MapPath, markers []MapMarker) string {
	q := url.Values{}
	q.Set("size", "640x480")
	q.Set("scale", "2")
	q.Set("format", "png")
	q.Set("maptype", "roadmap")
	for _, p := range paths {
		q.Add("path", fmt.Sprintf("color:%s|weight:%d|enc:%s", p.Color, p.Weight, p.Encoded))
	}
	for _, m := range markers {
		var parts []string
		if m.Size != "" {
			parts = append(parts, "size:"+m.Size)
		}
		parts = append(parts, "color:"+m.Color)
		if m.Label != "" {
			parts = append(parts, "label:"+m.Label)
		}
		parts = append(parts, strconv.FormatFloat(m.At.Latitude, 'f', 5, 64)+","+strconv.FormatFloat(m.At.Longitude, 'f', 5, 64))
		q.Add("markers", strings.Join(parts, "|"))
	}
	return endpoint + "?" + q.Encode()
}

// Render fetches the PNG.
func (s *StaticMap) Render(ctx context.Context, apiKey string, paths []MapPath, markers []MapMarker) ([]byte, error) {
	u := StaticMapURL(s.Endpoint, paths, markers) + "&key=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google static map: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("google static map: read: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google static map: %s: %s", res.Status, strings.TrimSpace(string(body[:min(len(body), 300)])))
	}
	if !bytes.HasPrefix(body, []byte("\x89PNG")) {
		return nil, fmt.Errorf("google static map: response is not a PNG")
	}
	return body, nil
}
