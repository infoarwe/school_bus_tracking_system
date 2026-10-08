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
