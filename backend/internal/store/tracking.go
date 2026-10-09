package store

import (
	"context"
	"errors"
	"time"
)

// LocationRow is one stored GPS fix.
type LocationRow struct {
	Latitude, Longitude, AccuracyM float64
	SpeedMPS, Heading              *float64
	RecordedAt                     time.Time
}

// InsertLocations stores a trip's GPS history in one statement.
func InsertLocations(ctx context.Context, q DBTX, schoolID, tripID string, rows []LocationRow) error {
	if len(rows) == 0 {
		return nil
	}
	lat := make([]float64, len(rows))
	lng := make([]float64, len(rows))
	acc := make([]float64, len(rows))
	speed := make([]*float64, len(rows))
	heading := make([]*float64, len(rows))
	at := make([]time.Time, len(rows))
	for i, r := range rows {
		lat[i], lng[i], acc[i], speed[i], heading[i], at[i] = r.Latitude, r.Longitude, r.AccuracyM, r.SpeedMPS, r.Heading, r.RecordedAt
	}
	_, err := q.Exec(ctx, `
		INSERT INTO trip_locations (trip_id, school_id, latitude, longitude, accuracy_m, speed_mps, heading, recorded_at)
		SELECT $1, $2, u.lat, u.lng, u.acc, u.speed, u.heading, u.at
		FROM unnest($3::float8[], $4::float8[], $5::float8[], $6::float8[], $7::float8[], $8::timestamptz[])
			AS u(lat, lng, acc, speed, heading, at)`,
		tripID, schoolID, lat, lng, acc, speed, heading, at)
	return err
}

type TrackingSettings struct {
	LocationRetentionDays int      `json:"location_retention_days"`
	StaleAfterSeconds     int      `json:"stale_after_seconds"`
	ApproachDistanceM     int      `json:"approach_distance_m"`
	SchoolLatitude        *float64 `json:"school_latitude"`  // destination of Morning Pickup
	SchoolLongitude       *float64 `json:"school_longitude"` // (null: "School Reached" is not detected)
}

func GetTrackingSettings(ctx context.Context, q DBTX, schoolID string) (*TrackingSettings, error) {
	var s TrackingSettings
	err := q.QueryRow(ctx, `SELECT location_retention_days, stale_after_seconds, approach_distance_m, latitude, longitude
		FROM schools WHERE id = $1`, schoolID).
		Scan(&s.LocationRetentionDays, &s.StaleAfterSeconds, &s.ApproachDistanceM, &s.SchoolLatitude, &s.SchoolLongitude)
	if err != nil {
		return nil, mapErr(err)
	}
	return &s, nil
}

func SetTrackingSettings(ctx context.Context, q DBTX, schoolID string, s TrackingSettings) error {
	tag, err := q.Exec(ctx, `UPDATE schools SET location_retention_days = $2, stale_after_seconds = $3,
		approach_distance_m = $4, latitude = $5, longitude = $6 WHERE id = $1`,
		schoolID, s.LocationRetentionDays, s.StaleAfterSeconds, s.ApproachDistanceM, s.SchoolLatitude, s.SchoolLongitude)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// StaleLimits returns every school's stale threshold (for the stale sweep).
func StaleLimits(ctx context.Context, q DBTX) (map[string]time.Duration, error) {
	rows, err := q.Query(ctx, `SELECT id, stale_after_seconds FROM schools`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Duration{}
	for rows.Next() {
		var id string
		var secs int
		if err := rows.Scan(&id, &secs); err != nil {
			return nil, err
		}
		out[id] = time.Duration(secs) * time.Second
	}
	return out, rows.Err()
}

// DeleteExpiredLocations removes GPS history older than each school's retention,
// in batches so a big backlog does not lock the table. Returns rows deleted.
func DeleteExpiredLocations(ctx context.Context, q DBTX, batch int) (int64, error) {
	var total int64
	for {
		tag, err := q.Exec(ctx, `
			DELETE FROM trip_locations WHERE id IN (
				SELECT l.id FROM trip_locations l JOIN schools s ON s.id = l.school_id
				WHERE l.recorded_at < now() - make_interval(days => s.location_retention_days)
				LIMIT $1)`, batch)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < int64(batch) {
			return total, nil
		}
	}
}

// ParentCanSeeTrip reports whether a parent login has an active linked child who
// rides this trip: same route, and a stop for the trip's type. Returns the
// trip's school for the subscription.
func ParentCanSeeTrip(ctx context.Context, q DBTX, parentUserID, tripID string) (schoolID string, ok bool, err error) {
	err = q.QueryRow(ctx, `
		SELECT t.school_id FROM trips t
		JOIN student_assignments sa ON sa.route_id = t.route_id AND sa.ended_at IS NULL
			AND CASE WHEN t.trip_type = 'morning_pickup' THEN sa.pickup_stop_id ELSE sa.drop_stop_id END IS NOT NULL
		JOIN students s ON s.id = sa.student_id AND s.status = 'active'
		JOIN parent_students ps ON ps.student_id = s.id
		JOIN parents p ON p.id = ps.parent_id AND p.status = 'active'
		WHERE t.id = $1 AND p.user_id = $2
		LIMIT 1`, tripID, parentUserID).Scan(&schoolID)
	if err != nil {
		if errors.Is(mapErr(err), ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return schoolID, true, nil
}

// TripLocations returns the stored GPS history of a trip (for replay, Sprint 8).
func TripLocations(ctx context.Context, q DBTX, tripID string) ([]LocationRow, error) {
	rows, err := q.Query(ctx, `SELECT latitude, longitude, accuracy_m, speed_mps, heading, recorded_at
		FROM trip_locations WHERE trip_id = $1 ORDER BY recorded_at`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LocationRow{}
	for rows.Next() {
		var r LocationRow
		if err := rows.Scan(&r.Latitude, &r.Longitude, &r.AccuracyM, &r.SpeedMPS, &r.Heading, &r.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StopEventRow is one stored stop event (school_reached has no stop).
type StopEventRow struct {
	StopID     *string   `json:"stop_id"`
	StopName   *string   `json:"stop_name"`
	EventType  string    `json:"event_type"`
	Missed     bool      `json:"missed"`
	OccurredAt time.Time `json:"occurred_at"`
}

// InsertStopEvent stores an event once; a repeat (e.g. two requests racing) is ignored.
// stopID "" means the school.
func InsertStopEvent(ctx context.Context, q DBTX, schoolID, tripID, stopID, eventType string, missed bool,
	at time.Time, lat, lng float64) error {
	var sid *string
	if stopID != "" {
		sid = &stopID
	}
	_, err := q.Exec(ctx, `
		INSERT INTO trip_stop_events (trip_id, school_id, stop_id, event_type, missed, occurred_at, latitude, longitude)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT ON CONSTRAINT trip_stop_events_once DO NOTHING`,
		tripID, schoolID, sid, eventType, missed, at, lat, lng)
	return err
}

// TripStopEvents lists a trip's stop events in time order (timeline, history).
func TripStopEvents(ctx context.Context, q DBTX, tripID string) ([]StopEventRow, error) {
	rows, err := q.Query(ctx, `
		SELECT e.stop_id, s.name, e.event_type, e.missed, e.occurred_at
		FROM trip_stop_events e LEFT JOIN stops s ON s.id = e.stop_id
		WHERE e.trip_id = $1 ORDER BY e.occurred_at, e.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StopEventRow{}
	for rows.Next() {
		var r StopEventRow
		if err := rows.Scan(&r.StopID, &r.StopName, &r.EventType, &r.Missed, &r.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
