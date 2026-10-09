// Command seed creates demo data for local development and for the mobile team's
// shared dev environment. It is idempotent: existing records are left alone.
//
//	go run ./cmd/seed
//
// Refuses to run when APP_ENV=production.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/config"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/database"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

const demoPassword = "Admin@12345"

type seedUser struct {
	role   models.Role
	name   string
	email  string
	mobile string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() {
		return errors.New("refusing to seed demo data in production")
	}
	ctx := context.Background()
	pool, err := database.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	q := pool

	hash, err := auth.HashPassword(demoPassword)
	if err != nil {
		return err
	}

	if err := ensureUser(ctx, q, nil, seedUser{models.RoleSuperAdmin, "Super Admin", "superadmin@sbts.local", ""}, hash); err != nil {
		return err
	}

	schools := []struct {
		in    store.SchoolInput
		users []seedUser
	}{
		{
			in: store.SchoolInput{Name: "Demo Public School", Code: "DEMO", City: "Coimbatore", State: "Tamil Nadu"},
			users: []seedUser{
				{models.RoleSchoolAdmin, "Demo School Admin", "admin@demo.local", ""},
				{models.RoleTransportManager, "Demo Transport Manager", "transport@demo.local", ""},
				{models.RoleDriver, "Kumar (Demo Driver)", "", "+919000000001"},
				{models.RoleParent, "Demo Parent", "", "+919000000002"},
			},
		},
		{
			// A second school, to check that data never leaks between schools.
			in: store.SchoolInput{Name: "Second Demo School", Code: "DEMO2", City: "Chennai", State: "Tamil Nadu"},
			users: []seedUser{
				{models.RoleSchoolAdmin, "Second School Admin", "admin@demo2.local", ""},
			},
		},
	}

	for _, s := range schools {
		s.in.WorkingDays = []string{"mon", "tue", "wed", "thu", "fri"}
		s.in.Timezone = "Asia/Kolkata"
		s.in.TransportConfig = []byte(`{}`)
		var schoolID string
		school, err := store.CreateSchool(ctx, q, s.in)
		var ce *store.ConflictError
		switch {
		case errors.As(err, &ce):
			if err := q.QueryRow(ctx, `SELECT id FROM schools WHERE code = $1`, s.in.Code).Scan(&schoolID); err != nil {
				return err
			}
			fmt.Printf("school %s exists\n", s.in.Code)
		case err != nil:
			return fmt.Errorf("create school %s: %w", s.in.Code, err)
		default:
			schoolID = school.ID
			fmt.Printf("created school %s\n", s.in.Code)
		}
		for _, u := range s.users {
			if err := ensureUser(ctx, q, &schoolID, u, hash); err != nil {
				return err
			}
		}
		if s.in.Code == "DEMO" {
			if err := seedTransport(ctx, q, schoolID); err != nil {
				return err
			}
			if err := seedStudents(ctx, q, schoolID); err != nil {
				return err
			}
			if err := seedTodaysTrips(ctx, q, schoolID); err != nil {
				return err
			}
		}
	}

	fmt.Printf("\nWeb logins use password %q. Driver/parent apps log in with OTP (set OTP_DEV_CODE for a fixed code).\n", demoPassword)
	return nil
}

func ensureUser(ctx context.Context, q store.DBTX, schoolID *string, u seedUser, hash string) error {
	in := store.UserInput{SchoolID: schoolID, Role: u.role, Name: u.name}
	if u.email != "" {
		in.Email = &u.email
		in.PasswordHash = &hash
	}
	if u.mobile != "" {
		in.Mobile = &u.mobile
	}
	_, err := store.CreateUser(ctx, q, in)
	var ce *store.ConflictError
	switch {
	case errors.As(err, &ce):
		fmt.Printf("  user %s%s exists\n", u.email, u.mobile)
	case err != nil:
		return fmt.Errorf("create user %s%s: %w", u.email, u.mobile, err)
	default:
		fmt.Printf("  created %s %s%s\n", u.role, u.email, u.mobile)
	}
	return nil
}

