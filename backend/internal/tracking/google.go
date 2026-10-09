package tracking

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GoogleRoutes computes ETAs with the Google Routes API (computeRoutes), using
// the school's own server key. One request returns a leg per stop, so one call
// gives the ETA to every stop ahead.
type GoogleRoutes struct {
	HTTP     *http.Client
	Endpoint string // overridable in tests
}

func NewGoogleRoutes() *GoogleRoutes {
	return &GoogleRoutes{
		HTTP:     &http.Client{Timeout: 5 * time.Second},
		Endpoint: "https://routes.googleapis.com/directions/v2:computeRoutes",
	}
}

// maxIntermediates is the Routes API limit for intermediate waypoints.
const maxIntermediates = 25

type latLng struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type waypoint struct {
	Location struct {
		LatLng latLng `json:"latLng"`
	} `json:"location"`
}

func wp(lat, lng float64) waypoint {
	var w waypoint
	w.Location.LatLng = latLng{lat, lng}
	return w
}

// ETAs returns the cumulative driving time and distance from the bus to each
// pending stop (at most 26; the rest are left to the estimate).
func (g *GoogleRoutes) ETAs(ctx context.Context, apiKey string, from Point, pending []StopProgress, now time.Time) (*GoogleETA, error) {
	if len(pending) == 0 {
		return nil, fmt.Errorf("no stops ahead")
	}
	if len(pending) > maxIntermediates+1 {
		pending = pending[:maxIntermediates+1]
	}
	body := map[string]any{
		"origin":            wp(from.Latitude, from.Longitude),
		"destination":       wp(pending[len(pending)-1].Latitude, pending[len(pending)-1].Longitude),
		"travelMode":        "DRIVE",
		"routingPreference": "TRAFFIC_AWARE",
	}
	if len(pending) > 1 {
		mids := make([]waypoint, len(pending)-1)
		for i, s := range pending[:len(pending)-1] {
			mids[i] = wp(s.Latitude, s.Longitude)
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
	req.Header.Set("X-Goog-FieldMask", "routes.legs.duration,routes.legs.distanceMeters")
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
			Legs []struct {
				Duration       string `json:"duration"` // e.g. "523s"
				DistanceMeters int    `json:"distanceMeters"`
			} `json:"legs"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("google routes: decode: %w", err)
	}
	if len(out.Routes) == 0 || len(out.Routes[0].Legs) != len(pending) {
		return nil, fmt.Errorf("google routes: no route")
	}
	eta := &GoogleETA{ComputedAt: now}
	var secs, meters int
	for i, leg := range out.Routes[0].Legs {
		d, err := strconv.ParseFloat(strings.TrimSuffix(leg.Duration, "s"), 64)
		if err != nil {
			return nil, fmt.Errorf("google routes: bad duration %q", leg.Duration)
		}
		// Each stop on the way costs a little dwell time too.
		secs += int(d)
		if i > 0 {
			secs += int(stopDwell.Seconds())
		}
		meters += leg.DistanceMeters
		eta.StopIDs = append(eta.StopIDs, pending[i].StopID)
		eta.CumSeconds = append(eta.CumSeconds, secs)
		eta.CumMeters = append(eta.CumMeters, meters)
	}
	return eta, nil
}
