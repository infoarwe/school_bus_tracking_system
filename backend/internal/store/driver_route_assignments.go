package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

const assignmentSelect = `SELECT a.id, a.school_id, a.trip_type,
	r.id, r.code, r.name, b.id, b.vehicle_number, d.id, du.name, du.mobile,
	a.created_at, a.updated_at
	FROM driver_route_assignments a
	JOIN routes r ON r.id = a.route_id
	JOIN buses b ON b.id = a.bus_id
	JOIN drivers d ON d.id = a.driver_id
	JOIN users du ON du.id = d.user_id`

func scanAssignment(row pgx.Row) (*models.DriverRouteAssignment, error) {
	var a models.DriverRouteAssignment
	err := row.Scan(&a.ID, &a.SchoolID, &a.TripType,
		&a.Route.ID, &a.Route.Code, &a.Route.Name, &a.Bus.ID, &a.Bus.VehicleNumber,
		&a.Driver.ID, &a.Driver.Name, &a.Driver.Mobile, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &a, nil
}

func scanAssignments(rows pgx.Rows) ([]models.DriverRouteAssignment, error) {
	defer rows.Close()
	out := []models.DriverRouteAssignment{}
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// AssignmentInput is a permanent Morning Pickup / Evening Drop driver+bus+route binding.
type AssignmentInput struct {
	RouteID  string
	TripType string
	DriverID string
	BusID    string
}

// CreateAssignment inserts a new current assignment. A conflict on
// driver_route_assignments_route_key/_bus_key/_driver_key (another current assignment
// already holds that route, bus or driver for this trip type) surfaces as *ConflictError.
func CreateAssignment(ctx context.Context, q DBTX, schoolID string, in AssignmentInput, createdBy *string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO driver_route_assignments (school_id, route_id, trip_type, driver_id, bus_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		schoolID, in.RouteID, in.TripType, in.DriverID, in.BusID, createdBy).Scan(&id)
	return id, mapErr(err)
}

// UpdateAssignment ends the current row by id and creates a fresh one with the new
// values, preserving history - the same end-old-insert-new shape as store.AssignStudent.
func UpdateAssignment(ctx context.Context, q DBTX, schoolID, id string, in AssignmentInput, updatedBy *string) (string, error) {
	tag, err := q.Exec(ctx, `UPDATE driver_route_assignments SET ended_at = now()
		WHERE id = $1 AND school_id = $2 AND ended_at IS NULL`, id, schoolID)
	if err != nil {
		return "", mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return CreateAssignment(ctx, q, schoolID, in, updatedBy)
}

// DeactivateAssignment ends the current row by id, with no replacement.
func DeactivateAssignment(ctx context.Context, q DBTX, schoolID, id string) error {
	tag, err := q.Exec(ctx, `UPDATE driver_route_assignments SET ended_at = now()
		WHERE id = $1 AND school_id = $2 AND ended_at IS NULL`, id, schoolID)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetAssignment returns a current or past assignment by id, scoped to the school.
func GetAssignment(ctx context.Context, q DBTX, schoolID, id string) (*models.DriverRouteAssignment, error) {
	return scanAssignment(q.QueryRow(ctx, assignmentSelect+` WHERE a.id = $1 AND a.school_id = $2`, id, schoolID))
}

// ListAssignments lists current (ended_at IS NULL) assignments, optionally for one driver.
func ListAssignments(ctx context.Context, q DBTX, schoolID string, driverID string) ([]models.DriverRouteAssignment, error) {
	rows, err := q.Query(ctx, assignmentSelect+`
		WHERE a.school_id = $1 AND a.ended_at IS NULL AND ($2 = '' OR a.driver_id::text = $2)
		ORDER BY r.code, a.trip_type`, schoolID, driverID)
	if err != nil {
		return nil, err
	}
	return scanAssignments(rows)
}

// currentAssignment is the minimal projection EnsureDriverDailyTrips needs per slot.
type currentAssignment struct {
	RouteID, BusID, TripType string
}

func currentAssignmentsForDriver(ctx context.Context, q DBTX, driverID string) ([]currentAssignment, error) {
	rows, err := q.Query(ctx, `SELECT route_id, bus_id, trip_type FROM driver_route_assignments
		WHERE driver_id = $1 AND ended_at IS NULL`, driverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []currentAssignment{}
	for rows.Next() {
		var c currentAssignment
		if err := rows.Scan(&c.RouteID, &c.BusID, &c.TripType); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// EnsureDriverDailyTrips materializes a trips row for each of the driver's current
// permanent assignments on date, if one doesn't already exist. Mirrors CopyTrips' exact
// idiom (handlers/trips.go): INSERT ... ON CONFLICT DO NOTHING, so the trips table's own
// unique indexes (trips_route_slot_key/trips_bus_slot_key/trips_driver_slot_key) are the
// only guard needed - a trip in ANY status (scheduled, started, completed, or even
// cancelled-and-replaced) already occupying that route/bus/driver + date + type slot is
// left completely untouched; nothing is ever updated here, only inserted when missing.
// created_by is left NULL (not the calling driver's own user id), so the resulting trip's
// assigned_by computes as "staff", not "driver" - a driver cannot self-release a
// permanently assigned trip the way they can a self-claimed one (route_slots.go).
func EnsureDriverDailyTrips(ctx context.Context, q DBTX, schoolID, driverID, date string) error {
	assignments, err := currentAssignmentsForDriver(ctx, q, driverID)
	if err != nil {
		return err
	}
	for _, asn := range assignments {
		var id string
		err := q.QueryRow(ctx, `
			INSERT INTO trips (school_id, trip_date, trip_type, route_id, bus_id, driver_id, created_by)
			VALUES ($1, $2::date, $3, $4, $5, $6, NULL)
			ON CONFLICT DO NOTHING RETURNING id`,
			schoolID, date, asn.TripType, asn.RouteID, asn.BusID, driverID).Scan(&id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue // a trip already occupies this slot - exactly the desired no-op
			}
			return err
		}
		if err := InsertTripHistory(ctx, q, TripHistoryEntry{
			TripID: id, SchoolID: schoolID, To: models.TripScheduled,
			Reason: "materialized from permanent route assignment",
		}); err != nil {
			return err
		}
	}
	return nil
}
