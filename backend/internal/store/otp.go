package store

import (
	"context"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

type OTPStats struct {
	SentLastHour int
	LastSentAt   *time.Time
}

func GetOTPStats(ctx context.Context, q DBTX, mobile string, role models.Role) (*OTPStats, error) {
	var st OTPStats
	err := q.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE created_at > now() - interval '1 hour'), max(created_at)
		FROM otp_codes WHERE mobile = $1 AND role = $2`, mobile, role).Scan(&st.SentLastHour, &st.LastSentAt)
	return &st, err
}

// CreateOTP stores a new code and invalidates any earlier unused ones.
func CreateOTP(ctx context.Context, q DBTX, mobile string, role models.Role, codeHash string, expiresAt time.Time) error {
	if _, err := q.Exec(ctx, `UPDATE otp_codes SET consumed_at = now()
		WHERE mobile = $1 AND role = $2 AND consumed_at IS NULL`, mobile, role); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `INSERT INTO otp_codes (mobile, role, code_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		mobile, role, codeHash, expiresAt)
	return err
}

type ActiveOTP struct {
	ID       string
	CodeHash string
	Attempts int
}

func GetActiveOTP(ctx context.Context, q DBTX, mobile string, role models.Role) (*ActiveOTP, error) {
	var o ActiveOTP
	err := q.QueryRow(ctx, `
		SELECT id, code_hash, attempts FROM otp_codes
		WHERE mobile = $1 AND role = $2 AND consumed_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC LIMIT 1`, mobile, role).Scan(&o.ID, &o.CodeHash, &o.Attempts)
	if err != nil {
		return nil, mapErr(err)
	}
	return &o, nil
}

func IncrementOTPAttempts(ctx context.Context, q DBTX, id string) error {
	_, err := q.Exec(ctx, `UPDATE otp_codes SET attempts = attempts + 1 WHERE id = $1`, id)
	return err
}

func ConsumeOTP(ctx context.Context, q DBTX, id string) error {
	_, err := q.Exec(ctx, `UPDATE otp_codes SET consumed_at = now() WHERE id = $1`, id)
	return err
}
