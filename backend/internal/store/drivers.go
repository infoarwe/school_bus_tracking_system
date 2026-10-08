package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

const driverSelect = `SELECT d.id, d.school_id, d.user_id, u.name, u.mobile, d.license_number,
	d.license_expiry::text, d.address, d.emergency_contact_name, d.emergency_contact_phone,
	d.id_proof_type, d.id_proof_number, d.notes, d.status, u.last_login_at, d.created_at, d.updated_at
	FROM drivers d JOIN users u ON u.id = d.user_id`

func scanDriver(row pgx.Row) (*models.Driver, error) {
	var d models.Driver
	err := row.Scan(&d.ID, &d.SchoolID, &d.UserID, &d.Name, &d.Mobile, &d.LicenseNumber,
		&d.LicenseExpiry, &d.Address, &d.EmergencyContactName, &d.EmergencyContactPhone,
		&d.IDProofType, &d.IDProofNumber, &d.Notes, &d.Status, &d.LastLoginAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &d, nil
}

// DriverProfile is everything on the drivers row that an admin edits.
type DriverProfile struct {
	LicenseNumber         string
	LicenseExpiry         *string
	Address               string
	EmergencyContactName  string
	EmergencyContactPhone string
	IDProofType           string
	IDProofNumber         string
	Notes                 string
}

type ListFilter struct {
	Q      string
	Status string
}

func ListDrivers(ctx context.Context, q DBTX, schoolID string, f ListFilter, p Page) ([]models.Driver, int, error) {
	const where = ` WHERE d.school_id = $1 AND ($2 = '' OR d.status = $2)
		AND ($3 = '' OR u.name ILIKE '%' || $3 || '%' OR u.mobile LIKE '%' || $3 || '%'
			OR d.license_number ILIKE '%' || $3 || '%')`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM drivers d JOIN users u ON u.id = d.user_id`+where,
		schoolID, f.Status, f.Q).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, driverSelect+where+` ORDER BY u.name LIMIT $4 OFFSET $5`,
		schoolID, f.Status, f.Q, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Driver{}
	for rows.Next() {
		d, err := scanDriver(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *d)
	}
	return out, total, rows.Err()
}

func GetDriver(ctx context.Context, q DBTX, schoolID, id string) (*models.Driver, error) {
	return scanDriver(q.QueryRow(ctx, driverSelect+` WHERE d.id = $1 AND d.school_id = $2`, id, schoolID))
}

func CreateDriver(ctx context.Context, q DBTX, schoolID, userID string, p DriverProfile) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO drivers (school_id, user_id, license_number, license_expiry, address,
			emergency_contact_name, emergency_contact_phone, id_proof_type, id_proof_number, notes)
		VALUES ($1, $2, $3, $4::date, $5, $6, $7, $8, $9, $10) RETURNING id`,
		schoolID, userID, p.LicenseNumber, p.LicenseExpiry, p.Address, p.EmergencyContactName,
		p.EmergencyContactPhone, p.IDProofType, p.IDProofNumber, p.Notes).Scan(&id)
	return id, mapErr(err)
}

func UpdateDriver(ctx context.Context, q DBTX, schoolID, id string, p DriverProfile) error {
	tag, err := q.Exec(ctx, `
		UPDATE drivers SET license_number = $3, license_expiry = $4::date, address = $5,
			emergency_contact_name = $6, emergency_contact_phone = $7, id_proof_type = $8,
			id_proof_number = $9, notes = $10
		WHERE id = $1 AND school_id = $2`,
		id, schoolID, p.LicenseNumber, p.LicenseExpiry, p.Address, p.EmergencyContactName,
		p.EmergencyContactPhone, p.IDProofType, p.IDProofNumber, p.Notes)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func SetDriverStatus(ctx context.Context, q DBTX, schoolID, id, status string) error {
	tag, err := q.Exec(ctx, `UPDATE drivers SET status = $3 WHERE id = $1 AND school_id = $2`, id, schoolID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
