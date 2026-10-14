package tracking

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// LatLng is one point of a drawn path.
type LatLng struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Path returns the driving path through pts in order (the planned road route of
// a trip), following the roads. Long trips are split into requests of at most
// maxIntermediates+2 points that share their end points.
func (g *GoogleRoutes) Path(ctx context.Context, apiKey string, pts []LatLng) ([]LatLng, error) {
	if len(pts) < 2 {
		return nil, fmt.Errorf("need at least two points")
	}
	const chunk = maxIntermediates + 2
	var out []LatLng
	for start := 0; start < len(pts)-1; start += chunk - 1 {
		end := min(start+chunk, len(pts))
		seg, err := g.pathSegment(ctx, apiKey, pts[start:end])
		if err != nil {
			return nil, err
		}
		if len(out) > 0 && len(seg) > 0 {
			seg = seg[1:] // the shared point is already there
		}
		out = append(out, seg...)
	}
	return out, nil
}

func (g *GoogleRoutes) pathSegment(ctx context.Context, apiKey string, pts []LatLng) ([]LatLng, error) {
	last := pts[len(pts)-1]
	body := map[string]any{
		"origin":            wp(pts[0].Latitude, pts[0].Longitude),
		"destination":       wp(last.Latitude, last.Longitude),
		"travelMode":        "DRIVE",
		"routingPreference": "TRAFFIC_UNAWARE",
	}
	if len(pts) > 2 {
		mids := make([]waypoint, len(pts)-2)
		for i, p := range pts[1 : len(pts)-1] {
			mids[i] = wp(p.Latitude, p.Longitude)
		}
		body["intermediates"] = mids
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", apiKey)
	req.Header.Set("X-Goog-FieldMask", "routes.polyline.encodedPolyline")
	res, err := g.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google routes: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("google routes: %s: %s", res.Status, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Routes []struct {
			Polyline struct {
				EncodedPolyline string `json:"encodedPolyline"`
			} `json:"polyline"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("google routes: decode: %w", err)
	}
	if len(out.Routes) == 0 || out.Routes[0].Polyline.EncodedPolyline == "" {
		return nil, fmt.Errorf("google routes: no route")
	}
	return DecodePolyline(out.Routes[0].Polyline.EncodedPolyline)
}

// DecodePolyline decodes Google's encoded polyline format (precision 5).
func DecodePolyline(s string) ([]LatLng, error) {
	var out []LatLng
	var lat, lng int
	for i := 0; i < len(s); {
		var deltas [2]int
		for k := range deltas {
			var result, shift int
			for {
				if i >= len(s) {
					return nil, fmt.Errorf("polyline: truncated")
				}
				b := int(s[i]) - 63
				i++
				if b < 0 || b > 63 {
					return nil, fmt.Errorf("polyline: bad character")
				}
				result |= (b & 0x1f) << shift
				shift += 5
				if b < 0x20 {
					break
				}
				if shift > 30 {
					return nil, fmt.Errorf("polyline: value too long")
				}
			}
			if result&1 != 0 {
				deltas[k] = ^(result >> 1)
			} else {
				deltas[k] = result >> 1
			}
		}
		lat += deltas[0]
		lng += deltas[1]
		out = append(out, LatLng{Latitude: float64(lat) / 1e5, Longitude: float64(lng) / 1e5})
	}
	return out, nil
}
