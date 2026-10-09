package tracking

import (
	"math"
	"time"
)

// Stop statuses (CLAUDE.md rule 8: computed from GPS + geofence, never set by hand).
const (
	StopUpcoming    = "upcoming"
	StopApproaching = "approaching"
	StopReached     = "reached"
	StopCrossed     = "crossed"
)

// Stop event types, stored in trip_stop_events and pushed live.
const (
	EventApproaching   = "approaching"
	EventReached       = "reached"
	EventCrossed       = "crossed"
	EventSchoolReached = "school_reached"
)

// SchoolStopID marks the school itself, the last "stop" of a Morning Pickup.
const SchoolStopID = "school"

// SchoolRadiusM is the arrival circle around the school.
const SchoolRadiusM = 150

// GeofenceConfig tunes stop detection.
type GeofenceConfig struct {
	ApproachM    float64 // "approaching" within this distance of the next stop
	Consecutive  int     // fixes in a row needed to decide reached / crossed
	CrossBufferM float64 // must be this far outside the circle to count as left
	LookAhead    int     // how many stops beyond the current one may be "reached" (skipped stops)
}

func DefaultGeofence(approachM float64) GeofenceConfig {
	return GeofenceConfig{ApproachM: approachM, Consecutive: 2, CrossBufferM: 30, LookAhead: 2}
}

// StopInput is a stop as the engine needs it.
type StopInput struct {
	ID            string
	Sequence      int // 0 for the school
	Name          string
	Latitude      float64
	Longitude     float64
	RadiusM       float64
	ScheduledTime *string // pickup time (morning) or drop time (evening)
}

// StopProgress is one stop's live status on a trip.
type StopProgress struct {
	StopID        string     `json:"stop_id"` // "school" for the school
	Sequence      int        `json:"sequence"`
	Name          string     `json:"name"`
	Latitude      float64    `json:"latitude"`
	Longitude     float64    `json:"longitude"`
	RadiusM       float64    `json:"radius_m"`
	ScheduledTime *string    `json:"scheduled_time"`
	Status        string     `json:"status"`
	Missed        bool       `json:"missed"` // crossed without being reached
	ReachedAt     *time.Time `json:"reached_at"`
	CrossedAt     *time.Time `json:"crossed_at"`
	ETASeconds    *int       `json:"eta_seconds"` // null once reached or crossed
	DistanceM     *int       `json:"distance_m"`

	// Hysteresis counters; kept in the stored state, cleared before sending to clients.
	Inside  int `json:"inside,omitempty"`
	Outside int `json:"outside,omitempty"`
}

// TripProgress is the stop-by-stop state of a trip in progress, in travel order.
type TripProgress struct {
	TripID    string         `json:"trip_id"`
	TripType  string         `json:"trip_type"`
	Stops     []StopProgress `json:"stops"`
	SpeedMPS  float64        `json:"speed_mps"` // smoothed recent speed
	ETASource string         `json:"eta_source"`
	UpdatedAt time.Time      `json:"updated_at"`
	LastPoint *Point         `json:"last_point,omitempty"`
}

