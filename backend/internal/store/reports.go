package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Report definitions (CLAUDE.md "Reports"): trip, bus journey, driver trip, route
// performance, delay, assignment, notification and location history.
//
// Every query is scoped to the school ($1) and returns text/number columns
// already formatted in the school's time zone. Each report lists the filters it
// uses; only those become query parameters, in that order after the school.

type ReportColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type ReportDef struct {
	Key         string         `json:"key"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Filters     []string       `json:"filters"` // from, to, route_id, bus_id, driver_id, trip_id
	Columns     []ReportColumn `json:"columns"`
	sql         string
}

// ReportFilter holds the values; empty strings mean "any".
type ReportFilter struct {
	From, To, RouteID, BusID, DriverID, TripID string
}

// Value returns the filter named as in ReportDef.Filters.
func (f ReportFilter) Value(name string) string {
	switch name {
	case "from":
		return f.From
	case "to":
		return f.To
	case "route_id":
		return f.RouteID
	case "bus_id":
		return f.BusID
	case "driver_id":
		return f.DriverID
	case "trip_id":
		return f.TripID
	}
	return ""
}

func cols(pairs ...string) []ReportColumn {
	out := make([]ReportColumn, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, ReportColumn{pairs[i], pairs[i+1]})
	}
	return out
}

// Shared fragments.
const (
	tripJoins = `
		JOIN schools s ON s.id = t.school_id
		JOIN routes r ON r.id = t.route_id
		JOIN buses b ON b.id = t.bus_id
		JOIN drivers d ON d.id = t.driver_id
		JOIN users du ON du.id = d.user_id`
	localTime = `to_char(%s AT TIME ZONE s.timezone, 'YYYY-MM-DD HH24:MI')`
	clockTime = `to_char(%s AT TIME ZONE s.timezone, 'HH24:MI')`
	minutes   = `round(extract(epoch FROM (%s - %s)) / 60)::int`
	// Great-circle distance between consecutive fixes, in km.
	haversineKm = `6371 * 2 * asin(sqrt(power(sin(radians(latitude - plat) / 2), 2)
		+ cos(radians(plat)) * cos(radians(latitude)) * power(sin(radians(longitude - plng) / 2), 2)))`
)

func lt(col string) string    { return fmt.Sprintf(localTime, col) }
func clock(col string) string { return fmt.Sprintf(clockTime, col) }

// Reports lists the available reports, in menu order.
var Reports = []ReportDef{
	{
		Key: "trips", Title: "Trip report", Description: "Every trip in the period with its times, stops and delays.",
		Filters: []string{"from", "to", "route_id", "bus_id", "driver_id"},
		Columns: cols("date", "Date", "trip_type", "Trip", "route", "Route", "bus", "Bus", "driver", "Driver", "status", "Status",
			"started", "Started", "ended", "Ended", "duration_min", "Duration (min)", "stops_reached", "Stops reached",
			"stops_missed", "Stops missed", "delays", "Delays reported", "cancel_reason", "Cancel reason"),
		sql: `SELECT t.trip_date::text, t.trip_type, r.code, b.vehicle_number, du.name, t.status,
			` + clock("t.started_at") + `, ` + clock("t.ended_at") + `, ` + fmt.Sprintf(minutes, "t.ended_at", "t.started_at") + `,
			(SELECT count(*) FROM trip_stop_events e WHERE e.trip_id = t.id AND e.event_type = 'reached'),
			(SELECT count(*) FROM trip_stop_events e WHERE e.trip_id = t.id AND e.missed),
			(SELECT count(*) FROM delay_events de WHERE de.trip_id = t.id), t.cancel_reason
			FROM trips t` + tripJoins + `
			WHERE t.school_id = $1 AND t.trip_date BETWEEN $2::date AND $3::date
			AND ($4 = '' OR t.route_id::text = $4) AND ($5 = '' OR t.bus_id::text = $5) AND ($6 = '' OR t.driver_id::text = $6)
			ORDER BY t.trip_date, t.trip_type DESC, r.code`,
	},
	{
		Key: "bus_journeys", Title: "Bus journey report", Description: "Per bus and day: trips, time on the road and distance (from GPS history).",
		Filters: []string{"from", "to", "bus_id"},
		Columns: cols("date", "Date", "bus", "Bus", "trips", "Trips", "completed", "Completed", "first_start", "First start",
			"last_end", "Last end", "minutes_on_road", "Minutes on road", "distance_km", "Distance (km)"),
		sql: `WITH pts AS (
				SELECT l.trip_id, l.latitude, l.longitude,
					lag(l.latitude) OVER w AS plat, lag(l.longitude) OVER w AS plng
				FROM trip_locations l JOIN trips t ON t.id = l.trip_id
				WHERE t.school_id = $1 AND t.trip_date BETWEEN $2::date AND $3::date
				WINDOW w AS (PARTITION BY l.trip_id ORDER BY l.recorded_at)),
			dist AS (SELECT trip_id, sum(` + haversineKm + `) AS km FROM pts WHERE plat IS NOT NULL GROUP BY trip_id)
			SELECT t.trip_date::text, b.vehicle_number, count(*), count(*) FILTER (WHERE t.status = 'completed'),
				` + clock("min(t.started_at)") + `, ` + clock("max(t.ended_at)") + `,
				coalesce(sum(` + fmt.Sprintf(minutes, "t.ended_at", "t.started_at") + `), 0),
				round(coalesce(sum(dist.km), 0)::numeric, 1)
			FROM trips t JOIN schools s ON s.id = t.school_id JOIN buses b ON b.id = t.bus_id
			LEFT JOIN dist ON dist.trip_id = t.id
			WHERE t.school_id = $1 AND t.trip_date BETWEEN $2::date AND $3::date AND t.status <> 'cancelled'
			AND ($4 = '' OR t.bus_id::text = $4)
			GROUP BY t.trip_date, b.vehicle_number, s.timezone ORDER BY t.trip_date, b.vehicle_number`,
	},
	{
		Key: "driver_trips", Title: "Driver trip report", Description: "Per driver: trips assigned, completed and cancelled, delays, average duration.",
		Filters: []string{"from", "to", "driver_id"},
		Columns: cols("driver", "Driver", "mobile", "Mobile", "assigned", "Trips assigned", "completed", "Completed",
			"cancelled", "Cancelled", "delays", "Delays reported", "avg_duration_min", "Avg duration (min)"),
		sql: `SELECT du.name, du.mobile, count(t.id), count(t.id) FILTER (WHERE t.status = 'completed'),
				count(t.id) FILTER (WHERE t.status = 'cancelled'),
				(SELECT count(*) FROM delay_events de JOIN trips t2 ON t2.id = de.trip_id
					WHERE t2.driver_id = d.id AND t2.trip_date BETWEEN $2::date AND $3::date),
				round(avg(` + fmt.Sprintf(minutes, "t.ended_at", "t.started_at") + `))::int
			FROM drivers d JOIN users du ON du.id = d.user_id
			LEFT JOIN trips t ON t.driver_id = d.id AND t.trip_date BETWEEN $2::date AND $3::date
			WHERE d.school_id = $1 AND ($4 = '' OR d.id::text = $4)
			GROUP BY d.id, du.name, du.mobile ORDER BY du.name`,
	},
	{
		Key: "route_performance", Title: "Route performance report",
		Description: "Per route: completed trips, average duration, missed stops, delays, and how late the bus reached stops versus their scheduled time.",
		Filters:     []string{"from", "to", "route_id"},
		Columns: cols("route", "Route", "name", "Name", "trips", "Trips", "completed", "Completed", "avg_duration_min", "Avg duration (min)",
			"stops_reached", "Stops reached", "stops_missed", "Stops missed", "delays", "Delays", "avg_delay_min", "Avg reported delay (min)",
			"avg_late_min", "Avg arrival vs schedule (min)"),
		sql: `SELECT r.code, r.name, count(DISTINCT t.id), count(DISTINCT t.id) FILTER (WHERE t.status = 'completed'),
				round(avg(` + fmt.Sprintf(minutes, "t.ended_at", "t.started_at") + `))::int,
				(SELECT count(*) FROM trip_stop_events e JOIN trips x ON x.id = e.trip_id
					WHERE x.route_id = r.id AND x.trip_date BETWEEN $2::date AND $3::date AND e.event_type = 'reached'),
				(SELECT count(*) FROM trip_stop_events e JOIN trips x ON x.id = e.trip_id
					WHERE x.route_id = r.id AND x.trip_date BETWEEN $2::date AND $3::date AND e.missed),
				(SELECT count(*) FROM delay_events de JOIN trips x ON x.id = de.trip_id
					WHERE x.route_id = r.id AND x.trip_date BETWEEN $2::date AND $3::date),
				(SELECT round(avg(de.minutes))::int FROM delay_events de JOIN trips x ON x.id = de.trip_id
					WHERE x.route_id = r.id AND x.trip_date BETWEEN $2::date AND $3::date),
				(SELECT round(avg(extract(epoch FROM ((e.occurred_at AT TIME ZONE s.timezone)::time
						- CASE WHEN x.trip_type = 'morning_pickup' THEN st.pickup_time ELSE st.drop_time END)) / 60))::int
					FROM trip_stop_events e JOIN trips x ON x.id = e.trip_id JOIN stops st ON st.id = e.stop_id
					WHERE x.route_id = r.id AND x.trip_date BETWEEN $2::date AND $3::date AND e.event_type = 'reached'
					AND CASE WHEN x.trip_type = 'morning_pickup' THEN st.pickup_time ELSE st.drop_time END IS NOT NULL)
			FROM routes r JOIN schools s ON s.id = r.school_id
			LEFT JOIN trips t ON t.route_id = r.id AND t.trip_date BETWEEN $2::date AND $3::date AND t.status <> 'cancelled'
			WHERE r.school_id = $1 AND ($4 = '' OR r.id::text = $4)
			GROUP BY r.id, r.code, r.name, s.timezone ORDER BY r.code`,
	},
	{
		Key: "delays", Title: "Delay report", Description: "Every delay and breakdown reported, with reason and who reported it.",
		Filters: []string{"from", "to", "route_id", "bus_id", "driver_id"},
		Columns: cols("reported_at", "Reported at", "trip_type", "Trip", "route", "Route", "bus", "Bus", "driver", "Driver",
			"minutes", "Minutes", "reason", "Reason", "note", "Note", "reported_by", "Reported by", "reporter_role", "Role"),
		sql: `SELECT ` + lt("x.created_at") + `, t.trip_type, r.code, b.vehicle_number, du.name, x.minutes, x.reason, x.note,
				rep.name, x.reporter_role
			FROM delay_events x JOIN trips t ON t.id = x.trip_id` + tripJoins + `
			LEFT JOIN users rep ON rep.id = x.reported_by
			WHERE x.school_id = $1 AND t.trip_date BETWEEN $2::date AND $3::date
			AND ($4 = '' OR t.route_id::text = $4) AND ($5 = '' OR t.bus_id::text = $5) AND ($6 = '' OR t.driver_id::text = $6)
			ORDER BY x.created_at`,
	},
	{
		Key: "assignments", Title: "Assignment report", Description: "Student route and stop assignments that were in effect during the period, with history.",
		Filters: []string{"from", "to", "route_id"},
		Columns: cols("admission_no", "Adm. no.", "student", "Student", "class", "Class", "route", "Route", "pickup_stop", "Pickup stop",
			"drop_stop", "Drop stop", "from", "From", "to", "To", "assigned_by", "Assigned by"),
		sql: `SELECT st.admission_no, st.name, concat_ws('-', nullif(st.class, ''), nullif(st.section, '')), r.code, ps.name, ds.name,
				` + lt("sa.assigned_at") + `, coalesce(` + lt("sa.ended_at") + `, 'current'), u.name
			FROM student_assignments sa
			JOIN schools s ON s.id = sa.school_id
			JOIN students st ON st.id = sa.student_id
			JOIN routes r ON r.id = sa.route_id
			LEFT JOIN stops ps ON ps.id = sa.pickup_stop_id
			LEFT JOIN stops ds ON ds.id = sa.drop_stop_id
			LEFT JOIN users u ON u.id = sa.assigned_by
			WHERE sa.school_id = $1 AND (sa.assigned_at AT TIME ZONE s.timezone)::date <= $3::date
			AND (sa.ended_at IS NULL OR (sa.ended_at AT TIME ZONE s.timezone)::date >= $2::date)
			AND ($4 = '' OR sa.route_id::text = $4)
			ORDER BY r.code, st.name, sa.assigned_at`,
	},
	{
		Key: "notifications", Title: "Notification report", Description: "Notifications sent to parents, with delivery and read counts.",
		Filters: []string{"from", "to", "route_id"},
		Columns: cols("sent_at", "Sent at", "type", "Type", "title", "Title", "route", "Route", "recipients", "Parents",
			"pushed", "Pushed", "failed", "Failed", "read", "Read"),
		sql: `SELECT ` + lt("n.created_at") + `, n.type, n.title, coalesce(r.code, 'Whole school'), n.recipient_count,
				(SELECT count(*) FROM push_deliveries p WHERE p.notification_id = n.id AND p.status = 'sent'),
				(SELECT count(*) FROM push_deliveries p WHERE p.notification_id = n.id AND p.status IN ('failed', 'invalid')),
				(SELECT count(DISTINCT user_id) FROM notification_recipients nr WHERE nr.notification_id = n.id AND nr.read_at IS NOT NULL)
			FROM notifications n JOIN schools s ON s.id = n.school_id LEFT JOIN routes r ON r.id = n.route_id
			WHERE n.school_id = $1 AND (n.created_at AT TIME ZONE s.timezone)::date BETWEEN $2::date AND $3::date
			AND ($4 = '' OR n.route_id::text = $4)
			ORDER BY n.created_at`,
	},
	{
		Key: "location_history", Title: "Location history", Description: "Every stored GPS point of one trip (kept as long as the school's retention setting).",
		Filters: []string{"trip_id"},
		Columns: cols("recorded_at", "Recorded at", "latitude", "Latitude", "longitude", "Longitude", "speed_kmh", "Speed (km/h)",
			"accuracy_m", "Accuracy (m)"),
		sql: `SELECT to_char(l.recorded_at AT TIME ZONE s.timezone, 'YYYY-MM-DD HH24:MI:SS'), round(l.latitude::numeric, 6),
				round(l.longitude::numeric, 6), round((l.speed_mps * 3.6)::numeric, 1), round(l.accuracy_m::numeric)
			FROM trip_locations l JOIN trips t ON t.id = l.trip_id JOIN schools s ON s.id = t.school_id
			WHERE t.school_id = $1 AND t.id::text = $2
			ORDER BY l.recorded_at`,
	},
}

// FindReport returns a report definition by key.
func FindReport(key string) (*ReportDef, bool) {
	for i := range Reports {
		if Reports[i].Key == key {
			return &Reports[i], true
		}
	}
	return nil, false
}

func (r *ReportDef) args(schoolID string, f ReportFilter) []any {
	args := []any{schoolID}
	for _, name := range r.Filters {
		args = append(args, f.Value(name))
	}
	return args
}

// RunReport streams the report's rows as strings to emit. limit/offset 0 = all rows.
func RunReport(ctx context.Context, q DBTX, r *ReportDef, schoolID string, f ReportFilter, limit, offset int,
	emit func(row []string) error) error {
	sql := r.sql
	args := r.args(schoolID, f)
	if limit > 0 {
		sql = fmt.Sprintf("SELECT * FROM (%s) rep LIMIT %d OFFSET %d", sql, limit, offset)
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("report %s: %w", r.Key, err)
	}
	defer rows.Close()
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return err
		}
		out := make([]string, len(vals))
		for i, v := range vals {
			out[i] = formatCell(v)
		}
		if err := emit(out); err != nil {
			return err
		}
	}
	return rows.Err()
}

// CountReport returns how many rows the report has (for paging the preview).
func CountReport(ctx context.Context, q DBTX, r *ReportDef, schoolID string, f ReportFilter) (int, error) {
	var n int
	err := q.QueryRow(ctx, "SELECT count(*) FROM ("+r.sql+") rep", r.args(schoolID, f)...).Scan(&n)
	return n, err
}

func formatCell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Time:
		return x.Format(time.RFC3339)
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case fmt.Stringer:
		return x.String()
	default:
		// pgtype.Numeric and friends
		if m, ok := v.(interface{ MarshalJSON() ([]byte, error) }); ok {
			if b, err := m.MarshalJSON(); err == nil {
				return strings.Trim(string(b), `"`)
			}
		}
		return fmt.Sprint(v)
	}
}
