package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

const routeCols = `r.id, r.school_id, r.name, r.code, r.start_point, r.description, r.supports_pickup,
	r.supports_drop, r.status, (SELECT count(*) FROM stops s WHERE s.route_id = r.id), r.created_at, r.updated_at`

func scanRoute(row pgx.Row) (*models.Route, error) {
	var r models.Route
	err := row.Scan(&r.ID, &r.SchoolID, &r.Name, &r.Code, &r.StartPoint, &r.Description, &r.SupportsPickup,
		&r.SupportsDrop, &r.Status, &r.StopCount, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &r, nil
}

type RouteInput struct {
	Name           string
	Code           string
	StartPoint     string
	Description    string
	SupportsPickup bool
	SupportsDrop   bool
}

func ListRoutes(ctx context.Context, q DBTX, schoolID string, f ListFilter, p Page) ([]models.Route, int, error) {
	const where = ` WHERE r.school_id = $1 AND ($2 = '' OR r.status = $2)
		AND ($3 = '' OR r.name ILIKE '%' || $3 || '%' OR r.code ILIKE '%' || $3 || '%')`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM routes r`+where, schoolID, f.Status, f.Q).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `SELECT `+routeCols+` FROM routes r`+where+` ORDER BY r.code LIMIT $4 OFFSET $5`,
		schoolID, f.Status, f.Q, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Route{}
	for rows.Next() {
		r, err := scanRoute(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

func GetRoute(ctx context.Context, q DBTX, schoolID, id string) (*models.Route, error) {
	return scanRoute(q.QueryRow(ctx, `SELECT `+routeCols+` FROM routes r WHERE r.id = $1 AND r.school_id = $2`, id, schoolID))
}

func CreateRoute(ctx context.Context, q DBTX, schoolID string, in RouteInput) (*models.Route, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO routes (school_id, name, code, start_point, description, supports_pickup, supports_drop)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		schoolID, in.Name, in.Code, in.StartPoint, in.Description, in.SupportsPickup, in.SupportsDrop).Scan(&id)
	if err != nil {
		return nil, mapErr(err)
	}
	return GetRoute(ctx, q, schoolID, id)
}

func UpdateRoute(ctx context.Context, q DBTX, schoolID, id string, in RouteInput) (*models.Route, error) {
	tag, err := q.Exec(ctx, `
		UPDATE routes SET name = $3, code = $4, start_point = $5, description = $6,
			supports_pickup = $7, supports_drop = $8
		WHERE id = $1 AND school_id = $2`,
		id, schoolID, in.Name, in.Code, in.StartPoint, in.Description, in.SupportsPickup, in.SupportsDrop)
	if err != nil {
		return nil, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return GetRoute(ctx, q, schoolID, id)
}

func SetRouteStatus(ctx context.Context, q DBTX, schoolID, id, status string) (*models.Route, error) {
	tag, err := q.Exec(ctx, `UPDATE routes SET status = $3 WHERE id = $1 AND school_id = $2`, id, schoolID, status)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return GetRoute(ctx, q, schoolID, id)
}

// Stops

const stopCols = `id, route_id, name, landmark, latitude, longitude, sequence,
	to_char(pickup_time, 'HH24:MI'), to_char(drop_time, 'HH24:MI'), geofence_radius_m, created_at, updated_at`

func scanStop(row pgx.Row) (*models.Stop, error) {
	var s models.Stop
	err := row.Scan(&s.ID, &s.RouteID, &s.Name, &s.Landmark, &s.Latitude, &s.Longitude, &s.Sequence,
		&s.PickupTime, &s.DropTime, &s.GeofenceRadiusM, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &s, nil
}

type StopInput struct {
	Name            string
	Landmark        string
	Latitude        float64
	Longitude       float64
	PickupTime      *string
	DropTime        *string
	GeofenceRadiusM int
}

// ListStops returns a route's stops in travel order. The caller has already checked the route's school.
func ListStops(ctx context.Context, q DBTX, routeID string) ([]models.Stop, error) {
	rows, err := q.Query(ctx, `SELECT `+stopCols+` FROM stops WHERE route_id = $1 ORDER BY sequence`, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Stop{}
	for rows.Next() {
		s, err := scanStop(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func GetStop(ctx context.Context, q DBTX, schoolID, routeID, id string) (*models.Stop, error) {
	return scanStop(q.QueryRow(ctx, `SELECT `+stopCols+` FROM stops WHERE id = $1 AND route_id = $2 AND school_id = $3`,
		id, routeID, schoolID))
}

// LockRoute serializes stop changes on one route so sequences cannot collide.
func LockRoute(ctx context.Context, q DBTX, schoolID, routeID string) error {
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM routes WHERE id = $1 AND school_id = $2 FOR UPDATE`, routeID, schoolID).Scan(&id)
	return mapErr(err)
}

