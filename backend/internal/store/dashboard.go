package store

import "context"

// Dashboard is the admin overview for one school (CLAUDE.md "Dashboard shows").
type Dashboard struct {
	Today  string `json:"today"` // YYYY-MM-DD in the school's time zone
	Totals struct {
		BusesActive         int `json:"buses_active"`
		BusesTotal          int `json:"buses_total"`
		BusesInMaintenance  int `json:"buses_in_maintenance"`
		DriversActive       int `json:"drivers_active"`
		StudentsOnTransport int `json:"students_on_transport"` // active, uses transport
		StudentsUnassigned  int `json:"students_unassigned"`   // ...without a route yet
		RoutesActive        int `json:"routes_active"`
	} `json:"totals"`
	// Today's trips: trip type → status → count.
	Trips map[string]map[string]int `json:"trips"`
	Live  struct {
		OnRoad  int `json:"on_road"` // trips in progress with GPS
		Offline int `json:"offline"` // ...of which stale
	} `json:"live"`
	DelayedTrips    int            `json:"delayed_trips"` // trips with a delay reported today
	OpenEmergencies int            `json:"open_emergencies"`
	Routes          []RouteSummary `json:"routes"`
}

// RouteSummary is one active route's day at a glance.
type RouteSummary struct {
	RouteID       string  `json:"route_id"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Students      int     `json:"students"`
	MorningStatus *string `json:"morning_status"` // null: no trip assigned today
	EveningStatus *string `json:"evening_status"`
	MorningBus    *string `json:"morning_bus"`
	EveningBus    *string `json:"evening_bus"`
	Delays        int     `json:"delays"`
	MissedStops   int     `json:"missed_stops"`
}

// GetDashboard computes the overview. Live counts come from Redis (caller).
func GetDashboard(ctx context.Context, q DBTX, schoolID string) (*Dashboard, error) {
	d := &Dashboard{Trips: map[string]map[string]int{}, Routes: []RouteSummary{}}
	t := &d.Totals
	err := q.QueryRow(ctx, `
		SELECT (now() AT TIME ZONE s.timezone)::date::text,
			(SELECT count(*) FILTER (WHERE status = 'active') FROM buses WHERE school_id = s.id),
			(SELECT count(*) FROM buses WHERE school_id = s.id),
			(SELECT count(*) FILTER (WHERE status = 'maintenance') FROM buses WHERE school_id = s.id),
			(SELECT count(*) FROM drivers WHERE school_id = s.id AND status = 'active'),
			(SELECT count(*) FROM students WHERE school_id = s.id AND status = 'active' AND transport_status = 'uses_transport'),
			(SELECT count(*) FROM students st WHERE st.school_id = s.id AND st.status = 'active' AND st.transport_status = 'uses_transport'
				AND NOT EXISTS (SELECT 1 FROM student_assignments sa WHERE sa.student_id = st.id AND sa.ended_at IS NULL)),
			(SELECT count(*) FROM routes WHERE school_id = s.id AND status = 'active'),
			(SELECT count(DISTINCT de.trip_id) FROM delay_events de JOIN trips tr ON tr.id = de.trip_id
				WHERE de.school_id = s.id AND tr.trip_date = (now() AT TIME ZONE s.timezone)::date),
			(SELECT count(*) FROM emergencies WHERE school_id = s.id AND status <> 'resolved')
		FROM schools s WHERE s.id = $1`, schoolID).
		Scan(&d.Today, &t.BusesActive, &t.BusesTotal, &t.BusesInMaintenance, &t.DriversActive, &t.StudentsOnTransport,
			&t.StudentsUnassigned, &t.RoutesActive, &d.DelayedTrips, &d.OpenEmergencies)
	if err != nil {
		return nil, mapErr(err)
	}

	rows, err := q.Query(ctx, `SELECT trip_type, status, count(*) FROM trips
		WHERE school_id = $1 AND trip_date = $2::date GROUP BY 1, 2`, schoolID, d.Today)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var typ, status string
		var n int
		if err := rows.Scan(&typ, &status, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if d.Trips[typ] == nil {
			d.Trips[typ] = map[string]int{}
		}
		d.Trips[typ][status] = n
	}
	rows.Close()

	rows, err = q.Query(ctx, `
		SELECT r.id, r.code, r.name,
			(SELECT count(*) FROM student_assignments sa JOIN students st ON st.id = sa.student_id AND st.status = 'active'
				WHERE sa.route_id = r.id AND sa.ended_at IS NULL),
			m.status, e.status, mb.vehicle_number, eb.vehicle_number,
			(SELECT count(*) FROM delay_events de WHERE de.trip_id IN (m.id, e.id)),
			(SELECT count(*) FROM trip_stop_events se WHERE se.trip_id IN (m.id, e.id) AND se.missed)
		FROM routes r
		LEFT JOIN trips m ON m.route_id = r.id AND m.trip_date = $2::date AND m.trip_type = 'morning_pickup' AND m.status <> 'cancelled'
		LEFT JOIN trips e ON e.route_id = r.id AND e.trip_date = $2::date AND e.trip_type = 'evening_drop' AND e.status <> 'cancelled'
		LEFT JOIN buses mb ON mb.id = m.bus_id
		LEFT JOIN buses eb ON eb.id = e.bus_id
		WHERE r.school_id = $1 AND r.status = 'active'
		ORDER BY r.code`, schoolID, d.Today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rs RouteSummary
		if err := rows.Scan(&rs.RouteID, &rs.Code, &rs.Name, &rs.Students, &rs.MorningStatus, &rs.EveningStatus,
			&rs.MorningBus, &rs.EveningBus, &rs.Delays, &rs.MissedStops); err != nil {
			return nil, err
		}
		d.Routes = append(d.Routes, rs)
	}
	return d, rows.Err()
}
