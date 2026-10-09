package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// student_count: students on the route who have a stop for this trip type
// (pickup stop for Morning Pickup, drop stop for Evening Drop).
const tripSelect = `SELECT t.id, t.school_id, t.trip_date::text, t.trip_type, t.status,
	r.id, r.code, r.name, (SELECT count(*) FROM stops s WHERE s.route_id = r.id),
	b.id, b.vehicle_number, b.capacity,
	d.id, du.name, du.mobile,
	(SELECT count(*) FROM student_assignments sa JOIN students st ON st.id = sa.student_id
		WHERE sa.route_id = t.route_id AND sa.ended_at IS NULL AND st.status = 'active'
		AND CASE WHEN t.trip_type = 'morning_pickup' THEN sa.pickup_stop_id ELSE sa.drop_stop_id END IS NOT NULL),
	t.notes, t.cancel_reason, t.confirmed_at, t.started_at, t.ended_at, t.cancelled_at, t.created_at, t.updated_at
	FROM trips t
	JOIN routes r ON r.id = t.route_id
	JOIN buses b ON b.id = t.bus_id
	JOIN drivers d ON d.id = t.driver_id
	JOIN users du ON du.id = d.user_id`

func scanTrip(row pgx.Row) (*models.Trip, error) {
	var t models.Trip
	err := row.Scan(&t.ID, &t.SchoolID, &t.TripDate, &t.TripType, &t.Status,
		&t.Route.ID, &t.Route.Code, &t.Route.Name, &t.Route.StopCount,
		&t.Bus.ID, &t.Bus.VehicleNumber, &t.Bus.Capacity,
		&t.Driver.ID, &t.Driver.Name, &t.Driver.Mobile,
		&t.StudentCount, &t.Notes, &t.CancelReason,
		&t.ConfirmedAt, &t.StartedAt, &t.EndedAt, &t.CancelledAt, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &t, nil
}

func scanTrips(rows pgx.Rows) ([]models.Trip, error) {
	defer rows.Close()
	out := []models.Trip{}
	for rows.Next() {
		t, err := scanTrip(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// SchoolToday is today's date in the school's time zone (YYYY-MM-DD).
func SchoolToday(ctx context.Context, q DBTX, schoolID string) (string, error) {
	var d string
	err := q.QueryRow(ctx, `SELECT (now() AT TIME ZONE timezone)::date::text FROM schools WHERE id = $1`, schoolID).Scan(&d)
	return d, mapErr(err)
}

type TripFilter struct {
	Date     string
	From, To string // inclusive date range, used when Date is empty
	TripType string
	Status   string
	RouteID  string
	DriverID string
	BusID    string
}

func ListTrips(ctx context.Context, q DBTX, schoolID string, f TripFilter, p Page) ([]models.Trip, int, error) {
	const where = ` WHERE t.school_id = $1
		AND ($2 = '' OR t.trip_date = $2::date)
		AND ($3 = '' OR t.trip_date >= $3::date)
		AND ($4 = '' OR t.trip_date <= $4::date)
		AND ($5 = '' OR t.trip_type = $5)
		AND ($6 = '' OR t.status = $6)
		AND ($7 = '' OR t.route_id::text = $7)
		AND ($8 = '' OR t.driver_id::text = $8)
		AND ($9 = '' OR t.bus_id::text = $9)`
	args := []any{schoolID, f.Date, f.From, f.To, f.TripType, f.Status, f.RouteID, f.DriverID, f.BusID}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM trips t`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, tripSelect+where+
		` ORDER BY t.trip_date DESC, t.trip_type DESC, r.code LIMIT $10 OFFSET $11`, append(args, p.PageSize, p.Offset())...)
	if err != nil {
		return nil, 0, err
	}
	trips, err := scanTrips(rows)
	return trips, total, err
}

func GetTrip(ctx context.Context, q DBTX, schoolID, id string) (*models.Trip, error) {
	return scanTrip(q.QueryRow(ctx, tripSelect+` WHERE t.id = $1 AND t.school_id = $2`, id, schoolID))
}

// GetDriverTrip returns a trip only if it is assigned to the driver.
func GetDriverTrip(ctx context.Context, q DBTX, driverID, id string) (*models.Trip, error) {
	return scanTrip(q.QueryRow(ctx, tripSelect+` WHERE t.id = $1 AND t.driver_id = $2`, id, driverID))
}

// DriverTripsOn lists a driver's trips on a date, Morning Pickup first. Cancelled trips are left out.
func DriverTripsOn(ctx context.Context, q DBTX, driverID, date string) ([]models.Trip, error) {
	rows, err := q.Query(ctx, tripSelect+` WHERE t.driver_id = $1 AND t.trip_date = $2::date AND t.status <> 'cancelled'
		ORDER BY t.trip_type DESC`, driverID, date)
	if err != nil {
		return nil, err
	}
	return scanTrips(rows)
}

// RouteTripsOn lists the trips of a route on a date (for the Parent app), including cancelled ones.
func RouteTripsOn(ctx context.Context, q DBTX, routeID, date string) ([]models.Trip, error) {
	rows, err := q.Query(ctx, tripSelect+` WHERE t.route_id = $1 AND t.trip_date = $2::date
		ORDER BY t.trip_type DESC, t.created_at`, routeID, date)
	if err != nil {
		return nil, err
	}
	return scanTrips(rows)
}

type TripInput struct {
	TripDate string
	TripType string
	RouteID  string
	BusID    string
	DriverID string
	Notes    string
}

func CreateTrip(ctx context.Context, q DBTX, schoolID, createdBy string, in TripInput) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO trips (school_id, trip_date, trip_type, route_id, bus_id, driver_id, notes, created_by)
		VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8) RETURNING id`,
		schoolID, in.TripDate, in.TripType, in.RouteID, in.BusID, in.DriverID, in.Notes, createdBy).Scan(&id)
	return id, mapErr(err)
}

// UpdateTripAssignment changes route/bus/driver/notes; status is changed separately.
func UpdateTripAssignment(ctx context.Context, q DBTX, schoolID, id string, in TripInput) error {
	tag, err := q.Exec(ctx, `
		UPDATE trips SET trip_date = $3::date, trip_type = $4, route_id = $5, bus_id = $6, driver_id = $7, notes = $8
		WHERE id = $1 AND school_id = $2`,
		id, schoolID, in.TripDate, in.TripType, in.RouteID, in.BusID, in.DriverID, in.Notes)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrStatusChanged: the trip's status changed since it was read (concurrent update).
var ErrStatusChanged = errors.New("trip status changed; reload and try again")

// SetTripStatus moves a trip from one status to another, stamping the matching
// timestamp. It fails with ErrStatusChanged if the trip is no longer in `from`.
func SetTripStatus(ctx context.Context, q DBTX, id, from, to, cancelReason string) error {
	tag, err := q.Exec(ctx, `
		UPDATE trips SET status = $3,
			confirmed_at = CASE WHEN $3 = 'confirmed' THEN now() WHEN $3 = 'scheduled' THEN NULL ELSE confirmed_at END,
			started_at   = CASE WHEN $3 = 'started'   THEN now() ELSE started_at END,
			ended_at     = CASE WHEN $3 = 'completed' THEN now() ELSE ended_at END,
			cancelled_at = CASE WHEN $3 = 'cancelled' THEN now() ELSE cancelled_at END,
			cancel_reason = CASE WHEN $3 = 'cancelled' THEN $4 ELSE cancel_reason END
		WHERE id = $1 AND status = $2`, id, from, to, cancelReason)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrStatusChanged
	}
	return nil
}

type TripHistoryEntry struct {
	TripID, SchoolID string
	From             *string
	To               string
	ByUserID         *string
	ByRole           string
	Reason           string
}

func InsertTripHistory(ctx context.Context, q DBTX, e TripHistoryEntry) error {
	_, err := q.Exec(ctx, `
		INSERT INTO trip_status_history (trip_id, school_id, from_status, to_status, changed_by, changed_by_role, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, e.TripID, e.SchoolID, e.From, e.To, e.ByUserID, e.ByRole, e.Reason)
	return err
}

func TripHistory(ctx context.Context, q DBTX, tripID string) ([]models.TripStatusChange, error) {
	rows, err := q.Query(ctx, `
		SELECT h.from_status, h.to_status, u.name, h.changed_by_role, h.reason, h.created_at
		FROM trip_status_history h LEFT JOIN users u ON u.id = h.changed_by
		WHERE h.trip_id = $1 ORDER BY h.created_at, h.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.TripStatusChange{}
	for rows.Next() {
		var c models.TripStatusChange
		if err := rows.Scan(&c.FromStatus, &c.ToStatus, &c.ChangedByName, &c.ChangedByRole, &c.Reason, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// StopStudentCounts returns, per stop, how many students board (Morning Pickup)
// or get off (Evening Drop) there. Counts only: drivers never see student details.
func StopStudentCounts(ctx context.Context, q DBTX, routeID, tripType string) (map[string]int, error) {
	rows, err := q.Query(ctx, `
		SELECT CASE WHEN $2 = 'morning_pickup' THEN sa.pickup_stop_id ELSE sa.drop_stop_id END AS stop_id, count(*)
		FROM student_assignments sa JOIN students st ON st.id = sa.student_id
		WHERE sa.route_id = $1 AND sa.ended_at IS NULL AND st.status = 'active'
		GROUP BY 1 HAVING CASE WHEN $2 = 'morning_pickup' THEN sa.pickup_stop_id ELSE sa.drop_stop_id END IS NOT NULL`,
		routeID, tripType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// TripRefs is what trip validation needs to know about the chosen route, bus and driver.
type TripRefs struct {
	RouteFound, BusFound, DriverFound    bool
	RouteStatus, BusStatus, DriverStatus string
	SupportsPickup, SupportsDrop         bool
	StopCount                            int
}

func GetTripRefs(ctx context.Context, q DBTX, schoolID, routeID, busID, driverID string) (*TripRefs, error) {
	var t TripRefs
	err := q.QueryRow(ctx, `SELECT status, supports_pickup, supports_drop,
		(SELECT count(*) FROM stops WHERE route_id = routes.id) FROM routes WHERE id = $1 AND school_id = $2`,
		routeID, schoolID).Scan(&t.RouteStatus, &t.SupportsPickup, &t.SupportsDrop, &t.StopCount)
	if err == nil {
		t.RouteFound = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	err = q.QueryRow(ctx, `SELECT status FROM buses WHERE id = $1 AND school_id = $2`, busID, schoolID).Scan(&t.BusStatus)
	if err == nil {
		t.BusFound = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	err = q.QueryRow(ctx, `SELECT status FROM drivers WHERE id = $1 AND school_id = $2`, driverID, schoolID).Scan(&t.DriverStatus)
	if err == nil {
		t.DriverFound = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return &t, nil
}

// TripsOnDate returns the non-cancelled trips of a school on a date (copy-day source).
func TripsOnDate(ctx context.Context, q DBTX, schoolID, date string, tripTypes []string) ([]models.Trip, error) {
	rows, err := q.Query(ctx, tripSelect+` WHERE t.school_id = $1 AND t.trip_date = $2::date
		AND t.status <> 'cancelled' AND t.trip_type = ANY($3) ORDER BY t.trip_type DESC, r.code`, schoolID, date, tripTypes)
	if err != nil {
		return nil, err
	}
	return scanTrips(rows)
}