// CreateStop inserts at position (1-based); nil or past the end appends.
// Run inside a transaction after LockRoute.
func CreateStop(ctx context.Context, q DBTX, schoolID, routeID string, position *int, in StopInput) (*models.Stop, error) {
	var count int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM stops WHERE route_id = $1`, routeID).Scan(&count); err != nil {
		return nil, err
	}
	seq := count + 1
	if position != nil && *position >= 1 && *position <= count {
		seq = *position
		if _, err := q.Exec(ctx, `UPDATE stops SET sequence = sequence + 1 WHERE route_id = $1 AND sequence >= $2`,
			routeID, seq); err != nil {
			return nil, err
		}
	}
	return scanStop(q.QueryRow(ctx, `
		INSERT INTO stops (school_id, route_id, name, landmark, latitude, longitude, sequence,
			pickup_time, drop_time, geofence_radius_m)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::time, $9::time, $10) RETURNING `+stopCols,
		schoolID, routeID, in.Name, in.Landmark, in.Latitude, in.Longitude, seq,
		in.PickupTime, in.DropTime, in.GeofenceRadiusM))
}

func UpdateStop(ctx context.Context, q DBTX, schoolID, routeID, id string, in StopInput) (*models.Stop, error) {
	return scanStop(q.QueryRow(ctx, `
		UPDATE stops SET name = $4, landmark = $5, latitude = $6, longitude = $7,
			pickup_time = $8::time, drop_time = $9::time, geofence_radius_m = $10
		WHERE id = $1 AND route_id = $2 AND school_id = $3 RETURNING `+stopCols,
		id, routeID, schoolID, in.Name, in.Landmark, in.Latitude, in.Longitude,
		in.PickupTime, in.DropTime, in.GeofenceRadiusM))
}

// DeleteStop removes a stop and closes the gap in the sequence. Run after LockRoute.
func DeleteStop(ctx context.Context, q DBTX, schoolID, routeID, id string) error {
	var seq int
	err := q.QueryRow(ctx, `DELETE FROM stops WHERE id = $1 AND route_id = $2 AND school_id = $3 RETURNING sequence`,
		id, routeID, schoolID).Scan(&seq)
	if err != nil {
		return mapErr(err)
	}
	_, err = q.Exec(ctx, `UPDATE stops SET sequence = sequence - 1 WHERE route_id = $1 AND sequence > $2`, routeID, seq)
	return err
}

// ErrStopOrderMismatch: the list must contain exactly the route's stops, each once.
var ErrStopOrderMismatch = fmt.Errorf("stop list does not match the route's stops")

// ReorderStops sets sequence = position in stopIDs. Run after LockRoute.
func ReorderStops(ctx context.Context, q DBTX, routeID string, stopIDs []string) error {
	var matched int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM stops WHERE route_id = $1 AND id = ANY($2::uuid[])`, routeID, stopIDs).Scan(&matched)
	if err != nil {
		return err
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM stops WHERE route_id = $1`, routeID).Scan(&total); err != nil {
		return err
	}
	if matched != len(stopIDs) || matched != total {
		return ErrStopOrderMismatch
	}
	_, err = q.Exec(ctx, `
		UPDATE stops s SET sequence = o.pos
		FROM unnest($2::uuid[]) WITH ORDINALITY AS o(id, pos)
		WHERE s.id = o.id AND s.route_id = $1`, routeID, stopIDs)
	return err
}
