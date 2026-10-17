package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// ParentUserIDsForStudent: the user_id (not parent_id) of every parent linked
// to this student — used to reconcile topic subscriptions when a student's
// route/stop assignment changes.
func ParentUserIDsForStudent(ctx context.Context, q DBTX, studentID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id FROM parent_students ps
		JOIN parents p ON p.id = ps.parent_id
		JOIN users u ON u.id = p.user_id
		WHERE ps.student_id = $1`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ChildAssignment is one linked, active child's current route/stop membership
// (as much as is set — a route may run only one of the two directions).
type ChildAssignment struct {
	RouteID      string
	PickupStopID *string
	DropStopID   *string
}

// ParentTopicContext is what's needed to compute a parent's full desired topic
// set: their school plus every linked active child's current assignment.
type ParentTopicContext struct {
	SchoolID    string
	Assignments []ChildAssignment
}

// ParentTopicInfo loads the data behind a parent's topic set directly from
// student_assignments (the source of truth) — never from anything the client
// reports. ok is false if userID is not an active parent with a school.
func ParentTopicInfo(ctx context.Context, q DBTX, userID string) (info ParentTopicContext, ok bool, err error) {
	err = q.QueryRow(ctx, `SELECT school_id FROM parents WHERE user_id = $1 AND status = 'active'`, userID).
		Scan(&info.SchoolID)
	if err != nil {
		if mapErr(err) == ErrNotFound {
			return info, false, nil
		}
		return info, false, err
	}
	rows, err := q.Query(ctx, `
		SELECT DISTINCT sa.route_id, sa.pickup_stop_id, sa.drop_stop_id
		FROM parent_students ps
		JOIN parents p ON p.id = ps.parent_id AND p.user_id = $1
		JOIN students s ON s.id = ps.student_id AND s.status = 'active'
		JOIN student_assignments sa ON sa.student_id = s.id AND sa.ended_at IS NULL`, userID)
	if err != nil {
		return info, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var a ChildAssignment
		if err := rows.Scan(&a.RouteID, &a.PickupStopID, &a.DropStopID); err != nil {
			return info, false, err
		}
		info.Assignments = append(info.Assignments, a)
	}
	return info, true, rows.Err()
}

// ParentDeviceToken is one active device registration belonging to a parent —
// used by the one-time topic-subscription backfill (cmd/backfill-topics).
type ParentDeviceToken struct {
	UserID string
	Token  string
}

// AllActiveParentDeviceTokens lists every device token of an active session
// belonging to an active parent user — the full set the backfill reconciles.
// "Active" mirrors ActiveDeviceTokens' own definition (a live, unexpired,
// unrevoked session, and a non-suspended user), so the backfill does the same
// work live registration/login already does for new tokens going forward.
func AllActiveParentDeviceTokens(ctx context.Context, q DBTX) ([]ParentDeviceToken, error) {
	rows, err := q.Query(ctx, `
		SELECT d.user_id, d.token FROM device_tokens d
		JOIN user_sessions s ON s.id = d.session_id AND s.revoked_at IS NULL AND s.expires_at > now()
		JOIN users u ON u.id = d.user_id AND u.status = 'active' AND u.role = 'parent'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParentDeviceToken
	for rows.Next() {
		var p ParentDeviceToken
		if err := rows.Scan(&p.UserID, &p.Token); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeviceTokensForSession: device tokens registered under one login session —
// used on logout to unsubscribe them from their topics. Topic delivery has no
// concept of "session" the way the old per-device-token send did (which
// filtered by active session at send time, via ActiveDeviceTokens); without
// this explicit unsubscribe, a bare session logout would leave Firebase still
// delivering to that device until it's overwritten by a future login or an
// explicit POST /devices/unregister.
func DeviceTokensForSession(ctx context.Context, q DBTX, sessionID string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT token FROM device_tokens WHERE session_id = $1`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

// TrackedTopics: the topics device_topic_subscriptions currently believes
// token is subscribed to (our own ledger, not a live Firebase query).
func TrackedTopics(ctx context.Context, q DBTX, token string) (map[string]bool, error) {
	rows, err := q.Query(ctx, `SELECT topic FROM device_topic_subscriptions WHERE token = $1`, token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var topic string
		if err := rows.Scan(&topic); err != nil {
			return nil, err
		}
		out[topic] = true
	}
	return out, rows.Err()
}

// AddTopicSubscription records that token is now subscribed to topic (call
// only after Firebase confirms the subscribe succeeded).
func AddTopicSubscription(ctx context.Context, q DBTX, token, topic string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO device_topic_subscriptions (token, topic) VALUES ($1, $2)
		ON CONFLICT (token, topic) DO NOTHING`, token, topic)
	return err
}

// RemoveTopicSubscription records that token is no longer subscribed to topic
// (call only after Firebase confirms the unsubscribe succeeded).
func RemoveTopicSubscription(ctx context.Context, q DBTX, token, topic string) error {
	_, err := q.Exec(ctx, `DELETE FROM device_topic_subscriptions WHERE token = $1 AND topic = $2`, token, topic)
	return err
}

// InsertTopicDelivery queues one topic-targeted push.
func InsertTopicDelivery(ctx context.Context, q DBTX, notificationID, topic string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO topic_deliveries (notification_id, topic) VALUES ($1, $2)`, notificationID, topic)
	return err
}

// TopicDelivery is one queued or sent topic push.
type TopicDelivery struct {
	ID       int64
	SchoolID string // whose Firebase project sends it
	Topic    string
	Title    string
	Body     string
	Data     map[string]string
	Attempts int
}

// ClaimTopicDeliveries takes due topic sends for sending (mirrors
// ClaimDeliveries, but one row per topic dispatch rather than per device).
func ClaimTopicDeliveries(ctx context.Context, q DBTX, limit int) ([]TopicDelivery, error) {
	rows, err := q.Query(ctx, `
		UPDATE topic_deliveries SET next_attempt_at = now() + interval '1 minute'
		WHERE id IN (SELECT td.id FROM topic_deliveries td
			WHERE td.status = 'pending' AND td.next_attempt_at <= now()
			ORDER BY td.next_attempt_at, td.id LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id, topic, attempts,
			(SELECT n.title FROM notifications n WHERE n.id = topic_deliveries.notification_id),
			(SELECT n.body FROM notifications n WHERE n.id = topic_deliveries.notification_id),
			(SELECT n.data FROM notifications n WHERE n.id = topic_deliveries.notification_id),
			(SELECT n.school_id FROM notifications n WHERE n.id = topic_deliveries.notification_id)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicDelivery{}
	for rows.Next() {
		var d TopicDelivery
		var data []byte
		if err := rows.Scan(&d.ID, &d.Topic, &d.Attempts, &d.Title, &d.Body, &data, &d.SchoolID); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(data, &d.Data)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, rows.Err()
}

// FinishTopicDelivery records the outcome. status 'pending' + retryAt
// schedules a retry; 'config_missing' is terminal (no school Firebase project
// configured — nothing to retry until the school uploads a key, which this
// migration does not auto-detect).
func FinishTopicDelivery(ctx context.Context, q DBTX, id int64, status, lastError string, retryAt *time.Time) error {
	_, err := q.Exec(ctx, `
		UPDATE topic_deliveries SET status = $2, last_error = $3, attempts = attempts + 1,
			next_attempt_at = COALESCE($4, next_attempt_at),
			sent_at = CASE WHEN $2 = 'sent' THEN now() ELSE sent_at END
		WHERE id = $1`, id, status, lastError, retryAt)
	return err
}

// TopicDeliveryStats summarises one notification's topic dispatch (there is
// normally exactly one row — one topic per notification — but the query
// aggregates in case that ever changes).
type TopicDeliveryStats struct {
	Sent          int `json:"sent"`
	Pending       int `json:"pending"`
	Failed        int `json:"failed"`
	ConfigMissing int `json:"config_missing"`
}

func NotificationTopicStats(ctx context.Context, q DBTX, notificationID string) (*TopicDeliveryStats, error) {
	var s TopicDeliveryStats
	err := q.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'sent'),
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'failed'),
			count(*) FILTER (WHERE status = 'config_missing')
		FROM topic_deliveries WHERE notification_id = $1`, notificationID).
		Scan(&s.Sent, &s.Pending, &s.Failed, &s.ConfigMissing)
	return &s, err
}
