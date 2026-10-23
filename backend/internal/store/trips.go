package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// student_count: students on the route who have a stop for this trip type
// (pickup stop for Morning Pickup, drop stop for Evening Drop/Special Trip). A
// routeless Special Trip/Outing has no route, so this is simply 0 for it (the
// subquery's WHERE sa.route_id = t.route_id never matches a NULL route_id).
// routes/buses/drivers are all LEFT JOINs: a Special Trip/Outing may have no
// route, and any trip may have no bus and/or no driver (Unassign), so every
// r.*/b.*/d.*/du.* column below is nullable.
const tripSelect = `SELECT t.id, t.school_id, t.trip_date::text, t.trip_type, t.status,
	r.id, r.code, r.name, (SELECT count(*) FROM stops s WHERE s.route_id = r.id),
	b.id, b.vehicle_number, b.capacity,
	d.id, du.name, du.mobile,
	t.scheduled_start_at, t.scheduled_end_at,
	(SELECT count(*) FROM student_assignments sa JOIN students st ON st.id = sa.student_id
		WHERE sa.route_id = t.route_id AND sa.ended_at IS NULL AND st.status = 'active'
		AND CASE WHEN t.trip_type = 'morning_pickup' THEN sa.pickup_stop_id ELSE sa.drop_stop_id END IS NOT NULL),
	t.title, t.notify_parents, t.notes, t.cancel_reason, t.confirmed_at, t.started_at, t.ended_at, t.cancelled_at,
	t.created_at, t.updated_at,
	CASE WHEN d.user_id IS NOT NULL AND t.created_by = d.user_id THEN 'driver' ELSE 'staff' END
	FROM trips t
	LEFT JOIN routes r ON r.id = t.route_id
	LEFT JOIN buses b ON b.id = t.bus_id
	LEFT JOIN drivers d ON d.id = t.driver_id
	LEFT JOIN users du ON du.id = d.user_id`

