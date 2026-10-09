package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Announcement struct {
	ID             string         `json:"id"`
	SchoolID       string         `json:"school_id"`
	Category       string         `json:"category"`
	Target         string         `json:"target"` // school | route
	RouteID        *string        `json:"route_id"`
	RouteCode      *string        `json:"route_code"`
	Title          string         `json:"title"`
	Message        string         `json:"message"`
	AttachmentURL  string         `json:"attachment_url"`
	ScheduledAt    *time.Time     `json:"scheduled_at"`
	Status         string         `json:"status"` // scheduled | sending | sent | cancelled
	SentAt         *time.Time     `json:"sent_at"`
	NotificationID *string        `json:"notification_id"`
	CreatedByName  *string        `json:"created_by_name"`
	CreatedAt      time.Time      `json:"created_at"`
	Stats          *DeliveryStats `json:"stats"` // once sent
}

const announcementSelect = `SELECT a.id, a.school_id, a.category, a.target, a.route_id, r.code, a.title, a.message,
	a.attachment_url, a.scheduled_at, a.status, a.sent_at, a.notification_id, u.name, a.created_at
	FROM announcements a LEFT JOIN routes r ON r.id = a.route_id LEFT JOIN users u ON u.id = a.created_by`

func scanAnnouncement(row pgx.Row) (*Announcement, error) {
	var a Announcement
	err := row.Scan(&a.ID, &a.SchoolID, &a.Category, &a.Target, &a.RouteID, &a.RouteCode, &a.Title, &a.Message,
		&a.AttachmentURL, &a.ScheduledAt, &a.Status, &a.SentAt, &a.NotificationID, &a.CreatedByName, &a.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &a, nil
}

type AnnouncementInput struct {
	Category, Target, Title, Message, AttachmentURL, CreatedBy string
	RouteID                                                    *string
	ScheduledAt                                                *time.Time
}

func InsertAnnouncement(ctx context.Context, q DBTX, schoolID string, in AnnouncementInput) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO announcements (school_id, category, target, route_id, title, message, attachment_url, scheduled_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		schoolID, in.Category, in.Target, in.RouteID, in.Title, in.Message, in.AttachmentURL, in.ScheduledAt, in.CreatedBy).Scan(&id)
	return id, mapErr(err)
}

func GetAnnouncement(ctx context.Context, q DBTX, schoolID, id string) (*Announcement, error) {
	return scanAnnouncement(q.QueryRow(ctx, announcementSelect+` WHERE a.id = $1 AND a.school_id = $2`, id, schoolID))
}

func ListAnnouncements(ctx context.Context, q DBTX, schoolID, status string, p Page) ([]Announcement, int, error) {
	const where = ` WHERE a.school_id = $1 AND ($2 = '' OR a.status = $2)`
	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM announcements a`+where, schoolID, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, announcementSelect+where+` ORDER BY COALESCE(a.sent_at, a.scheduled_at, a.created_at) DESC
		LIMIT $3 OFFSET $4`, schoolID, status, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		a, err := scanAnnouncement(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

func CancelAnnouncement(ctx context.Context, q DBTX, schoolID, id string) error {
	tag, err := q.Exec(ctx, `UPDATE announcements SET status = 'cancelled' WHERE id = $1 AND school_id = $2 AND status = 'scheduled'`, id, schoolID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStatusChanged
	}
	return nil
}

// ClaimDueAnnouncements marks announcements whose time has come as "sending"
// and returns them. Safe across API instances (SKIP LOCKED).
func ClaimDueAnnouncements(ctx context.Context, q DBTX, limit int) ([]Announcement, error) {
	rows, err := q.Query(ctx, `
		UPDATE announcements SET status = 'sending'
		WHERE id IN (SELECT id FROM announcements WHERE status = 'scheduled' AND COALESCE(scheduled_at, created_at) <= now()
			ORDER BY scheduled_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id, school_id`, limit)
	if err != nil {
		return nil, err
	}
	var ids [][2]string
	for rows.Next() {
		var id, school string
		if err := rows.Scan(&id, &school); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, [2]string{id, school})
	}
	rows.Close()
	out := []Announcement{}
	for _, x := range ids {
		a, err := GetAnnouncement(ctx, q, x[1], x[0])
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, nil
}

func MarkAnnouncementSent(ctx context.Context, q DBTX, id string, notificationID *string) error {
	_, err := q.Exec(ctx, `UPDATE announcements SET status = 'sent', sent_at = now(), notification_id = $2 WHERE id = $1`, id, notificationID)
	return err
}
