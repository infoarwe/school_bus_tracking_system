package store

import (
	"context"
)

// Driver self-assignment: the routes a driver may take on a date and trip type,
// and the buses free in that slot. A claim itself is an ordinary trips row
// (CreateTrip); the partial unique indexes on trips keep a route, bus and driver
// to one trip per date + trip type, so concurrent claims cannot double-book.

// RouteSlot is one eligible route for a date and trip type, with the trip that
// currently holds it (nil fields: nobody has taken it).
type RouteSlot struct {
	RouteID      string
	Code         string
	Name         string
	StartPoint   string
	StopCount    int
	StudentCount int
	TripID       *string
	TripStatus   *string
	DriverID     *string
	BusID        *string
}

// RouteSlots lists the school's active routes that run tripType and have stops,
// with the non-cancelled trip (if any) on date. A route can now have more than
// one non-cancelled trip of the same type and date (different scheduled
// times) — this still surfaces only one (the earliest-created) as "the" trip
// holding the slot, preserving the existing one-claim-per-route-per-day
// self-assignment behavior; a LEFT JOIN here would instead silently return one
// duplicate RouteSlot row per extra trip.
func RouteSlots(ctx context.Context, q DBTX, schoolID, date, tripType string) ([]RouteSlot, error) {
	rows, err := q.Query(ctx, `
		SELECT r.id, r.code, r.name, r.start_point,
			(SELECT count(*) FROM stops s WHERE s.route_id = r.id),
			(SELECT count(*) FROM student_assignments sa JOIN students st ON st.id = sa.student_id
				WHERE sa.route_id = r.id AND sa.ended_at IS NULL AND st.status = 'active'
				AND CASE WHEN $3 = 'morning_pickup' THEN sa.pickup_stop_id ELSE sa.drop_stop_id END IS NOT NULL),
			t.id, t.status, t.driver_id, t.bus_id
		FROM routes r
		LEFT JOIN LATERAL (
			SELECT id, status, driver_id, bus_id FROM trips
			WHERE route_id = r.id AND trip_date = $2::date AND trip_type = $3 AND status <> 'cancelled'
			ORDER BY created_at LIMIT 1
		) t ON true
		WHERE r.school_id = $1 AND r.status = 'active'
			AND CASE WHEN $3 = 'morning_pickup' THEN r.supports_pickup ELSE r.supports_drop END
			AND EXISTS (SELECT 1 FROM stops s WHERE s.route_id = r.id)
		ORDER BY r.code`, schoolID, date, tripType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RouteSlot{}
	for rows.Next() {
		var s RouteSlot
		if err := rows.Scan(&s.RouteID, &s.Code, &s.Name, &s.StartPoint, &s.StopCount, &s.StudentCount,
			&s.TripID, &s.TripStatus, &s.DriverID, &s.BusID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SlotStop is a stop as a driver sees it while choosing a route (no student names).
type SlotStop struct {
	RouteID  string  `json:"-"`
	Sequence int     `json:"sequence"`
	Name     string  `json:"name"`
	Landmark string  `json:"landmark"`
	Time     *string `json:"time"` // pickup time (Morning Pickup) or drop time (Evening Drop), HH:MM
}

// SlotStops returns the ordered stops of the given routes, with the time for tripType.
func SlotStops(ctx context.Context, q DBTX, routeIDs []string, tripType string) (map[string][]SlotStop, error) {
	rows, err := q.Query(ctx, `
		SELECT route_id, sequence, name, landmark,
			to_char(CASE WHEN $2 = 'morning_pickup' THEN pickup_time ELSE drop_time END, 'HH24:MI')
		FROM stops WHERE route_id = ANY($1::uuid[]) ORDER BY route_id, sequence`, routeIDs, tripType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]SlotStop{}
	for rows.Next() {
		var s SlotStop
		if err := rows.Scan(&s.RouteID, &s.Sequence, &s.Name, &s.Landmark, &s.Time); err != nil {
			return nil, err
		}
		out[s.RouteID] = append(out[s.RouteID], s)
	}
	return out, rows.Err()
}

// SlotBus is an active bus of the school; InUse when another trip has it in this slot.
type SlotBus struct {
	ID            string `json:"id"`
	VehicleNumber string `json:"vehicle_number"`
	Capacity      int    `json:"capacity"`
	Available     bool   `json:"available"`
	// TripID of the non-cancelled trip using it in this slot (for "mine" detection).
	TripID *string `json:"-"`
}

// SlotBuses: same duplicate-row risk as RouteSlots once a bus can have more
// than one non-cancelled trip of this type/date — bounded the same way.
func SlotBuses(ctx context.Context, q DBTX, schoolID, date, tripType string) ([]SlotBus, error) {
	rows, err := q.Query(ctx, `
		SELECT b.id, b.vehicle_number, b.capacity, t.id
		FROM buses b
		LEFT JOIN LATERAL (
			SELECT id FROM trips
			WHERE bus_id = b.id AND trip_date = $2::date AND trip_type = $3 AND status <> 'cancelled'
			ORDER BY created_at LIMIT 1
		) t ON true
		WHERE b.school_id = $1 AND b.status = 'active'
		ORDER BY b.vehicle_number`, schoolID, date, tripType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SlotBus{}
	for rows.Next() {
		var b SlotBus
		if err := rows.Scan(&b.ID, &b.VehicleNumber, &b.Capacity, &b.TripID); err != nil {
			return nil, err
		}
		b.Available = b.TripID == nil
		out = append(out, b)
	}
	return out, rows.Err()
}

// SchoolStaff are the users told about route claims: the school's active School
// Admins and Transport Managers (both run daily transport).
func SchoolStaff(ctx context.Context, q DBTX, schoolID string) ([]Rider, error) {
	return scanRiders(ctx, q, `
		SELECT id, NULL::uuid, '' FROM users
		WHERE school_id = $1 AND status = 'active' AND role IN ('school_admin', 'transport_manager')
		ORDER BY id`, schoolID)
}