func scanTrip(row pgx.Row) (*models.Trip, error) {
	var t models.Trip
	var routeID, routeCode, routeName sql.NullString
	var routeStopCount sql.NullInt32
	var busID, busVehicleNumber sql.NullString
	var busCapacity sql.NullInt32
	var driverID, driverName, driverMobile sql.NullString
	err := row.Scan(&t.ID, &t.SchoolID, &t.TripDate, &t.TripType, &t.Status,
		&routeID, &routeCode, &routeName, &routeStopCount,
		&busID, &busVehicleNumber, &busCapacity,
		&driverID, &driverName, &driverMobile,
		&t.ScheduledStartAt, &t.ScheduledEndAt,
		&t.StudentCount, &t.Title, &t.NotifyParents, &t.Notes, &t.CancelReason,
		&t.ConfirmedAt, &t.StartedAt, &t.EndedAt, &t.CancelledAt, &t.CreatedAt, &t.UpdatedAt, &t.AssignedBy)
	if err != nil {
		return nil, mapErr(err)
	}
	if routeID.Valid {
		t.Route = &models.TripRoute{ID: routeID.String, Code: routeCode.String, Name: routeName.String, StopCount: int(routeStopCount.Int32)}
	}
	if busID.Valid {
		t.Bus = &models.TripBus{ID: busID.String, VehicleNumber: busVehicleNumber.String, Capacity: int(busCapacity.Int32)}
	}
	if driverID.Valid {
		t.Driver = &models.TripDriver{ID: driverID.String, Name: driverName.String, Mobile: driverMobile.String}
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

// SchoolTimezone is the school's IANA time zone name (e.g. "Asia/Kolkata"), used
// to combine a trip's scheduled HH:MM wall-clock times with its calendar
// trip_date into a real instant (see handlers/trips.go's validateTrip).
func SchoolTimezone(ctx context.Context, q DBTX, schoolID string) (string, error) {
	var tz string
	err := q.QueryRow(ctx, `SELECT timezone FROM schools WHERE id = $1`, schoolID).Scan(&tz)
	return tz, mapErr(err)
}

type TripFilter struct {
	Date     string
	From, To string // inclusive date range, used when Date is empty
	TripType string
	Status   string
	RouteID  string
	DriverID string
	BusID    string
	// Ascending lists the earliest date first (upcoming trips); default is newest first.
	Ascending bool
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
	order := ` ORDER BY t.trip_date DESC, t.trip_type DESC, r.code`
	if f.Ascending {
		order = ` ORDER BY t.trip_date, t.trip_type DESC, r.code`
	}
	rows, err := q.Query(ctx, tripSelect+where+order+` LIMIT $10 OFFSET $11`, append(args, p.PageSize, p.Offset())...)
	if err != nil {
		return nil, 0, err
	}
	trips, err := scanTrips(rows)
	return trips, total, err
}

// bulkFilterWhere is the WHERE clause shared by BulkUnassignPreview and
// BulkUnassignResolveIDs — the same filter dimensions as TripFilter/ListTrips
// (Date/From/To/TripType/RouteID/DriverID/BusID), always excluding cancelled
// trips (there is nothing to unassign on one), with no JOIN needed since
// every column is on trips itself.
const bulkFilterWhere = ` WHERE school_id = $1 AND status <> 'cancelled'
	AND ($2 = '' OR trip_date = $2::date)
	AND ($3 = '' OR trip_date >= $3::date)
	AND ($4 = '' OR trip_date <= $4::date)
	AND ($5 = '' OR trip_type = $5)
	AND ($6 = '' OR route_id::text = $6)
	AND ($7 = '' OR driver_id::text = $7)
	AND ($8 = '' OR bus_id::text = $8)`

func bulkFilterArgs(schoolID string, f TripFilter) []any {
	return []any{schoolID, f.Date, f.From, f.To, f.TripType, f.RouteID, f.DriverID, f.BusID}
}

// BulkUnassignPreview counts how many of a school's non-cancelled trips match
// a filter, split into eligible (scheduled/confirmed — what the real
// operation would act on) and ineligible (started/completed — would be
// skipped with a reason), and the earliest/latest date among all of them.
// This earliest/latest span is what lets the UI auto-detect "the applicable
// assignment period" directly from a route/driver/bus/trip-type selection
// alone, with no manual date entry: it is computed purely from the actual
// matching trip records, never derived from today's date or a route's
// creation date. Status is deliberately not part of the WHERE filter itself
// (eligibility is decided per-trip by the actual operation, not by this
// preview), so total may exceed eligible; that's expected, not a bug, and
// the real per-trip results after confirming say so explicitly.
func BulkUnassignPreview(ctx context.Context, q DBTX, schoolID string, f TripFilter) (total, eligible, ineligible int, earliest, latest string, err error) {
	var e, l sql.NullString
	err = q.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE status IN ('scheduled', 'confirmed')),
		count(*) FILTER (WHERE status NOT IN ('scheduled', 'confirmed')),
		min(trip_date)::text, max(trip_date)::text
		FROM trips`+bulkFilterWhere,
		bulkFilterArgs(schoolID, f)...).Scan(&total, &eligible, &ineligible, &e, &l)
	return total, eligible, ineligible, e.String, l.String, err
}

// BulkUnassignResolveIDs is the same filter, returning the matching trip IDs
// themselves (oldest date first, for a deterministic, readable results
// order), capped at limit+1 so the caller can detect "too many matching
// trips" and refuse rather than silently processing only a subset.
func BulkUnassignResolveIDs(ctx context.Context, q DBTX, schoolID string, f TripFilter, limit int) ([]string, error) {
	args := append(bulkFilterArgs(schoolID, f), limit+1)
	rows, err := q.Query(ctx, `SELECT id FROM trips`+bulkFilterWhere+` ORDER BY trip_date, trip_type LIMIT $9`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func GetTrip(ctx context.Context, q DBTX, schoolID, id string) (*models.Trip, error) {
	return scanTrip(q.QueryRow(ctx, tripSelect+` WHERE t.id = $1 AND t.school_id = $2`, id, schoolID))
}

// GetDriverTrip returns a trip only if it is assigned to the driver.
func GetDriverTrip(ctx context.Context, q DBTX, driverID, id string) (*models.Trip, error) {
	return scanTrip(q.QueryRow(ctx, tripSelect+` WHERE t.id = $1 AND t.driver_id = $2`, id, driverID))
}

// DriverTripsOn lists a driver's trips on a date, Morning Pickup first, then by
// scheduled start time (so Trip 1/2/3 on the same bus+driver show in the order
// the driver will actually run them) — a trip with no scheduled window (legacy,
// or materialized from a permanent assignment) sorts after any that do, rather
// than before everything by accident of a NULL comparing least. Cancelled trips
// are left out.
func DriverTripsOn(ctx context.Context, q DBTX, driverID, date string) ([]models.Trip, error) {
	rows, err := q.Query(ctx, tripSelect+` WHERE t.driver_id = $1 AND t.trip_date = $2::date AND t.status <> 'cancelled'
		ORDER BY t.trip_type DESC, t.scheduled_start_at NULLS LAST`, driverID, date)
	if err != nil {
		return nil, err
	}
	return scanTrips(rows)
}

// RouteTripsOn lists the trips of a route on a date (for the Parent app),
// including cancelled ones, ordered by scheduled start time within each trip
// type (falling back to creation order for a legacy trip with no scheduled
// window, so it still appears deterministically rather than jumping around).
func RouteTripsOn(ctx context.Context, q DBTX, routeID, date string) ([]models.Trip, error) {
	rows, err := q.Query(ctx, tripSelect+` WHERE t.route_id = $1 AND t.trip_date = $2::date
		ORDER BY t.trip_type DESC, t.scheduled_start_at NULLS LAST, t.created_at`, routeID, date)
	if err != nil {
		return nil, err
	}
	return scanTrips(rows)
}

type TripInput struct {
	TripDate string
	TripType string
	// RouteID is nil for a routeless Special Trip/Outing; required for the other two types.
	RouteID       *string
	BusID         string
	DriverID      string
	Notes         string
	Title         string // Special Trip/Outing name; ignored for the other two types
	NotifyParents bool
	// ScheduledStartAt/ScheduledEndAt: this trip's own planned window, already
	// resolved to UTC (handlers/trips.go combines the request's HH:MM wall-clock
	// times with trip_date in the school's timezone). Nil when not given — the
	// trip then has no overlap protection (trips_{route,bus,driver}_no_overlap
	// only apply when both are set), matching a pre-this-feature trip exactly.
	ScheduledStartAt *time.Time
	ScheduledEndAt   *time.Time
}

func CreateTrip(ctx context.Context, q DBTX, schoolID, createdBy string, in TripInput) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO trips (school_id, trip_date, trip_type, route_id, bus_id, driver_id, notes, title, notify_parents,
			scheduled_start_at, scheduled_end_at, created_by)
		VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
		schoolID, in.TripDate, in.TripType, in.RouteID, in.BusID, in.DriverID, in.Notes, in.Title, in.NotifyParents,
		in.ScheduledStartAt, in.ScheduledEndAt, createdBy).Scan(&id)
	return id, mapErr(err)
}

// UpdateTripAssignment changes route/bus/driver/notes/title/notify_parents; status is changed separately.
func UpdateTripAssignment(ctx context.Context, q DBTX, schoolID, id string, in TripInput) error {
	tag, err := q.Exec(ctx, `
		UPDATE trips SET trip_date = $3::date, trip_type = $4, route_id = $5, bus_id = $6, driver_id = $7,
			notes = $8, title = $9, notify_parents = $10, scheduled_start_at = $11, scheduled_end_at = $12
		WHERE id = $1 AND school_id = $2`,
		id, schoolID, in.TripDate, in.TripType, in.RouteID, in.BusID, in.DriverID, in.Notes, in.Title, in.NotifyParents,
		in.ScheduledStartAt, in.ScheduledEndAt)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UnassignTrip clears a trip's bus and driver together — route, schedule,
// trip_date, trip_type, stops and students are all untouched; only the
// assignment relationship is removed. The caller (handlers.UnassignTrip) is
// responsible for checking the trip is still open (scheduled/confirmed) and
// resetting a confirmed trip to scheduled first, through the usual
// changeTripStatus path, before calling this — by the time this runs there is
// nothing left to validate here. Safe to call again on an already-unassigned
// trip (RowsAffected is 1 either way; re-nulling an already-null column is a
// harmless no-op), matching this project's existing idempotent-retry
// convention (e.g. DriverClaimRoute).
func UnassignTrip(ctx context.Context, q DBTX, schoolID, id string) error {
	tag, err := q.Exec(ctx, `UPDATE trips SET bus_id = NULL, driver_id = NULL, confirmed_at = NULL
		WHERE id = $1 AND school_id = $2`, id, schoolID)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReassignDriver replaces only a trip's driver — bus, route, schedule, status
// and everything else are untouched. The caller has already run the same
// overlap check (TripSlotClash, scoped to this trip's own route/bus/schedule)
// used everywhere else in this feature, so a genuine conflict never reaches
// this far; this is just the actual column change.
func ReassignDriver(ctx context.Context, q DBTX, schoolID, id, driverID string) error {
	tag, err := q.Exec(ctx, `UPDATE trips SET driver_id = $3 WHERE id = $1 AND school_id = $2`, id, schoolID, driverID)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReassignBus replaces only a trip's bus — driver, route, schedule, status and
// everything else are untouched. Same conflict-checking precondition as
// ReassignDriver.
func ReassignBus(ctx context.Context, q DBTX, schoolID, id, busID string) error {
	tag, err := q.Exec(ctx, `UPDATE trips SET bus_id = $3 WHERE id = $1 AND school_id = $2`, id, schoolID, busID)
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
	SupportsOuting                       bool
	StopCount                            int
	// DropSequenceComplete: every stop on the route with a drop_time has a
	// drop_sequence set (the route's evening order is fully configured). Only
	// meaningful (and only computed) when a route was given.
	DropSequenceComplete bool
}

// GetTripRefs looks up the chosen bus and driver, and the route when routeID is
// non-nil (a routeless Special Trip/Outing has no route to check).
func GetTripRefs(ctx context.Context, q DBTX, schoolID string, routeID *string, busID, driverID string) (*TripRefs, error) {
	var t TripRefs
	if routeID != nil {
		err := q.QueryRow(ctx, `SELECT status, supports_pickup, supports_drop, supports_outing,
			(SELECT count(*) FROM stops WHERE route_id = routes.id),
			NOT EXISTS (SELECT 1 FROM stops WHERE route_id = routes.id AND drop_time IS NOT NULL AND drop_sequence IS NULL)
			FROM routes WHERE id = $1 AND school_id = $2`,
			*routeID, schoolID).Scan(&t.RouteStatus, &t.SupportsPickup, &t.SupportsDrop, &t.SupportsOuting,
			&t.StopCount, &t.DropSequenceComplete)
		if err == nil {
			t.RouteFound = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	// busID/driverID empty means "not being checked" (e.g. ReassignTripDriver
	// only cares about the driver side) — skipped the same way routeID is
	// skipped when nil, rather than erroring on an invalid empty uuid param.
	if busID != "" {
		err := q.QueryRow(ctx, `SELECT status FROM buses WHERE id = $1 AND school_id = $2`, busID, schoolID).Scan(&t.BusStatus)
		if err == nil {
			t.BusFound = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if driverID != "" {
		err := q.QueryRow(ctx, `SELECT status FROM drivers WHERE id = $1 AND school_id = $2`, driverID, schoolID).Scan(&t.DriverStatus)
		if err == nil {
			t.DriverFound = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
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

// TripCalendarDay counts one date + trip type's trips (Trip Planner calendar).
type TripCalendarDay struct {
	TripDate  string `json:"trip_date"`
	TripType  string `json:"trip_type"`
	Assigned  int    `json:"assigned"` // not cancelled: scheduled, confirmed, started or completed
	Confirmed int    `json:"confirmed"`
	Started   int    `json:"started"`
	Completed int    `json:"completed"`
	Cancelled int    `json:"cancelled"`
}

// TripCalendar counts a school's trips per date and trip type in an inclusive date range.
func TripCalendar(ctx context.Context, q DBTX, schoolID, from, to string) ([]TripCalendarDay, error) {
	rows, err := q.Query(ctx, `
		SELECT trip_date::text, trip_type,
			count(*) FILTER (WHERE status <> 'cancelled'),
			count(*) FILTER (WHERE status = 'confirmed'),
			count(*) FILTER (WHERE status = 'started'),
			count(*) FILTER (WHERE status = 'completed'),
			count(*) FILTER (WHERE status = 'cancelled')
		FROM trips WHERE school_id = $1 AND trip_date BETWEEN $2::date AND $3::date
		GROUP BY trip_date, trip_type ORDER BY trip_date, trip_type DESC`, schoolID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TripCalendarDay{}
	for rows.Next() {
		var d TripCalendarDay
		if err := rows.Scan(&d.TripDate, &d.TripType, &d.Assigned, &d.Confirmed, &d.Started, &d.Completed, &d.Cancelled); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// FindUnassignedSlot looks for a non-cancelled trip on this exact
// route+date+type that currently has no bus and no driver at all — the
// literal placeholder Unassign leaves behind — for staff re-assigning that
// route/date/type (single create, Bulk Assign, Copy) to fill in rather than
// be told the route "already has an overlapping trip" (itself) or get a
// second, competing row for the same slot.
//
// If there is exactly one such candidate, it is returned regardless of
// whether its own scheduled window happens to overlap [start, end): in
// normal use there is only ever one trip per route/date/type being managed
// at a time, so a single unassigned trip for this slot IS the one being
// re-assigned even if the admin is also correcting its time — refusing to
// reuse it here would silently leave it orphaned forever while a second,
// duplicate trip gets created alongside it (the exact mechanism behind a
// trip count that grows every time a schedule is changed while
// re-assigning). Only when more than one candidate exists (the rarer case
// of several same-slot trips having all been unassigned — the
// Multiple-Daily-Trips feature) is the match narrowed to one whose window
// actually overlaps (or is schedule-less on either side); if that is still
// ambiguous, "" is returned and the caller falls back to its normal
// create/clash-check path rather than guessing which one to fill in.
func FindUnassignedSlot(ctx context.Context, q DBTX, schoolID, routeID, tripDate, tripType string, start, end *time.Time) (string, error) {
	rows, err := q.Query(ctx, `
		SELECT id, scheduled_start_at, scheduled_end_at FROM trips
		WHERE school_id = $1 AND route_id = $2 AND trip_date = $3::date AND trip_type = $4
			AND status <> 'cancelled' AND bus_id IS NULL AND driver_id IS NULL`,
		schoolID, routeID, tripDate, tripType)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	type candidate struct {
		id          string
		start, end_ *time.Time
	}
	var all []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.start, &c.end_); err != nil {
			return "", err
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(all) == 0 {
		return "", nil
	}
	if len(all) == 1 {
		return all[0].id, nil
	}
	overlaps := func(c candidate) bool {
		if c.start == nil || c.end_ == nil || start == nil || end == nil {
			return true
		}
		return c.start.Before(*end) && start.Before(*c.end_)
	}
	match, matches := "", 0
	for _, c := range all {
		if overlaps(c) {
			match, matches = c.id, matches+1
		}
	}
	if matches == 1 {
		return match, nil
	}
	return "", nil
}

// TripSlotClash says which of route, bus or driver already has a (non-cancelled)
// trip whose scheduled window overlaps [start, end) — deliberately regardless of
// trip_type or trip_date (a driver can't run a Morning Pickup and a Special Trip
// at the same real moment any more than two Morning Pickups) — or "" if none do.
// Mirrors trips_{route,bus,driver}_no_overlap and the restored
// trips_{route,bus,driver}_slot_key, the actual transactional guards against
// a race; this is only a friendlier pre-check so a clash names which field
// it's in before the insert/update is even attempted. excludeTripID is the
// trip's own ID on an update (it must never conflict with itself) or "" for
// a new trip.
func TripSlotClash(ctx context.Context, q DBTX, schoolID string, routeID, busID, driverID *string, tripDate, tripType, excludeTripID string, start, end *time.Time) (string, error) {
	var route, bus, driver bool
	// A schedule-less trip (either side: the existing row, or the one being
	// created/updated) has no time window to compare, so it conflicts with
	// anything else on the same route/bus/driver for that date+type — the
	// same one-per-slot rule this feature's trips had before scheduled
	// windows existed. Only when both sides have a window does the real
	// overlap check decide it.
	err := q.QueryRow(ctx, `
		SELECT coalesce(bool_or(route_id = $2), false), coalesce(bool_or(bus_id = $3), false), coalesce(bool_or(driver_id = $4), false)
		FROM trips
		WHERE school_id = $1 AND status <> 'cancelled'
			AND (route_id = $2 OR bus_id = $3 OR driver_id = $4)
			AND trip_date = $7::date AND trip_type = $8
			AND ($9 = '' OR id <> $9::uuid)
			AND (
				scheduled_start_at IS NULL OR scheduled_end_at IS NULL
				OR $5::timestamptz IS NULL OR $6::timestamptz IS NULL
				OR tstzrange(scheduled_start_at, scheduled_end_at, '[)') && tstzrange($5::timestamptz, $6::timestamptz, '[)')
			)`,
		schoolID, routeID, busID, driverID, start, end, tripDate, tripType, excludeTripID).Scan(&route, &bus, &driver)
	switch {
	case err != nil:
		return "", err
	case route:
		return "route already has a trip", nil
	case bus:
		return "bus is already on another trip", nil
	case driver:
		return "driver is already on another trip", nil
	}
	return "", nil
}