// seedTransport adds the CLAUDE.md example: buses, a driver profile for the demo
// driver login, and route RS-01 School → Gandhipuram → Peelamedu → Hope College → Singanallur.
func seedTransport(ctx context.Context, q store.DBTX, schoolID string) error {
	// The school sits just past the last stop: Morning Pickup ends there ("School Reached").
	if _, err := q.Exec(ctx, `UPDATE schools SET latitude = 11.0050, longitude = 77.0450
		WHERE id = $1 AND latitude IS NULL`, schoolID); err != nil {
		return fmt.Errorf("seed school location: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO drivers (school_id, user_id, license_number)
		SELECT school_id, id, 'TN3820190001234' FROM users
		WHERE school_id = $1 AND role = 'driver'
		ON CONFLICT (user_id) DO NOTHING`, schoolID); err != nil {
		return fmt.Errorf("seed drivers: %w", err)
	}
	for _, b := range []struct {
		number   string
		capacity int
	}{{"TN-38-AB-1234", 40}, {"TN-38-CD-5678", 32}} {
		if _, err := q.Exec(ctx, `INSERT INTO buses (school_id, vehicle_number, capacity, make_model)
			VALUES ($1, $2, $3, 'Tata Starbus') ON CONFLICT DO NOTHING`, schoolID, b.number, b.capacity); err != nil {
			return fmt.Errorf("seed buses: %w", err)
		}
	}

	var routeID string
	err := q.QueryRow(ctx, `INSERT INTO routes (school_id, name, code, start_point)
		VALUES ($1, 'Gandhipuram - Singanallur', 'RS-01', 'School')
		ON CONFLICT (school_id, code) DO NOTHING RETURNING id`, schoolID).Scan(&routeID)
	if errors.Is(err, pgx.ErrNoRows) {
		fmt.Println("  route RS-01 exists")
		return nil
	}
	if err != nil {
		return fmt.Errorf("seed route: %w", err)
	}
	stops := []struct {
		name         string
		lat, lng     float64
		pickup, drop string
	}{
		{"Gandhipuram", 11.0168, 76.9663, "07:10", "16:40"},
		{"Peelamedu", 11.0285, 77.0028, "07:25", "16:25"},
		{"Hope College", 11.0263, 77.0185, "07:35", "16:15"},
		{"Singanallur", 10.9985, 77.0322, "07:45", "16:05"},
	}
	for i, s := range stops {
		if _, err := q.Exec(ctx, `INSERT INTO stops (school_id, route_id, name, latitude, longitude, sequence, pickup_time, drop_time)
			VALUES ($1, $2, $3, $4, $5, $6, $7::time, $8::time)`,
			schoolID, routeID, s.name, s.lat, s.lng, i+1, s.pickup, s.drop); err != nil {
			return fmt.Errorf("seed stop %s: %w", s.name, err)
		}
	}
	fmt.Println("  created buses, driver profile, route RS-01 with 4 stops")
	return nil
}

// seedStudents adds three students on RS-01; the demo parent login has two of them.
func seedStudents(ctx context.Context, q store.DBTX, schoolID string) error {
	if _, err := q.Exec(ctx, `
		INSERT INTO parents (school_id, user_id)
		SELECT school_id, id FROM users WHERE school_id = $1 AND role = 'parent'
		ON CONFLICT (user_id) DO NOTHING`, schoolID); err != nil {
		return fmt.Errorf("seed parents: %w", err)
	}
	students := []struct {
		admission, name, class, section, stop string
		demoParentChild                       bool
	}{
		{"DPS-1001", "Asha Ravi", "5", "A", "Peelamedu", true},
		{"DPS-1002", "Arjun Ravi", "2", "B", "Peelamedu", true},
		{"DPS-1003", "Divya Kumar", "7", "A", "Hope College", false},
	}
	for _, st := range students {
		var id string
		err := q.QueryRow(ctx, `INSERT INTO students (school_id, admission_no, name, class, section)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT (school_id, admission_no) DO NOTHING RETURNING id`,
			schoolID, st.admission, st.name, st.class, st.section).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // already seeded
		}
		if err != nil {
			return fmt.Errorf("seed student %s: %w", st.admission, err)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO student_assignments (school_id, student_id, route_id, pickup_stop_id, drop_stop_id)
			SELECT $1, $2, r.id, s.id, s.id FROM routes r JOIN stops s ON s.route_id = r.id
			WHERE r.school_id = $1 AND r.code = 'RS-01' AND s.name = $3`, schoolID, id, st.stop); err != nil {
			return fmt.Errorf("seed assignment %s: %w", st.admission, err)
		}
		if st.demoParentChild {
			if _, err := q.Exec(ctx, `
				INSERT INTO parent_students (parent_id, student_id, school_id, relationship)
				SELECT p.id, $2, $1, 'father' FROM parents p JOIN users u ON u.id = p.user_id
				WHERE p.school_id = $1 AND u.mobile = '+919000000002'`, schoolID, id); err != nil {
				return fmt.Errorf("seed parent link %s: %w", st.admission, err)
			}
		}
		fmt.Printf("  created student %s %s\n", st.admission, st.name)
	}
	return nil
}

// seedTodaysTrips assigns today's RS-01 Morning Pickup and Evening Drop to the demo
// driver and bus (the CLAUDE.md example). Re-running the seed on another day adds that day's trips.
func seedTodaysTrips(ctx context.Context, q store.DBTX, schoolID string) error {
	tag, err := q.Exec(ctx, `
		INSERT INTO trips (school_id, trip_date, trip_type, route_id, bus_id, driver_id)
		SELECT s.id, (now() AT TIME ZONE s.timezone)::date, tt.trip_type, r.id, b.id, d.id
		FROM schools s
		JOIN routes r ON r.school_id = s.id AND r.code = 'RS-01'
		JOIN buses b ON b.school_id = s.id AND b.vehicle_number = 'TN-38-AB-1234'
		JOIN drivers d ON d.school_id = s.id
		JOIN users u ON u.id = d.user_id AND u.mobile = '+919000000001'
		CROSS JOIN (VALUES ('morning_pickup'), ('evening_drop')) AS tt(trip_type)
		WHERE s.id = $1
		ON CONFLICT DO NOTHING`, schoolID)
	if err != nil {
		return fmt.Errorf("seed trips: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO trip_status_history (trip_id, school_id, to_status, reason)
		SELECT t.id, t.school_id, 'scheduled', 'seed' FROM trips t
		WHERE t.school_id = $1 AND NOT EXISTS (SELECT 1 FROM trip_status_history h WHERE h.trip_id = t.id)`, schoolID); err != nil {
		return fmt.Errorf("seed trip history: %w", err)
	}
	fmt.Printf("  today's trips added: %d\n", tag.RowsAffected())
	return nil
}
