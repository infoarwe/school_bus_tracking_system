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
)

// Google Routes is billed per call, so it is asked at most once per the school's
// interval per trip (Settings → Live tracking, google_eta_refresh_seconds; 0 = never).
// Between calls its answer is not replayed as a countdown: it calibrates the live
// estimate (Google time ÷ our estimate from the same spot = the traffic factor),
// which follows the bus on every fix. Passing a stop needs no new call.
const (
	minGoogleMaxAge    = 3 * time.Minute
	minTrafficFactor   = 0.5
	maxTrafficFactor   = 3.0
	minCalibrationSecs = 30 // too close to a stop for a meaningful ratio
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
	// Our estimate from the same position to the same stops (EstimateAlong), saved
	// with the answer: CumSeconds/EstSeconds is the traffic factor.
	EstSeconds []int `json:"est_seconds,omitempty"`
	EstMeters  []int `json:"est_meters,omitempty"`
}

// Calibrate stores our own estimate from `from` (at the trip's speed then) next to
// Google's answer, so later fixes can be scaled by Google's traffic factor.
func (g *GoogleETA) Calibrate(from Point, speedMPS float64, pending []StopProgress) {
	if len(pending) > len(g.StopIDs) {
		pending = pending[:len(g.StopIDs)]
	}
	g.EstSeconds, g.EstMeters = EstimateAlong(from.Latitude, from.Longitude, speedMPS, pending)
}

// EstimateAlong: cumulative seconds and metres from (lat, lng) through the stops in
// order, with the fallback model (straight line × road factor, clamped speed, dwell).
func EstimateAlong(lat, lng, speedMPS float64, stops []StopProgress) (secs, meters []int) {
	speed := speedMPS
	if speed <= 0 {
		speed = defaultSpeedMPS
	}
	speed = math.Min(math.Max(speed, minSpeedMPS), maxSpeedMPS)
	var m float64
	for i, s := range stops {
		m += DistanceM(lat, lng, s.Latitude, s.Longitude) * roadFactor
		lat, lng = s.Latitude, s.Longitude
		secs = append(secs, int(math.Round(m/speed+float64(i)*stopDwell.Seconds())))
		meters = append(meters, int(math.Round(m)))
	}
	return secs, meters
}

// googleOffset finds where the stops still ahead start in a Google answer: the
// answer's stops from that index on must be the first stops ahead (stops passed
// since are skipped). -1 if the answer does not fit (route changed, other order).
func googleOffset(g *GoogleETA, pending []StopProgress) int {
	for off := 0; off < len(g.StopIDs); off++ {
		rest := g.StopIDs[off:]
		if len(rest) > len(pending) {
			continue
		}
		ok := true
		for i, id := range rest {
			if pending[i].StopID != id {
				ok = false
				break
			}
		}
		if ok {
			return off
		}
	}
	return -1
}

// GoogleMaxAge: how long a Google answer is used: two intervals (one failed refresh),
// at least 3 minutes; 0 when Google ETA is off.
func GoogleMaxAge(every time.Duration) time.Duration {
	if every <= 0 {
		return 0
	}
	return max(2*every, minGoogleMaxAge)
}

func clampFactor(google, estimate int) float64 {
	if estimate < minCalibrationSecs {
		return 1
	}
	return math.Min(math.Max(float64(google)/float64(estimate), minTrafficFactor), maxTrafficFactor)
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
// from the bus's latest position (rule 9): the estimate, scaled by the traffic
// factor of a recent Google answer when there is one.
func (p *TripProgress) UpdateETAs(now time.Time, g *GoogleETA, maxAge time.Duration) {
	for i := range p.Stops {
		p.Stops[i].ETASeconds, p.Stops[i].DistanceM = nil, nil
	}
	pending := p.Pending()
	if p.LastPoint == nil || len(pending) == 0 {
		return
	}
	secs, meters := EstimateAlong(p.LastPoint.Latitude, p.LastPoint.Longitude, p.SpeedMPS, pending)
	p.ETASource = ETASourceEstimate
	off := -1
	if g != nil && now.Sub(g.ComputedAt) <= maxAge && len(g.EstSeconds) == len(g.StopIDs) {
		off = googleOffset(g, pending)
	}
	for i, s := range pending {
		eta, dist := float64(secs[i]), float64(meters[i])
		if j := off + i; off >= 0 && j < len(g.StopIDs) {
			eta *= clampFactor(g.CumSeconds[j], g.EstSeconds[j])
			dist *= clampFactor(g.CumMeters[j], g.EstMeters[j])
			p.ETASource = ETASourceGoogle
		}
		p.Stop(s.StopID).ETASeconds = ptr(int(math.Round(eta)))
		p.Stop(s.StopID).DistanceM = ptr(int(math.Round(dist)))
	}
}

// NeedsGoogleRefresh: Google ETA is on (every > 0) and there is no usable result,
// the stops ahead no longer fit it (route changed), or it is `every` old. Passing a
// stop is not a reason.
func (p *TripProgress) NeedsGoogleRefresh(now time.Time, g *GoogleETA, every time.Duration) bool {
	pending := p.Pending()
	if every <= 0 || len(pending) == 0 || p.LastPoint == nil {
		return false
	}
	return g == nil || len(g.EstSeconds) != len(g.StopIDs) || googleOffset(g, pending) < 0 ||
		now.Sub(g.ComputedAt) >= every
}
