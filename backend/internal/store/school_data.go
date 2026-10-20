package store

import (
	"context"
	"fmt"
)

// Parts of a school's data that ClearSchoolData can remove. The school itself,
// its settings, branding, staff users (School Admin, Transport Manager) and the
// audit log are never removed.
const (
	ClearTrips         = "trips"         // trips and everything under them (history, GPS, stop events, delays, emergencies, snapshots)
	ClearNotifications = "notifications" // notifications, inbox, push queue, announcements
	ClearStudents      = "students"      // students, parents (and their logins), links, route assignments
	ClearRoutes        = "routes"        // routes, stops, student route assignments, driver route assignments
	ClearBuses         = "buses"
	ClearDrivers       = "drivers" // drivers and their logins
)

var ClearScopes = []string{ClearTrips, ClearNotifications, ClearStudents, ClearRoutes, ClearBuses, ClearDrivers}

// ClearScopeNeeds lists what each part requires to be cleared with it: trips
// reference routes, buses and drivers; notifications and announcements can
// target a route.
var ClearScopeNeeds = map[string][]string{
	ClearRoutes:  {ClearTrips, ClearNotifications},
	ClearBuses:   {ClearTrips},
	ClearDrivers: {ClearTrips},
}

// StartedTripCount counts a school's trips in progress (clearing trips waits for them).
func StartedTripCount(ctx context.Context, q DBTX, schoolID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM trips WHERE school_id = $1 AND status = 'started'`, schoolID).Scan(&n)
	return n, err
}

// RouteSnapshotTripIDs lists the trips of a school that have a stored route image.
func RouteSnapshotTripIDs(ctx context.Context, q DBTX, schoolID string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT trip_id FROM trip_route_snapshots WHERE school_id = $1`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SchoolParentDeviceTokens lists the device tokens of a school's parents (to
// unsubscribe them from push topics before their logins are removed).
func SchoolParentDeviceTokens(ctx context.Context, q DBTX, schoolID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT d.token FROM device_tokens d JOIN users u ON u.id = d.user_id
		WHERE u.school_id = $1 AND u.role = 'parent'`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ClearSchoolData deletes the chosen parts of one school's data and returns how
// many rows went per kind. Callers check ClearScopeNeeds first and run it in a
// transaction. Driver and parent logins are deleted with their records; audit
// entries they made keep their role but lose the user link.
func ClearSchoolData(ctx context.Context, q DBTX, schoolID string, scopes map[string]bool) (map[string]int64, error) {
	counts := map[string]int64{}
	exec := func(key, sql string, args ...any) error {
		tag, err := q.Exec(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("clear %s: %w", key, err)
		}
		if key != "" {
			counts[key] += tag.RowsAffected()
		}
		return nil
	}
	count := func(key, sql string) error {
		var n int64
		if err := q.QueryRow(ctx, sql, schoolID).Scan(&n); err != nil {
			return fmt.Errorf("count %s: %w", key, err)
		}
		counts[key] = n
		return nil
	}

	// Logins removed with drivers / parents, collected before their records go.
	var users []string
	addUsers := func(sql string) error {
		rows, err := q.Query(ctx, sql, schoolID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			users = append(users, id)
		}
		return rows.Err()
	}
	if scopes[ClearDrivers] {
		if err := addUsers(`SELECT user_id FROM drivers WHERE school_id = $1`); err != nil {
			return nil, err
		}
	}
	if scopes[ClearStudents] {
		if err := addUsers(`SELECT user_id FROM parents WHERE school_id = $1`); err != nil {
			return nil, err
		}
	}

	if scopes[ClearNotifications] {
		if err := exec("announcements", `DELETE FROM announcements WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
		// Delays kept (trips not cleared) lose their link to the notification they sent.
		if err := exec("", `UPDATE delay_events SET notification_id = NULL WHERE school_id = $1 AND notification_id IS NOT NULL`, schoolID); err != nil {
			return nil, err
		}
		if err := exec("notifications", `DELETE FROM notifications WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
	}
	if scopes[ClearTrips] {
		// Rows that go with the trips (ON DELETE CASCADE), counted for the summary.
		for key, table := range map[string]string{"gps_points": "trip_locations", "delay_events": "delay_events", "emergencies": "emergencies"} {
			if err := count(key, `SELECT count(*) FROM `+table+` WHERE school_id = $1`); err != nil {
				return nil, err
			}
		}
		if err := exec("trips", `DELETE FROM trips WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
	}
	if scopes[ClearRoutes] || scopes[ClearBuses] || scopes[ClearDrivers] {
		if err := exec("driver_assignments", `DELETE FROM driver_route_assignments WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
	}
	if scopes[ClearStudents] {
		if err := exec("parents", `DELETE FROM parents WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
		// parent_students, student_assignments and inbox rows cascade.
		if err := exec("students", `DELETE FROM students WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
	}
	if scopes[ClearRoutes] {
		if err := exec("student_assignments", `DELETE FROM student_assignments WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
		if err := count("stops", `SELECT count(*) FROM stops WHERE school_id = $1`); err != nil {
			return nil, err
		}
		if err := exec("routes", `DELETE FROM routes WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
	}
	if scopes[ClearBuses] {
		if err := exec("buses", `DELETE FROM buses WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
	}
	if scopes[ClearDrivers] {
		if err := exec("drivers", `DELETE FROM drivers WHERE school_id = $1`, schoolID); err != nil {
			return nil, err
		}
	}
	if len(users) > 0 {
		// Keep audit entries and staff-visible notifications; only drop the user link.
		if err := exec("", `UPDATE audit_logs SET actor_user_id = NULL WHERE actor_user_id = ANY($1)`, users); err != nil {
			return nil, err
		}
		for _, sql := range []string{
			`UPDATE notifications SET created_by = NULL WHERE created_by = ANY($1)`,
			`UPDATE trips SET created_by = NULL WHERE created_by = ANY($1)`,
			`UPDATE trip_status_history SET changed_by = NULL WHERE changed_by = ANY($1)`,
			`UPDATE delay_events SET reported_by = NULL WHERE reported_by = ANY($1)`,
			`UPDATE emergencies SET reported_by = NULL WHERE reported_by = ANY($1)`,
		} {
			if err := exec("", sql, users); err != nil {
				return nil, err
			}
		}
		// Sessions, device tokens and inbox rows cascade.
		if err := exec("logins", `DELETE FROM users WHERE id = ANY($1)`, users); err != nil {
			return nil, err
		}
	}
	return counts, nil
}
