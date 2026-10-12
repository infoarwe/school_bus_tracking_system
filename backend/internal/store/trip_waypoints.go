package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// TripWaypoint is one ad-hoc destination on a Special Trip/Outing, in travel
// order. It never touches the permanent routes/stops tables: a Special Trip's
// destinations are stored per-trip so no permanent route is duplicated for a
// one-off outing.
type TripWaypoint struct {
	ID              string    `json:"id"`
	TripID          string    `json:"trip_id"`
	Sequence        int       `json:"sequence"`
	Name            string    `json:"name"`
	Landmark        string    `json:"landmark"`
	Latitude        float64   `json:"latitude"`
	Longitude       float64   `json:"longitude"`
	GeofenceRadiusM int       `json:"geofence_radius_m"`
	ArrivedAt       *string   `json:"arrived_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type WaypointInput struct {
	Name            string
	Landmark        string
	Latitude        float64
	Longitude       float64
	GeofenceRadiusM int
}

const waypointCols = `id, trip_id, sequence, name, landmark, latitude, longitude, geofence_radius_m, arrived_at::text, created_at, updated_at`

func scanWaypoint(row pgx.Row) (*TripWaypoint, error) {
	var wp TripWaypoint
	err := row.Scan(&wp.ID, &wp.TripID, &wp.Sequence, &wp.Name, &wp.Landmark, &wp.Latitude, &wp.Longitude,
		&wp.GeofenceRadiusM, &wp.ArrivedAt, &wp.CreatedAt, &wp.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &wp, nil
}

// ListWaypoints returns a Special Trip/Outing's destinations in travel order.
// The caller has already checked the trip's school.
func ListWaypoints(ctx context.Context, q DBTX, tripID string) ([]TripWaypoint, error) {
	rows, err := q.Query(ctx, `SELECT `+waypointCols+` FROM trip_waypoints WHERE trip_id = $1 ORDER BY sequence`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TripWaypoint{}
	for rows.Next() {
		wp, err := scanWaypoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *wp)
	}
	return out, rows.Err()
}

// ReplaceWaypoints replaces a trip's whole destination list. Outings are
// created/edited as a whole (unlike permanent stops, which get fine-grained
// CRUD because they outlive any one day), so this is delete-then-bulk-insert
// rather than incremental add/remove/reorder. Run inside the same transaction
// as the trip create/update.
func ReplaceWaypoints(ctx context.Context, q DBTX, schoolID, tripID string, in []WaypointInput) error {
	if _, err := q.Exec(ctx, `DELETE FROM trip_waypoints WHERE trip_id = $1`, tripID); err != nil {
		return err
	}
	for i, wp := range in {
		if _, err := q.Exec(ctx, `
			INSERT INTO trip_waypoints (trip_id, school_id, sequence, name, landmark, latitude, longitude, geofence_radius_m)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			tripID, schoolID, i+1, wp.Name, wp.Landmark, wp.Latitude, wp.Longitude, wp.GeofenceRadiusM); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

// WaypointAsStop converts a waypoint into a models.Stop-shaped value, so a
// Special Trip's destinations can be returned through the same `stops[]` JSON
// shape the other two trip types already use (RouteID left empty: a waypoint
// belongs to a trip, not a permanent route; PickupTime/DropTime are always nil
// since an outing has no fixed schedule).
func WaypointAsStop(wp TripWaypoint) models.Stop {
	return models.Stop{
		ID: wp.ID, RouteID: "", Name: wp.Name, Landmark: wp.Landmark,
		Latitude: wp.Latitude, Longitude: wp.Longitude, Sequence: wp.Sequence,
		GeofenceRadiusM: wp.GeofenceRadiusM, CreatedAt: wp.CreatedAt, UpdatedAt: wp.UpdatedAt,
	}
}
