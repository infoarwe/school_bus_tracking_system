package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type NotificationInput struct {
	SchoolID  string
	Type      string
	Title     string
	Body      string
	Data      map[string]string
	TripID    *string
	RouteID   *string
	DedupeKey *string // automatic notifications: fires once per key
	CreatedBy *string
}

// InsertNotification returns created=false if a notification with the same
// dedupe key already exists (the event was already notified).
func InsertNotification(ctx context.Context, q DBTX, n NotificationInput) (id string, created bool, err error) {
	data, _ := json.Marshal(n.Data)
	err = q.QueryRow(ctx, `
		INSERT INTO notifications (school_id, type, title, body, data, trip_id, route_id, dedupe_key, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (dedupe_key) DO NOTHING RETURNING id`,
		n.SchoolID, n.Type, n.Title, n.Body, data, n.TripID, n.RouteID, n.DedupeKey, n.CreatedBy).Scan(&id)
	if err != nil {
		if errors.Is(mapErr(err), ErrNotFound) { // ON CONFLICT: already notified
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

// Rider is a parent to notify about one child (StudentID nil: not about one child).
type Rider struct {
	UserID      string
	StudentID   *string
	StudentName string
}

func scanRiders(ctx context.Context, q DBTX, sql string, args ...any) ([]Rider, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rider{}
	for rows.Next() {
		var r Rider
		if err := rows.Scan(&r.UserID, &r.StudentID, &r.StudentName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// parentsOfStudents joins active students to their active parents' logins.
const parentsOfStudents = `
	JOIN students s ON s.id = sa.student_id AND s.status = 'active'
	JOIN parent_students ps ON ps.student_id = s.id
	JOIN parents p ON p.id = ps.parent_id AND p.status = 'active'
	JOIN users u ON u.id = p.user_id AND u.status = 'active'`

// TripRiders: parents of the students who ride this trip (a stop for its trip type),
// optionally only those whose stop is stopID.
func TripRiders(ctx context.Context, q DBTX, tripID, stopID string) ([]Rider, error) {
	return scanRiders(ctx, q, `
		SELECT DISTINCT p.user_id, s.id, s.name
		FROM trips t
		JOIN student_assignments sa ON sa.route_id = t.route_id AND sa.ended_at IS NULL`+parentsOfStudents+`
		WHERE t.id = $1
		AND (CASE WHEN t.trip_type = 'morning_pickup' THEN sa.pickup_stop_id ELSE sa.drop_stop_id END) IS NOT NULL
		AND ($2 = '' OR (CASE WHEN t.trip_type = 'morning_pickup' THEN sa.pickup_stop_id ELSE sa.drop_stop_id END)::text = $2)`,
		tripID, stopID)
}

// RouteRiders: parents of every student assigned to the route.
func RouteRiders(ctx context.Context, q DBTX, schoolID, routeID string) ([]Rider, error) {
	return scanRiders(ctx, q, `
		SELECT DISTINCT p.user_id, s.id, s.name
		FROM student_assignments sa`+parentsOfStudents+`
		WHERE sa.route_id = $1 AND sa.school_id = $2 AND sa.ended_at IS NULL`, routeID, schoolID)
}

// SchoolParents: every active parent with an active child at the school (not about one child).
func SchoolParents(ctx context.Context, q DBTX, schoolID string) ([]Rider, error) {
	return scanRiders(ctx, q, `
		SELECT DISTINCT p.user_id, NULL::uuid, ''
		FROM parents p
		JOIN users u ON u.id = p.user_id AND u.status = 'active'
		JOIN parent_students ps ON ps.parent_id = p.id
		JOIN students s ON s.id = ps.student_id AND s.status = 'active'
		WHERE p.school_id = $1 AND p.status = 'active'`, schoolID)
}

func InsertRecipients(ctx context.Context, q DBTX, notificationID string, riders []Rider) error {
	users := make([]string, len(riders))
	students := make([]*string, len(riders))
	for i, r := range riders {
		users[i], students[i] = r.UserID, r.StudentID
	}
	_, err := q.Exec(ctx, `
		INSERT INTO notification_recipients (notification_id, user_id, student_id)
		SELECT $1, u, s FROM unnest($2::uuid[], $3::uuid[]) AS x(u, s)
		ON CONFLICT ON CONSTRAINT notification_recipients_once DO NOTHING`, notificationID, users, students)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE notifications SET recipient_count =
		(SELECT count(DISTINCT user_id) FROM notification_recipients WHERE notification_id = $1) WHERE id = $1`, notificationID)
	return err
}

// ActiveDeviceTokens: push tokens of devices whose login session is still active.
func ActiveDeviceTokens(ctx context.Context, q DBTX, userIDs []string) (map[string][]string, error) {
	rows, err := q.Query(ctx, `
		SELECT d.user_id, d.token FROM device_tokens d
		JOIN user_sessions s ON s.id = d.session_id AND s.revoked_at IS NULL AND s.expires_at > now()
		JOIN users u ON u.id = d.user_id AND u.status = 'active'
		WHERE d.user_id = ANY($1::uuid[])`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var user, token string
		if err := rows.Scan(&user, &token); err != nil {
			return nil, err
		}
		out[user] = append(out[user], token)
	}
	return out, rows.Err()
}

type DeliveryInput struct {
	UserID, Token, Title, Body string
	Data                       map[string]string
}

func InsertDeliveries(ctx context.Context, q DBTX, notificationID string, ds []DeliveryInput) error {
	for _, d := range ds {
		data, _ := json.Marshal(d.Data)
		if _, err := q.Exec(ctx, `
			INSERT INTO push_deliveries (notification_id, user_id, token, title, body, data)
			VALUES ($1, $2, $3, $4, $5, $6)`, notificationID, d.UserID, d.Token, d.Title, d.Body, data); err != nil {
			return err
		}
	}
	return nil
}

// Delivery is a queued push.
type Delivery struct {
	ID       int64
	Token    string
	Title    string
	Body     string
	Data     map[string]string
	Attempts int
}

// ClaimDeliveries takes due pushes for sending. Claimed rows are pushed a minute
// into the future, so another worker does not take them meanwhile.
func ClaimDeliveries(ctx context.Context, q DBTX, limit int) ([]Delivery, error) {
	rows, err := q.Query(ctx, `
		UPDATE push_deliveries SET next_attempt_at = now() + interval '1 minute'
		WHERE id IN (SELECT id FROM push_deliveries WHERE status = 'pending' AND next_attempt_at <= now()
			ORDER BY next_attempt_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id, token, title, body, data, attempts`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var data []byte
		if err := rows.Scan(&d.ID, &d.Token, &d.Title, &d.Body, &data, &d.Attempts); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(data, &d.Data)
		out = append(out, d)
	}
	return out, rows.Err()
}

// FinishDelivery records the outcome. status pending + retryAt schedules a retry.
func FinishDelivery(ctx context.Context, q DBTX, id int64, status, lastError string, retryAt *time.Time) error {
	_, err := q.Exec(ctx, `
		UPDATE push_deliveries SET status = $2, last_error = $3, attempts = attempts + 1,
			next_attempt_at = COALESCE($4, next_attempt_at),
			sent_at = CASE WHEN $2 = 'sent' THEN now() ELSE sent_at END
		WHERE id = $1`, id, status, lastError, retryAt)
	return err
}

func DeleteDeviceToken(ctx context.Context, q DBTX, token string) error {
	_, err := q.Exec(ctx, `DELETE FROM device_tokens WHERE token = $1`, token)
	return err
}

// UpsertDeviceToken registers a device for the current login session. A token
// moves to the new user/session if the phone changed hands.
func UpsertDeviceToken(ctx context.Context, q DBTX, userID, sessionID, token, platform string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO device_tokens (user_id, session_id, token, platform) VALUES ($1, $2, $3, $4)
		ON CONFLICT (token) DO UPDATE SET user_id = EXCLUDED.user_id, session_id = EXCLUDED.session_id,
			platform = EXCLUDED.platform, last_seen_at = now()`, userID, sessionID, token, platform)
	return err
}

func DeleteUserDeviceToken(ctx context.Context, q DBTX, userID, token string) error {
	_, err := q.Exec(ctx, `DELETE FROM device_tokens WHERE token = $1 AND user_id = $2`, token, userID)
	return err
}

// InboxItem is one notification in a parent's inbox.
type InboxItem struct {
	ID          string            `json:"id"` // recipient row id (used to mark read)
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Data        map[string]string `json:"data"`
	StudentID   *string           `json:"student_id"` // null: about all children / the school
	StudentName *string           `json:"student_name"`
	ReadAt      *time.Time        `json:"read_at"`
	CreatedAt   time.Time         `json:"created_at"`
}

// Inbox lists a user's notifications. With studentID, only those about that
// child plus the ones about no particular child.
func Inbox(ctx context.Context, q DBTX, userID, studentID string, p Page) ([]InboxItem, int, error) {
	const where = ` WHERE r.user_id = $1 AND ($2 = '' OR r.student_id IS NULL OR r.student_id::text = $2)`
	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM notification_recipients r`+where, userID, studentID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `
		SELECT r.id, n.type, n.title, n.body, n.data, r.student_id, s.name, r.read_at, r.created_at
		FROM notification_recipients r
		JOIN notifications n ON n.id = r.notification_id
		LEFT JOIN students s ON s.id = r.student_id`+where+`
		ORDER BY r.created_at DESC LIMIT $3 OFFSET $4`, userID, studentID, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []InboxItem{}
	for rows.Next() {
		var it InboxItem
		var data []byte
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.Body, &data, &it.StudentID, &it.StudentName, &it.ReadAt, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(data, &it.Data)
		out = append(out, it)
	}
	return out, total, rows.Err()
}

func UnreadCount(ctx context.Context, q DBTX, userID, studentID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM notification_recipients
		WHERE user_id = $1 AND read_at IS NULL AND ($2 = '' OR student_id IS NULL OR student_id::text = $2)`,
		userID, studentID).Scan(&n)
	return n, err
}

// MarkRead marks the user's own inbox items read (others' ids are ignored).
func MarkRead(ctx context.Context, q DBTX, userID string, ids []string) error {
	_, err := q.Exec(ctx, `UPDATE notification_recipients SET read_at = now()
		WHERE user_id = $1 AND id = ANY($2::uuid[]) AND read_at IS NULL`, userID, ids)
	return err
}

// DeliveryStats summarises one notification's reach.
type DeliveryStats struct {
	Recipients int `json:"recipients"`
	Read       int `json:"read"`
	Sent       int `json:"sent"`
	Pending    int `json:"pending"`
	Failed     int `json:"failed"` // failed + invalid tokens
}

func NotificationStats(ctx context.Context, q DBTX, notificationID string) (*DeliveryStats, error) {
	var s DeliveryStats
	err := q.QueryRow(ctx, `
		SELECT (SELECT count(DISTINCT user_id) FROM notification_recipients WHERE notification_id = $1),
			(SELECT count(DISTINCT user_id) FROM notification_recipients WHERE notification_id = $1 AND read_at IS NOT NULL),
			count(*) FILTER (WHERE status = 'sent'),
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status IN ('failed', 'invalid'))
		FROM push_deliveries WHERE notification_id = $1`, notificationID).
		Scan(&s.Recipients, &s.Read, &s.Sent, &s.Pending, &s.Failed)
	return &s, err
}