// StopEvent is a status change decided by one GPS fix.
type StopEvent struct {
	StopID    string    `json:"stop_id"` // "school" for school_reached
	Type      string    `json:"type"`
	Missed    bool      `json:"missed"`
	At        time.Time `json:"at"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
}

// NewProgress starts a trip with every stop upcoming. Stops must already be in travel order.
func NewProgress(tripID, tripType string, stops []StopInput) *TripProgress {
	p := &TripProgress{TripID: tripID, TripType: tripType, Stops: make([]StopProgress, len(stops))}
	for i, s := range stops {
		p.Stops[i] = StopProgress{StopID: s.ID, Sequence: s.Sequence, Name: s.Name, Latitude: s.Latitude,
			Longitude: s.Longitude, RadiusM: s.RadiusM, ScheduledTime: s.ScheduledTime, Status: StopUpcoming}
	}
	return p
}

// current is the first stop not yet crossed, or -1 when all are done.
func (p *TripProgress) current() int {
	for i, s := range p.Stops {
		if s.Status != StopCrossed && !(s.StopID == SchoolStopID && s.Status == StopReached) {
			return i
		}
	}
	return -1
}

// Apply feeds one accepted GPS fix and returns the stop events it caused.
func (p *TripProgress) Apply(pt Point, cfg GeofenceConfig) []StopEvent {
	p.updateSpeed(pt)
	var events []StopEvent
	ev := func(s *StopProgress, typ string, missed bool) {
		events = append(events, StopEvent{StopID: s.StopID, Type: typ, Missed: missed, At: pt.RecordedAt,
			Latitude: pt.Latitude, Longitude: pt.Longitude})
	}
	// A fix less precise than the stop's circle cannot say whether the bus is
	// inside it; it must not create reached/crossed events (CLAUDE.md: no false
	// events on poor GPS).
	reliable := func(s *StopProgress) bool { return pt.AccuracyM <= math.Max(s.RadiusM, 50) }
	dist := func(s *StopProgress) float64 { return DistanceM(pt.Latitude, pt.Longitude, s.Latitude, s.Longitude) }

	cur := p.current()
	if cur < 0 {
		return nil
	}

	// 1. Leaving the stop the bus has reached.
	if s := &p.Stops[cur]; s.Status == StopReached && reliable(s) {
		if dist(s) > s.RadiusM+cfg.CrossBufferM {
			s.Outside++
			if s.Outside >= cfg.Consecutive {
				p.cross(s, pt.RecordedAt, false)
				ev(s, EventCrossed, false)
				cur = p.current()
			}
		} else {
			s.Outside = 0
		}
	}
	if cur < 0 {
		return events
	}

	// 2. Arriving: at the current stop, or a little further on (stops skipped).
	last := min(cur+cfg.LookAhead, len(p.Stops)-1)
	for j := cur; j <= last; j++ {
		s := &p.Stops[j]
		if s.Status == StopReached || !reliable(s) {
			continue
		}
		if dist(s) > s.RadiusM {
			s.Inside = 0
			continue
		}
		s.Inside++
		if s.Inside < cfg.Consecutive {
			continue
		}
		// Reached j: every earlier stop is now behind the bus.
		for k := cur; k < j; k++ {
			prev := &p.Stops[k]
			if prev.Status == StopCrossed {
				continue
			}
			missed := prev.Status != StopReached
			p.cross(prev, pt.RecordedAt, missed)
			ev(prev, EventCrossed, missed)
		}
		s.Status, s.ReachedAt, s.Inside = StopReached, ptr(pt.RecordedAt), 0
		if s.StopID == SchoolStopID {
			ev(s, EventSchoolReached, false)
		} else {
			ev(s, EventReached, false)
		}
		break
	}

	// 3. Approaching the next stop not yet reached.
	for i := range p.Stops {
		s := &p.Stops[i]
		if s.Status == StopCrossed || s.Status == StopReached {
			continue
		}
		if s.Status == StopUpcoming && dist(s) <= cfg.ApproachM && s.StopID != SchoolStopID {
			s.Status = StopApproaching
			ev(s, EventApproaching, false)
		}
		break
	}
	return events
}

func (p *TripProgress) cross(s *StopProgress, at time.Time, missed bool) {
	s.Status, s.CrossedAt, s.Missed, s.Inside, s.Outside = StopCrossed, ptr(at), missed, 0, 0
}

// updateSpeed keeps a smoothed speed for the ETA fallback.
func (p *TripProgress) updateSpeed(pt Point) {
	var v float64
	switch {
	case pt.SpeedMPS != nil:
		v = *pt.SpeedMPS
	case p.LastPoint != nil:
		if dt := pt.RecordedAt.Sub(p.LastPoint.RecordedAt).Seconds(); dt > 0 {
			v = DistanceM(p.LastPoint.Latitude, p.LastPoint.Longitude, pt.Latitude, pt.Longitude) / dt
		}
	}
	if p.SpeedMPS == 0 {
		p.SpeedMPS = v
	} else {
		p.SpeedMPS = 0.3*v + 0.7*p.SpeedMPS
	}
	last := pt
	p.LastPoint = &last
}

// View is the progress as sent to clients (without internal counters).
func (p *TripProgress) View() *TripProgress {
	v := *p
	v.LastPoint = nil
	v.Stops = make([]StopProgress, len(p.Stops))
	for i, s := range p.Stops {
		s.Inside, s.Outside = 0, 0
		v.Stops[i] = s
	}
	return &v
}

// Stop returns the progress of one stop, or nil.
func (p *TripProgress) Stop(id string) *StopProgress {
	for i := range p.Stops {
		if p.Stops[i].StopID == id {
			return &p.Stops[i]
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
