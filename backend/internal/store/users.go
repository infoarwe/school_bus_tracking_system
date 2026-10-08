package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

const userCols = `id, school_id, role, name, email, mobile, status, totp_enabled, last_login_at,
	created_at, updated_at, password_hash, totp_secret`

func scanUser(row pgx.Row) (*models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.SchoolID, &u.Role, &u.Name, &u.Email, &u.Mobile, &u.Status,
		&u.TOTPEnabled, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt, &u.PasswordHash, &u.TOTPSecret)
	if err != nil {
		return nil, mapErr(err)
	}
	return &u, nil
}

func GetUser(ctx context.Context, q DBTX, id string) (*models.User, error) {
	return scanUser(q.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

// GetSchoolUser returns a user only if it belongs to the given school.
func GetSchoolUser(ctx context.Context, q DBTX, schoolID, id string) (*models.User, error) {
	return scanUser(q.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func GetUserByEmail(ctx context.Context, q DBTX, email string) (*models.User, error) {
	return scanUser(q.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE email = $1`, email))
}

func GetUserByMobile(ctx context.Context, q DBTX, mobile string, role models.Role) (*models.User, error) {
	return scanUser(q.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE mobile = $1 AND role = $2`, mobile, role))
}

type UserFilter struct {
	Roles  []models.Role
	Status string
	Q      string
}

func ListSchoolUsers(ctx context.Context, q DBTX, schoolID string, f UserFilter, p Page) ([]models.User, int, error) {
	roles := make([]string, len(f.Roles))
	for i, r := range f.Roles {
		roles[i] = string(r)
	}
	const where = `WHERE school_id = $1 AND role = ANY($2)
		AND ($3 = '' OR status = $3)
		AND ($4 = '' OR name ILIKE '%' || $4 || '%' OR email ILIKE '%' || $4 || '%' OR mobile LIKE '%' || $4 || '%')`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM users `+where,
		schoolID, roles, f.Status, f.Q).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `SELECT `+userCols+` FROM users `+where+
		` ORDER BY name LIMIT $5 OFFSET $6`, schoolID, roles, f.Status, f.Q, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *u)
	}
	return out, total, rows.Err()
}

type UserInput struct {
	SchoolID     *string
	Role         models.Role
	Name         string
	Email        *string
	Mobile       *string
	PasswordHash *string
}

func CreateUser(ctx context.Context, q DBTX, in UserInput) (*models.User, error) {
	return scanUser(q.QueryRow(ctx, `
		INSERT INTO users (school_id, role, name, email, mobile, password_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+userCols,
		in.SchoolID, in.Role, in.Name, in.Email, in.Mobile, in.PasswordHash))
}

func UpdateUserProfile(ctx context.Context, q DBTX, id, name string, email, mobile *string) (*models.User, error) {
	return scanUser(q.QueryRow(ctx, `
		UPDATE users SET name = $2, email = $3, mobile = $4 WHERE id = $1 RETURNING `+userCols,
		id, name, email, mobile))
}

func SetUserStatus(ctx context.Context, q DBTX, id, status string) (*models.User, error) {
	return scanUser(q.QueryRow(ctx,
		`UPDATE users SET status = $2 WHERE id = $1 RETURNING `+userCols, id, status))
}

func SetUserPassword(ctx context.Context, q DBTX, id, hash string) error {
	_, err := q.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, id, hash)
	return err
}

func SetUserTOTPSecret(ctx context.Context, q DBTX, id, secret string) error {
	_, err := q.Exec(ctx, `UPDATE users SET totp_secret = $2, totp_enabled = false WHERE id = $1`, id, secret)
	return err
}

func EnableUserTOTP(ctx context.Context, q DBTX, id string) error {
	_, err := q.Exec(ctx, `UPDATE users SET totp_enabled = true WHERE id = $1`, id)
	return err
}

func TouchUserLogin(ctx context.Context, q DBTX, id string) error {
	_, err := q.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	return err
}
