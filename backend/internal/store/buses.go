package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

const busCols = `id, school_id, vehicle_number, capacity, make_model, gps_device_id, notes, status, created_at, updated_at`

func scanBus(row pgx.Row) (*models.Bus, error) {
	var b models.Bus
	err := row.Scan(&b.ID, &b.SchoolID, &b.VehicleNumber, &b.Capacity, &b.MakeModel, &b.GPSDeviceID,
		&b.Notes, &b.Status, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &b, nil
}

type BusInput struct {
	VehicleNumber string
	Capacity      int
	MakeModel     string
	GPSDeviceID   string
	Notes         string
}

func ListBuses(ctx context.Context, q DBTX, schoolID string, f ListFilter, p Page) ([]models.Bus, int, error) {
	const where = ` WHERE school_id = $1 AND ($2 = '' OR status = $2)
		AND ($3 = '' OR vehicle_number ILIKE '%' || $3 || '%' OR make_model ILIKE '%' || $3 || '%')`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM buses`+where, schoolID, f.Status, f.Q).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `SELECT `+busCols+` FROM buses`+where+` ORDER BY vehicle_number LIMIT $4 OFFSET $5`,
		schoolID, f.Status, f.Q, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Bus{}
	for rows.Next() {
		b, err := scanBus(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *b)
	}
	return out, total, rows.Err()
}

func GetBus(ctx context.Context, q DBTX, schoolID, id string) (*models.Bus, error) {
	return scanBus(q.QueryRow(ctx, `SELECT `+busCols+` FROM buses WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func CreateBus(ctx context.Context, q DBTX, schoolID string, in BusInput) (*models.Bus, error) {
	return scanBus(q.QueryRow(ctx, `
		INSERT INTO buses (school_id, vehicle_number, capacity, make_model, gps_device_id, notes)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+busCols,
		schoolID, in.VehicleNumber, in.Capacity, in.MakeModel, in.GPSDeviceID, in.Notes))
}

func UpdateBus(ctx context.Context, q DBTX, schoolID, id string, in BusInput) (*models.Bus, error) {
	return scanBus(q.QueryRow(ctx, `
		UPDATE buses SET vehicle_number = $3, capacity = $4, make_model = $5, gps_device_id = $6, notes = $7
		WHERE id = $1 AND school_id = $2 RETURNING `+busCols,
		id, schoolID, in.VehicleNumber, in.Capacity, in.MakeModel, in.GPSDeviceID, in.Notes))
}

func SetBusStatus(ctx context.Context, q DBTX, schoolID, id, status string) (*models.Bus, error) {
	return scanBus(q.QueryRow(ctx,
		`UPDATE buses SET status = $3 WHERE id = $1 AND school_id = $2 RETURNING `+busCols, id, schoolID, status))
}
