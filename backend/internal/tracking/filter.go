// Package tracking handles live GPS: filtering raw points from the Driver app,
// the current bus position in Redis, live events, and the WebSocket hub.
package tracking

import (
	"math"
	"sort"
	"time"
)

const (
	MaxAccuracyM     = 100.0           // fixes less accurate than this are dropped
	MaxSpeedMPS      = 150.0 / 3.6     // 150 km/h: anything faster between fixes is a GPS jump
	MaxClockSkew     = 2 * time.Minute // device clock may be slightly ahead
	MaxPointAge      = 6 * time.Hour   // offline queues older than this are not useful
	MaxPointsPerCall = 100
)

// Point is one GPS fix from the Driver app.
type Point struct {
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	AccuracyM  float64   `json:"accuracy_m"`
	SpeedMPS   *float64  `json:"speed_mps,omitempty"`
	Heading    *float64  `json:"heading,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

// Rejection says why a point was dropped (returned to the app for debugging).
type Rejection struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// Filter keeps the plausible points, sorted by time. `last` is the previous
// accepted point of the trip (nil for the first batch); `tripStart` is when the
// trip started. Points are checked against each other in time order, so one bad
// fix in the middle of a batch does not poison the rest.
func Filter(points []Point, last *Point, tripStart, now time.Time) (kept []Point, rejected []Rejection) {
	idx := make([]int, len(points))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return points[idx[a]].RecordedAt.Before(points[idx[b]].RecordedAt) })

	prev := last
	for _, i := range idx {
		p := points[i]
		if reason := check(p, prev, tripStart, now); reason != "" {
			rejected = append(rejected, Rejection{Index: i, Reason: reason})
			continue
		}
		kept = append(kept, p)
		prev = &kept[len(kept)-1]
	}
	sort.Slice(rejected, func(a, b int) bool { return rejected[a].Index < rejected[b].Index })
	return kept, rejected
}

func check(p Point, prev *Point, tripStart, now time.Time) string {
	switch {
	case p.Latitude < -90 || p.Latitude > 90 || p.Longitude < -180 || p.Longitude > 180:
		return "coordinates out of range"
	case p.Latitude == 0 && p.Longitude == 0:
		return "no fix (0,0)"
	case p.AccuracyM <= 0 || p.AccuracyM > MaxAccuracyM:
		return "accuracy worse than 100 m"
	case p.RecordedAt.IsZero():
		return "recorded_at missing"
	case p.RecordedAt.After(now.Add(MaxClockSkew)):
		return "recorded_at is in the future"
	case p.RecordedAt.Before(now.Add(-MaxPointAge)):
		return "older than 6 hours"
	case p.RecordedAt.Before(tripStart.Add(-time.Minute)):
		return "recorded before the trip started"
	}
	if prev != nil {
		dt := p.RecordedAt.Sub(prev.RecordedAt).Seconds()
		if dt <= 0 {
			return "duplicate or out of order"
		}
		if DistanceM(prev.Latitude, prev.Longitude, p.Latitude, p.Longitude)/dt > MaxSpeedMPS {
			return "jump faster than 150 km/h"
		}
	}
	return ""
}

// DistanceM is the great-circle distance in metres (haversine).
func DistanceM(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(a))
}
