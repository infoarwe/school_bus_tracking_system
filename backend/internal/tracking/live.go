package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis layout (all per school, so nothing crosses tenants):
//
//	trip:{tripID}:loc          latest BusLocation of a trip (JSON), expires after 12 h
//	school:{schoolID}:live     hash tripID → BusLocation of every trip in progress
//	live:schools               set of schools with trips in progress (for the stale sweep)
//	school:{schoolID}:events   pub/sub channel for Event messages
const (
	locTTL         = 12 * time.Hour
	liveSchoolsKey = "live:schools"
	eventsPattern  = "school:*:events"
)

func tripKey(tripID string) string     { return "trip:" + tripID + ":loc" }
func progressKey(tripID string) string { return "trip:" + tripID + ":progress" }
func googleKey(tripID string) string   { return "trip:" + tripID + ":google_eta" }
func liveKey(schoolID string) string   { return "school:" + schoolID + ":live" }
func eventsKey(schoolID string) string { return "school:" + schoolID + ":events" }

// BusLocation is the current position of the bus on a trip, with enough trip
// details for a live map without another lookup.
type BusLocation struct {
	TripID     string    `json:"trip_id"`
	SchoolID   string    `json:"school_id"`
	TripType   string    `json:"trip_type"`
	RouteID    string    `json:"route_id"`
	RouteCode  string    `json:"route_code"`
	BusNumber  string    `json:"bus_number"`
	DriverName string    `json:"driver_name"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	AccuracyM  float64   `json:"accuracy_m"`
	SpeedMPS   *float64  `json:"speed_mps"`
	Heading    *float64  `json:"heading"`
	RecordedAt time.Time `json:"recorded_at"` // device time of the fix
	ReceivedAt time.Time `json:"received_at"` // server time: "last seen"
	Stale      bool      `json:"stale"`       // no GPS for longer than the school's limit
}

func (l *BusLocation) Point() Point {
	return Point{Latitude: l.Latitude, Longitude: l.Longitude, AccuracyM: l.AccuracyM,
		SpeedMPS: l.SpeedMPS, Heading: l.Heading, RecordedAt: l.RecordedAt}
}

// Event types sent to WebSocket subscribers.
const (
	EventLocation   = "location"
	EventTripStatus = "trip_status"
	EventBusStale   = "bus_stale"
	EventStopStatus = "stop_status" // a stop became approaching / reached / crossed
	EventProgress   = "progress"    // every stop's status and ETA, after each location
)

type Event struct {
	Type     string        `json:"type"`
	SchoolID string        `json:"school_id"`
	TripID   string        `json:"trip_id"`
	Location *BusLocation  `json:"location,omitempty"`
	Status   string        `json:"status,omitempty"` // trip_status
	Stop     *StopEvent    `json:"stop,omitempty"`   // stop_status
	Progress *TripProgress `json:"progress,omitempty"`
}

// Live reads and writes live positions in Redis and publishes events.
type Live struct{ rdb *redis.Client }

func NewLive(rdb *redis.Client) *Live { return &Live{rdb: rdb} }

// Last returns the trip's latest position, or nil if none yet.
func (l *Live) Last(ctx context.Context, tripID string) (*BusLocation, error) {
	raw, err := l.rdb.Get(ctx, tripKey(tripID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get location: %w", err)
	}
	var loc BusLocation
	if err := json.Unmarshal(raw, &loc); err != nil {
		return nil, err
	}
	return &loc, nil
}

// Update stores the new current position and notifies subscribers.
func (l *Live) Update(ctx context.Context, loc BusLocation) error {
	raw, err := json.Marshal(loc)
	if err != nil {
		return err
	}
	ev, err := json.Marshal(Event{Type: EventLocation, SchoolID: loc.SchoolID, TripID: loc.TripID, Location: &loc})
	if err != nil {
		return err
	}
	_, err = l.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, tripKey(loc.TripID), raw, locTTL)
		p.HSet(ctx, liveKey(loc.SchoolID), loc.TripID, raw)
		p.Expire(ctx, liveKey(loc.SchoolID), locTTL)
		p.SAdd(ctx, liveSchoolsKey, loc.SchoolID)
		p.Publish(ctx, eventsKey(loc.SchoolID), ev)
		return nil
	})
	if err != nil {
		return fmt.Errorf("redis update location: %w", err)
	}
	return nil
}

// TripStatusChanged tells subscribers about a status change. A finished trip
// is removed from the live map.
func (l *Live) TripStatusChanged(ctx context.Context, schoolID, tripID, status string) error {
	ev, err := json.Marshal(Event{Type: EventTripStatus, SchoolID: schoolID, TripID: tripID, Status: status})
	if err != nil {
		return err
	}
	_, err = l.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		if status == "completed" || status == "cancelled" {
			p.HDel(ctx, liveKey(schoolID), tripID)
			p.Del(ctx, tripKey(tripID), progressKey(tripID), googleKey(tripID))
		}
		p.Publish(ctx, eventsKey(schoolID), ev)
		return nil
	})
	if err != nil {
		return fmt.Errorf("redis trip status: %w", err)
	}
	return nil
}

// Snapshot returns every bus with a trip in progress at the school.
func (l *Live) Snapshot(ctx context.Context, schoolID string) ([]BusLocation, error) {
	all, err := l.rdb.HGetAll(ctx, liveKey(schoolID)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis snapshot: %w", err)
	}
	out := make([]BusLocation, 0, len(all))
	for _, raw := range all {
		var loc BusLocation
		if json.Unmarshal([]byte(raw), &loc) == nil {
			out = append(out, loc)
		}
	}
	return out, nil
}

// SweepStale marks buses with no GPS for longer than staleAfter(school) as stale
// and publishes a bus_stale event once per outage. Run periodically.
func (l *Live) SweepStale(ctx context.Context, now time.Time, staleAfter func(schoolID string) time.Duration) error {
	schools, err := l.rdb.SMembers(ctx, liveSchoolsKey).Result()
	if err != nil {
		return fmt.Errorf("redis live schools: %w", err)
	}
	for _, schoolID := range schools {
		buses, err := l.Snapshot(ctx, schoolID)
		if err != nil {
			return err
		}
		if len(buses) == 0 {
			l.rdb.SRem(ctx, liveSchoolsKey, schoolID)
			continue
		}
		limit := staleAfter(schoolID)
		for _, b := range buses {
			if b.Stale || now.Sub(b.ReceivedAt) < limit {
				continue
			}
			b.Stale = true
			raw, _ := json.Marshal(b)
			ev, _ := json.Marshal(Event{Type: EventBusStale, SchoolID: schoolID, TripID: b.TripID, Location: &b})
			if _, err := l.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
				p.Set(ctx, tripKey(b.TripID), raw, locTTL)
				p.HSet(ctx, liveKey(schoolID), b.TripID, raw)
				p.Publish(ctx, eventsKey(schoolID), ev)
				return nil
			}); err != nil {
				return fmt.Errorf("redis mark stale: %w", err)
			}
		}
	}
	return nil
}

// subscribeEvents listens to every school's events channel.
func (l *Live) subscribeEvents(ctx context.Context) *redis.PubSub {
	return l.rdb.PSubscribe(ctx, eventsPattern)
}

// schoolOfChannel extracts the school ID from "school:{id}:events".
func schoolOfChannel(ch string) string {
	return strings.TrimSuffix(strings.TrimPrefix(ch, "school:"), ":events")
}

// Progress returns the trip's stop-by-stop state, or nil before the first fix.
func (l *Live) Progress(ctx context.Context, tripID string) (*TripProgress, error) {
	var p TripProgress
	ok, err := l.getJSON(ctx, progressKey(tripID), &p)
	if !ok || err != nil {
		return nil, err
	}
	return &p, nil
}

// SaveProgress stores the state and publishes the stop events and the new
// progress to the school's subscribers.
func (l *Live) SaveProgress(ctx context.Context, schoolID string, p *TripProgress, events []StopEvent) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	msgs := make([][]byte, 0, len(events)+1)
	for i := range events {
		m, _ := json.Marshal(Event{Type: EventStopStatus, SchoolID: schoolID, TripID: p.TripID, Stop: &events[i]})
		msgs = append(msgs, m)
	}
	m, _ := json.Marshal(Event{Type: EventProgress, SchoolID: schoolID, TripID: p.TripID, Progress: p.View()})
	msgs = append(msgs, m)
	_, err = l.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, progressKey(p.TripID), raw, locTTL)
		for _, m := range msgs {
			pipe.Publish(ctx, eventsKey(schoolID), m)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("redis save progress: %w", err)
	}
	return nil
}

// GoogleETA returns the trip's cached Google Routes result, or nil.
func (l *Live) GoogleETA(ctx context.Context, tripID string) (*GoogleETA, error) {
	var g GoogleETA
	ok, err := l.getJSON(ctx, googleKey(tripID), &g)
	if !ok || err != nil {
		return nil, err
	}
	return &g, nil
}

func (l *Live) SaveGoogleETA(ctx context.Context, tripID string, g *GoogleETA) error {
	raw, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return l.rdb.Set(ctx, googleKey(tripID), raw, 10*time.Minute).Err()
}

// TryLock takes a short lock (e.g. one Google refresh per trip at a time, across
// API instances). It expires by itself.
func (l *Live) TryLock(ctx context.Context, name string, ttl time.Duration) (bool, error) {
	return l.rdb.SetNX(ctx, "lock:"+name, "1", ttl).Result()
}

// Unlock releases a TryLock lock early.
func (l *Live) Unlock(ctx context.Context, name string) {
	l.rdb.Del(ctx, "lock:"+name)
}

func (l *Live) getJSON(ctx context.Context, key string, dst any) (bool, error) {
	raw, err := l.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redis get %s: %w", key, err)
	}
	return true, json.Unmarshal(raw, dst)
}
