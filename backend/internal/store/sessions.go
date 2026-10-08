package store

import (
	"context"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

type NewSession struct {
	UserID           string
	RefreshTokenHash string
	DeviceName       string
	UserAgent        string
	IP               string
	ExpiresAt        time.Time
}

func CreateSession(ctx context.Context, q DBTX, s NewSession) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO user_sessions (user_id, refresh_token_hash, device_name, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		s.UserID, s.RefreshTokenHash, s.DeviceName, s.UserAgent, s.IP, s.ExpiresAt).Scan(&id)
	return id, mapErr(err)
}

// RotateRefreshToken swaps the stored refresh token for a new one, but only if
// the old one is still valid. It returns the session and user IDs.
func RotateRefreshToken(ctx context.Context, q DBTX, oldHash, newHash string, expiresAt time.Time) (sessionID, userID string, err error) {
	err = q.QueryRow(ctx, `
		UPDATE user_sessions
		SET refresh_token_hash = $2, expires_at = $3, last_used_at = now()
		WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		RETURNING id, user_id`, oldHash, newHash, expiresAt).Scan(&sessionID, &userID)
	return sessionID, userID, mapErr(err)
}

func RevokeSession(ctx context.Context, q DBTX, userID, sessionID string) error {
	tag, err := q.Exec(ctx, `UPDATE user_sessions SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, sessionID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func RevokeAllSessions(ctx context.Context, q DBTX, userID string) error {
	_, err := q.Exec(ctx, `UPDATE user_sessions SET revoked_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func ListActiveSessions(ctx context.Context, q DBTX, userID, currentSessionID string) ([]models.Session, error) {
	rows, err := q.Query(ctx, `
		SELECT id, device_name, user_agent, ip, created_at, last_used_at, expires_at
		FROM user_sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY last_used_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Session{}
	for rows.Next() {
		var s models.Session
		if err := rows.Scan(&s.ID, &s.DeviceName, &s.UserAgent, &s.IP, &s.CreatedAt, &s.LastUsedAt, &s.ExpiresAt); err != nil {
			return nil, err
		}
		s.Current = s.ID == currentSessionID
		out = append(out, s)
	}
	return out, rows.Err()
}

// SessionState is what the auth middleware re-checks on every request.
type SessionState struct {
	Role         models.Role
	SchoolID     *string
	UserStatus   string
	SchoolStatus *string
	TOTPEnabled  bool
}

// GetSessionState returns ErrNotFound if the session is revoked, expired, or not the user's.
func GetSessionState(ctx context.Context, q DBTX, sessionID, userID string) (*SessionState, error) {
	var st SessionState
	err := q.QueryRow(ctx, `
		SELECT u.role, u.school_id, u.status, sc.status, u.totp_enabled
		FROM user_sessions s
		JOIN users u ON u.id = s.user_id
		LEFT JOIN schools sc ON sc.id = u.school_id
		WHERE s.id = $1 AND s.user_id = $2 AND s.revoked_at IS NULL AND s.expires_at > now()`,
		sessionID, userID).Scan(&st.Role, &st.SchoolID, &st.UserStatus, &st.SchoolStatus, &st.TOTPEnabled)
	if err != nil {
		return nil, mapErr(err)
	}
	return &st, nil
}
