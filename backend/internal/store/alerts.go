package store

import (
	"context"
	"time"
)

// Alert is a delay report or an emergency, as shown on the staff alerts panel.
type Alert struct {
	Kind       string    `json:"kind"` // delay | emergency
	ID         string    `json:"id"`
	TripID     string    `json:"trip_id"`
	TripType   string    `json:"trip_type"`
	RouteCode  string    `json:"route_code"`
	BusNumber  string    `json:"bus_number"`
	DriverName string    `json:"driver_name"`
	CreatedAt  time.Time `json:"created_at"`
	ReportedBy *string   `json:"reported_by"` // name

	// delay
	Minutes *int    `json:"minutes,omitempty"`
	Reason  *string `json:"reason,omitempty"`
	Note    *string `json:"note,omitempty"`

	// emergency
	Message        *string    `json:"message,omitempty"`
	Latitude       *float64   `json:"latitude,omitempty"`
	Longitude      *float64   `json:"longitude,omitempty"`
	Status         *string    `json:"status,omitempty"` // open | acknowledged | resolved
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

const tripInfoJoins = `
	JOIN trips t ON t.id = x.trip_id
	JOIN routes r ON r.id = t.route_id
	JOIN buses b ON b.id = t.bus_id
	JOIN drivers d ON d.id = t.driver_id
	JOIN users du ON du.id = d.user_id
	LEFT JOIN users rep ON rep.id = x.reported_by`

func scanAlerts(ctx context.Context, q DBTX, sql string, args ...any) ([]Alert, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.Kind, &a.ID, &a.TripID, &a.TripType, &a.RouteCode, &a.BusNumber, &a.DriverName,
			&a.CreatedAt, &a.ReportedBy, &a.Minutes, &a.Reason, &a.Note, &a.Message, &a.Latitude, &a.Longitude,
			&a.Status, &a.AcknowledgedAt, &a.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const delaySelect = `SELECT 'delay', x.id, x.trip_id, t.trip_type, r.code, b.vehicle_number, du.name, x.created_at, rep.name,
	x.minutes, x.reason, x.note, NULL::text, NULL::float8, NULL::float8, NULL::text, NULL::timestamptz, NULL::timestamptz
	FROM delay_events x` + tripInfoJoins

const emergencySelect = `SELECT 'emergency', x.id, x.trip_id, t.trip_type, r.code, b.vehicle_number, du.name, x.created_at, rep.name,
	NULL::int, NULL::text, NULL::text, x.message, x.latitude, x.longitude, x.status, x.acknowledged_at, x.resolved_at
	FROM emergencies x` + tripInfoJoins

type DelayInput struct {
	TripID, Reason, Note, ReportedBy, ReporterRole string
	Minutes                                        int
}

func InsertDelay(ctx context.Context, q DBTX, schoolID string, d DelayInput) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO delay_events (school_id, trip_id, minutes, reason, note, reported_by, reporter_role)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		schoolID, d.TripID, d.Minutes, d.Reason, d.Note, d.ReportedBy, d.ReporterRole).Scan(&id)
	return id, mapErr(err)
}

func SetDelayNotification(ctx context.Context, q DBTX, delayID, notificationID string) error {
	_, err := q.Exec(ctx, `UPDATE delay_events SET notification_id = $2 WHERE id = $1`, delayID, notificationID)
	return err
}

func GetDelayAlert(ctx context.Context, q DBTX, id string) (*Alert, error) {
	list, err := scanAlerts(ctx, q, delaySelect+` WHERE x.id = $1`, id)
	if err != nil || len(list) == 0 {
		return nil, firstErr(err, ErrNotFound)
	}
	return &list[0], nil
}

type EmergencyInput struct {
	TripID, Message, ReportedBy string
	Latitude, Longitude         *float64
}

func InsertEmergency(ctx context.Context, q DBTX, schoolID string, e EmergencyInput) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO emergencies (school_id, trip_id, message, latitude, longitude, reported_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		schoolID, e.TripID, e.Message, e.Latitude, e.Longitude, e.ReportedBy).Scan(&id)
	return id, mapErr(err)
}

func GetEmergencyAlert(ctx context.Context, q DBTX, schoolID, id string) (*Alert, error) {
	list, err := scanAlerts(ctx, q, emergencySelect+` WHERE x.id = $1 AND x.school_id = $2`, id, schoolID)
	if err != nil || len(list) == 0 {
		return nil, firstErr(err, ErrNotFound)
	}
	return &list[0], nil
}

// SetEmergencyStatus moves an emergency forward (open → acknowledged → resolved).
func SetEmergencyStatus(ctx context.Context, q DBTX, schoolID, id, status, byUserID string) error {
	tag, err := q.Exec(ctx, `
		UPDATE emergencies SET status = $3,
			acknowledged_by = CASE WHEN $3 = 'acknowledged' THEN $4::uuid ELSE acknowledged_by END,
			acknowledged_at = CASE WHEN $3 = 'acknowledged' THEN now() ELSE acknowledged_at END,
			resolved_by = CASE WHEN $3 = 'resolved' THEN $4::uuid ELSE resolved_by END,
			resolved_at = CASE WHEN $3 = 'resolved' THEN now() ELSE resolved_at END
		WHERE id = $1 AND school_id = $2 AND status <> 'resolved' AND status <> $3`, id, schoolID, status, byUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStatusChanged
	}
	return nil
}

// SchoolAlerts: unresolved emergencies (any day) plus today's delays and resolved
// emergencies, newest first. "Today" is in the school's time zone.
func SchoolAlerts(ctx context.Context, q DBTX, schoolID string) ([]Alert, error) {
	return scanAlerts(ctx, q, `
		SELECT * FROM (
			`+emergencySelect+` JOIN schools s ON s.id = x.school_id
			WHERE x.school_id = $1 AND (x.status <> 'resolved' OR (x.created_at AT TIME ZONE s.timezone)::date = (now() AT TIME ZONE s.timezone)::date)
			UNION ALL
			`+delaySelect+` JOIN schools s ON s.id = x.school_id
			WHERE x.school_id = $1 AND (x.created_at AT TIME ZONE s.timezone)::date = (now() AT TIME ZONE s.timezone)::date
		) a ORDER BY 8 DESC`, schoolID)
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
