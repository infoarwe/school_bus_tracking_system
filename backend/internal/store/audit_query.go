package store

import (
	"context"
	"encoding/json"
	"time"
)

// AuditRow is one audit log entry for the viewer.
type AuditRow struct {
	ID         string          `json:"id"`
	SchoolID   *string         `json:"school_id"`
	SchoolName *string         `json:"school_name"`
	ActorID    *string         `json:"actor_id"`
	ActorName  *string         `json:"actor_name"`
	ActorRole  string          `json:"actor_role"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   *string         `json:"entity_id"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
	IP         string          `json:"ip"`
	RequestID  string          `json:"request_id"`
	CreatedAt  time.Time       `json:"created_at"`
}

type AuditFilter struct {
	// SchoolID scopes to one school. AllSchools (Super Admin only) lifts the scope,
	// including platform-level entries that belong to no school.
	SchoolID   string
	AllSchools bool
	// EntityTypes, if set, limits to these entity types (Transport Manager: transport only).
	EntityTypes []string
	EntityType  string
	EntityID    string
	Action      string // prefix, e.g. "trip." or "user.suspend"
	ActorID     string
	From, To    string // YYYY-MM-DD, in the school's (or UTC for platform) day
}

// ListAudit returns audit entries newest first.
func ListAudit(ctx context.Context, q DBTX, f AuditFilter, p Page) ([]AuditRow, int, error) {
	const where = ` WHERE ($1 OR a.school_id::text = $2)
		AND (cardinality($3::text[]) = 0 OR a.entity_type = ANY($3))
		AND ($4 = '' OR a.entity_type = $4)
		AND ($5 = '' OR a.entity_id::text = $5)
		AND ($6 = '' OR a.action LIKE $6 || '%')
		AND ($7 = '' OR a.actor_user_id::text = $7)
		AND ($8 = '' OR (a.created_at AT TIME ZONE coalesce(s.timezone, 'UTC'))::date >= $8::date)
		AND ($9 = '' OR (a.created_at AT TIME ZONE coalesce(s.timezone, 'UTC'))::date <= $9::date)`
	entityTypes := f.EntityTypes
	if entityTypes == nil {
		entityTypes = []string{}
	}
	args := []any{f.AllSchools, f.SchoolID, entityTypes, f.EntityType, f.EntityID, f.Action, f.ActorID, f.From, f.To}
	const from = ` FROM audit_logs a LEFT JOIN schools s ON s.id = a.school_id LEFT JOIN users u ON u.id = a.actor_user_id`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `SELECT a.id, a.school_id, s.name, a.actor_user_id, u.name, a.actor_role, a.action, a.entity_type,
		a.entity_id, a.before, a.after, a.ip, a.request_id, a.created_at`+from+where+`
		ORDER BY a.created_at DESC, a.id LIMIT $10 OFFSET $11`, append(args, p.PageSize, p.Offset())...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AuditRow{}
	for rows.Next() {
		var r AuditRow
		var before, after []byte
		if err := rows.Scan(&r.ID, &r.SchoolID, &r.SchoolName, &r.ActorID, &r.ActorName, &r.ActorRole, &r.Action, &r.EntityType,
			&r.EntityID, &before, &after, &r.IP, &r.RequestID, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		r.Before, r.After = json.RawMessage(before), json.RawMessage(after)
		if before == nil {
			r.Before = json.RawMessage("null")
		}
		if after == nil {
			r.After = json.RawMessage("null")
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// TripLocationPoints returns a trip's stored GPS track for replay.
type TrackPoint struct {
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	SpeedMPS   *float64  `json:"speed_mps"`
	RecordedAt time.Time `json:"recorded_at"`
}

func TripTrack(ctx context.Context, q DBTX, tripID string) ([]TrackPoint, error) {
	rows, err := q.Query(ctx, `SELECT latitude, longitude, speed_mps, recorded_at FROM trip_locations
		WHERE trip_id = $1 ORDER BY recorded_at`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackPoint{}
	for rows.Next() {
		var p TrackPoint
		var speed *float32
		if err := rows.Scan(&p.Latitude, &p.Longitude, &speed, &p.RecordedAt); err != nil {
			return nil, err
		}
		if speed != nil {
			v := float64(*speed)
			p.SpeedMPS = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
