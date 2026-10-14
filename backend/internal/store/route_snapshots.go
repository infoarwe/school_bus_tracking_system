package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Route snapshot statuses (trip_route_snapshots.status).
const (
	SnapshotReady  = "ready"
	SnapshotNoGPS  = "no_gps"
	SnapshotFailed = "failed"
)

// RouteSnapshot is the travelled-route record saved when a trip is completed.
type RouteSnapshot struct {
	TripID     string     `json:"trip_id"`
	Status     string     `json:"status"`
	ImageKey   *string    `json:"-"`
	HasImage   bool       `json:"has_image"`
	Polyline   string     `json:"polyline"`
	DistanceM  int        `json:"distance_m"`
	PointCount int        `json:"point_count"`
	StartedAt  *time.Time `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at"`
	Error      string     `json:"error"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// GetRouteSnapshot returns the trip's snapshot, or ErrNotFound if none was made.
func GetRouteSnapshot(ctx context.Context, q DBTX, schoolID, tripID string) (*RouteSnapshot, error) {
	var s RouteSnapshot
	err := q.QueryRow(ctx, `SELECT trip_id, status, image_key, polyline, distance_m, point_count,
		started_at, ended_at, error, updated_at
		FROM trip_route_snapshots WHERE school_id = $1 AND trip_id = $2`, schoolID, tripID).
		Scan(&s.TripID, &s.Status, &s.ImageKey, &s.Polyline, &s.DistanceM, &s.PointCount,
			&s.StartedAt, &s.EndedAt, &s.Error, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.HasImage = s.ImageKey != nil
	return &s, nil
}

// SaveRouteSnapshot inserts or replaces the trip's snapshot.
func SaveRouteSnapshot(ctx context.Context, q DBTX, schoolID string, s RouteSnapshot) error {
	_, err := q.Exec(ctx, `INSERT INTO trip_route_snapshots
		(trip_id, school_id, status, image_key, polyline, distance_m, point_count, started_at, ended_at, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (trip_id) DO UPDATE SET status = EXCLUDED.status, image_key = EXCLUDED.image_key,
			polyline = EXCLUDED.polyline, distance_m = EXCLUDED.distance_m, point_count = EXCLUDED.point_count,
			started_at = EXCLUDED.started_at, ended_at = EXCLUDED.ended_at, error = EXCLUDED.error`,
		s.TripID, schoolID, s.Status, s.ImageKey, s.Polyline, s.DistanceM, s.PointCount, s.StartedAt, s.EndedAt, s.Error)
	return err
}
