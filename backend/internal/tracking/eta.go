package tracking

import (
	"math"
	"time"
)

// Fallback ETA model, used when the school has no Google server key or Google
// fails: distance along the remaining stops × a road factor, at the bus's recent
// speed (clamped to city-bus values), plus a dwell time at each stop on the way.
const (
	roadFactor      = 1.3
	defaultSpeedMPS = 6.0  // ~22 km/h before the bus has moved
	minSpeedMPS     = 4.0  // ~15 km/h: traffic, short stops
	maxSpeedMPS     = 15.0 // ~54 km/h
	stopDwell       = 45 * time.Second
	googleMaxAge    = 3 * time.Minute // older Google results fall back to the estimate
)

const (
	ETASourceGoogle   = "google"
	ETASourceEstimate = "estimate"
)

// GoogleETA is a cached Google Routes result: cumulative duration and distance
// from the bus (at ComputedAt) to each of StopIDs, in travel order.
type GoogleETA struct {
	ComputedAt time.Time `json:"computed_at"`
	StopIDs    []string  `json:"stop_ids"`
	CumSeconds []int     `json:"cum_seconds"`
	CumMeters  []int     `json:"cum_meters"`
}

// Pending lists the stops still ahead of the bus (not reached or crossed), in order.
func (p *TripProgress) Pending() []StopProgress {
	var out []StopProgress
	for _, s := range p.Stops {
		if s.Status == StopUpcoming || s.Status == StopApproaching {
			out = append(out, s)
		}
	}
	return out
}

// UpdateETAs sets eta_seconds and distance_m for every stop ahead of the bus,
// from the bus's latest position (rule 9). A fresh Google result for the same
// stops is preferred; otherwise the estimate is used.
func (p *TripProgress) UpdateETAs(now time.Time, g *GoogleETA) {
	for i := range p.Stops {
		p.Stops[i].ETASeconds, p.Stops[i].DistanceM = nil, nil
	}
	pending := p.Pending()
	if p.LastPoint == nil || len(pending) == 0 {
		return
	}
	if g != nil && now.Sub(g.ComputedAt) <= googleMaxAge && sameStops(g.StopIDs, pending) {
		elapsed := int(now.Sub(g.ComputedAt).Seconds())
		for i, s := range pending {
			p.Stop(s.StopID).ETASeconds = ptr(max(0, g.CumSeconds[i]-elapsed))
			p.Stop(s.StopID).DistanceM = ptr(g.CumMeters[i])
		}
		p.ETASource = ETASourceGoogle
		return
	}

	speed := p.SpeedMPS
	if speed <= 0 {
		speed = defaultSpeedMPS
	}
	speed = math.Min(math.Max(speed, minSpeedMPS), maxSpeedMPS)
	var meters float64
	lat, lng := p.LastPoint.Latitude, p.LastPoint.Longitude
	for i, s := range pending {
		meters += DistanceM(lat, lng, s.Latitude, s.Longitude) * roadFactor
		lat, lng = s.Latitude, s.Longitude
		secs := meters/speed + float64(i)*stopDwell.Seconds()
		p.Stop(s.StopID).ETASeconds = ptr(int(math.Round(secs)))
		p.Stop(s.StopID).DistanceM = ptr(int(math.Round(meters)))
	}
	p.ETASource = ETASourceEstimate
}

// NeedsGoogleRefresh: no result yet, the stops ahead changed, or it is over a minute old.
func (p *TripProgress) NeedsGoogleRefresh(now time.Time, g *GoogleETA) bool {
	pending := p.Pending()
	if len(pending) == 0 || p.LastPoint == nil {
		return false
	}
	return g == nil || !sameStops(g.StopIDs, pending) || now.Sub(g.ComputedAt) > time.Minute
}

func sameStops(ids []string, stops []StopProgress) bool {
	if len(ids) != len(stops) {
		return false
	}
	for i, s := range stops {
		if ids[i] != s.StopID {
			return false
		}
	}
	return true
}
