package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

const schoolCols = `id, name, code, address, city, state, pincode, contact_name, contact_phone,
	contact_email, working_days, timezone, transport_config, status, created_at, updated_at`

func scanSchool(row pgx.Row) (*models.School, error) {
	var s models.School
	var cfg []byte
	err := row.Scan(&s.ID, &s.Name, &s.Code, &s.Address, &s.City, &s.State, &s.Pincode,
		&s.ContactName, &s.ContactPhone, &s.ContactEmail, &s.WorkingDays, &s.Timezone,
		&cfg, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	s.TransportConfig = json.RawMessage(cfg)
	return &s, nil
}

type SchoolInput struct {
	Name            string
	Code            string
	Address         string
	City            string
	State           string
	Pincode         string
	ContactName     string
	ContactPhone    string
	ContactEmail    string
	WorkingDays     []string
	Timezone        string
	TransportConfig json.RawMessage
}

type SchoolFilter struct {
	Q      string
	Status string
}

func ListSchools(ctx context.Context, q DBTX, f SchoolFilter, p Page) ([]models.School, int, error) {
	const where = `WHERE ($1 = '' OR name ILIKE '%' || $1 || '%' OR code ILIKE '%' || $1 || '%')
		AND ($2 = '' OR status = $2)`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM schools `+where, f.Q, f.Status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `SELECT `+schoolCols+` FROM schools `+where+
		` ORDER BY name LIMIT $3 OFFSET $4`, f.Q, f.Status, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []models.School{}
	for rows.Next() {
		s, err := scanSchool(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *s)
	}
	return out, total, rows.Err()
}

func GetSchool(ctx context.Context, q DBTX, id string) (*models.School, error) {
	return scanSchool(q.QueryRow(ctx, `SELECT `+schoolCols+` FROM schools WHERE id = $1`, id))
}

func CreateSchool(ctx context.Context, q DBTX, in SchoolInput) (*models.School, error) {
	return scanSchool(q.QueryRow(ctx, `
		INSERT INTO schools (name, code, address, city, state, pincode, contact_name, contact_phone,
			contact_email, working_days, timezone, transport_config)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+schoolCols,
		in.Name, in.Code, in.Address, in.City, in.State, in.Pincode, in.ContactName, in.ContactPhone,
		in.ContactEmail, in.WorkingDays, in.Timezone, []byte(in.TransportConfig)))
}

func UpdateSchool(ctx context.Context, q DBTX, id string, in SchoolInput) (*models.School, error) {
	return scanSchool(q.QueryRow(ctx, `
		UPDATE schools SET name = $2, code = $3, address = $4, city = $5, state = $6, pincode = $7,
			contact_name = $8, contact_phone = $9, contact_email = $10, working_days = $11,
			timezone = $12, transport_config = $13
		WHERE id = $1
		RETURNING `+schoolCols,
		id, in.Name, in.Code, in.Address, in.City, in.State, in.Pincode, in.ContactName, in.ContactPhone,
		in.ContactEmail, in.WorkingDays, in.Timezone, []byte(in.TransportConfig)))
}

func SetSchoolStatus(ctx context.Context, q DBTX, id, status string) (*models.School, error) {
	return scanSchool(q.QueryRow(ctx,
		`UPDATE schools SET status = $2 WHERE id = $1 RETURNING `+schoolCols, id, status))
}
