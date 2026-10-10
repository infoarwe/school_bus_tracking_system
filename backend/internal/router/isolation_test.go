package router_test

// S9-02: tenant isolation across every endpoint and every role.
//
// The suite walks the real router, so a new endpoint fails TestTenantIsolationAllEndpoints
// until it is classified below. For each school-owned or caller-scoped endpoint it checks,
// as every role of school A, that school B's data can be neither read nor changed:
//   - /schools/{B}/... answers 404 (another school looks like it does not exist);
//   - A's own paths with B's record IDs answer exactly like random IDs (never revealing that
//     B's records exist) and never succeed;
//   - afterwards every row of school B is byte-for-byte unchanged.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// tenant is a fully set-up school: master data, a started trip, alerts, and a token per role.
type tenant struct {
	id, base string
	tokens   map[models.Role]string
	ids      map[string]string // path parameter name → one of this school's records
	stops    []string
}

func newTenant(e *env, code string, n int) *tenant {
	e.t.Helper()
	ctx := context.Background()
	lc := strings.ToLower(code)
	t := &tenant{id: e.school(code), tokens: map[models.Role]string{}, ids: map[string]string{}}
	t.base = "/api/v1/schools/" + t.id
	t.ids["userID"] = e.user(&t.id, models.RoleSchoolAdmin, "admin@"+lc+".test", "")
	e.user(&t.id, models.RoleTransportManager, "tm@"+lc+".test", "")
	admin := e.login("admin@" + lc + ".test")
	t.tokens[models.RoleSchoolAdmin], t.tokens[models.RoleTransportManager] = admin, e.login("tm@"+lc+".test")

	create := func(path string, body any) string {
		r := e.do("POST", t.base+path, admin, body)
		e.expect(r, 201, "")
		return r.data()["id"].(string)
	}
	t.ids["routeID"], t.stops = e.routeWithStops(admin, t.id, "R-"+code, false, "One", "Two", "Three")
	t.ids["stopID"] = t.stops[0]
	t.ids["busID"] = create("/buses", map[string]any{"vehicle_number": "TN-38-" + code + "-1234", "capacity": 40})
	driverMobile := fmt.Sprintf("9%d00000001", n)
	t.ids["driverID"] = create("/drivers", map[string]any{"name": "Driver " + code, "mobile": driverMobile})
	t.ids["studentID"] = create("/students", map[string]any{"admission_no": code + "-1", "name": "Child " + code})
	e.expect(e.do("PUT", t.base+"/students/"+t.ids["studentID"]+"/assignment", admin, map[string]any{
		"route_id": t.ids["routeID"], "pickup_stop_id": t.stops[1], "drop_stop_id": t.stops[1]}), 200, "")
	parentMobile := fmt.Sprintf("9%d00000002", n)
	t.ids["parentID"] = create("/parents", map[string]any{"name": "Parent " + code, "mobile": parentMobile,
		"children": []map[string]string{{"student_id": t.ids["studentID"]}}})
	t.tokens[models.RoleDriver] = e.otpLogin(driverMobile, "driver")
	t.tokens[models.RoleParent] = e.otpLogin(parentMobile, "parent")

	t.ids["tripID"] = create("/trips", map[string]any{"trip_date": e.dateIn(t.id, 0), "trip_type": "morning_pickup",
		"route_id": t.ids["routeID"], "bus_id": t.ids["busID"], "driver_id": t.ids["driverID"]})
	drv := t.tokens[models.RoleDriver]
	e.expect(e.do("POST", "/api/v1/driver/trips/"+t.ids["tripID"]+"/confirm", drv, nil), 200, "")
	e.expect(e.do("POST", "/api/v1/driver/trips/"+t.ids["tripID"]+"/start", drv, nil), 200, "")
	em := e.do("POST", "/api/v1/driver/trips/"+t.ids["tripID"]+"/emergency", drv, map[string]any{"message": "test"})
	e.expect(em, 201, "")
	t.ids["emergencyID"] = em.data()["id"].(string)
	t.ids["announcementID"] = create("/announcements", map[string]any{"category": "other", "target": "school",
		"title": "Notice " + code, "message": "Hello", "scheduled_at": time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)})

	var sessionID string
	if err := e.pool.QueryRow(ctx, `SELECT id FROM user_sessions WHERE user_id = $1 LIMIT 1`, t.ids["userID"]).
		Scan(&sessionID); err != nil {
		e.t.Fatal(err)
	}
	t.ids["sessionID"] = sessionID
	t.ids["report"] = "trips"
	return t
}

// Route classes. Every route the router serves must be in exactly one.
var (
	publicRoutes = map[string]bool{ // no token, or authenticates itself
		"/health": true, "/docs": true, "/openapi.yaml": true, "/api/v1/ws": true,
		"/api/v1/auth/login": true, "/api/v1/auth/login/2fa": true, "/api/v1/auth/otp/send": true,
		"/api/v1/auth/otp/verify": true, "/api/v1/auth/refresh": true, "/api/v1/branding/logos/{file}": true,
	}
	superAdminRoutes = map[string]bool{"/api/v1/schools": true, "/api/v1/audit-logs": true}
	callerPrefixes   = []string{"/api/v1/auth/", "/api/v1/devices", "/api/v1/branding", "/api/v1/driver/", "/api/v1/parent/"}
	paramRe          = regexp.MustCompile(`\{([A-Za-z]+)\}`)
)

type endpoint struct{ method, pattern, class string }

func classify(pattern string) string {
	p := strings.TrimSuffix(pattern, "/")
	switch {
	case publicRoutes[p]:
		return "public"
	case superAdminRoutes[p]:
		return "super"
	case strings.HasPrefix(p, "/api/v1/schools/{schoolID}"):
		return "school"
	}
	for _, prefix := range callerPrefixes {
		if strings.HasPrefix(p, prefix) {
			return "caller"
		}
	}
	return ""
}

func endpoints(t *testing.T, h http.Handler) []endpoint {
	t.Helper()
	var out []endpoint
	err := chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		class := classify(route)
		if class == "" {
			t.Errorf("%s %s is not classified for the tenant-isolation suite: add it in isolation_test.go", method, route)
		}
		out = append(out, endpoint{method, route, class})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// path fills a route pattern with the school and record IDs.
func fillPath(pattern, schoolID string, ids map[string]string) string {
	p := paramRe.ReplaceAllStringFunc(strings.TrimSuffix(pattern, "/"), func(m string) string {
		name := m[1 : len(m)-1]
		if name == "schoolID" {
			return schoolID
		}
		return ids[name]
	})
	return p
}

// bodyFor is a request body that would be valid for the endpoint, so a missing
// tenant check would actually be reached (not masked by a validation error).
func (e *env) bodyFor(ep endpoint, owner *tenant, reps map[string]map[string]any) any {
	if ep.method == "GET" || ep.method == "DELETE" {
		return nil
	}
	p := strings.TrimSuffix(ep.pattern, "/")
	switch {
	case strings.HasSuffix(p, "/status"):
		status := "inactive"
		if strings.Contains(p, "/users/") {
			status = "suspended"
		}
		return map[string]any{"status": status}
	case strings.HasSuffix(p, "/password"):
		return map[string]any{"password": "NewPassword@123"}
	case strings.HasSuffix(p, "/cancel"):
		return map[string]any{"reason": "isolation test"}
	case strings.HasSuffix(p, "/override"):
		return map[string]any{"status": "completed", "reason": "isolation test"}
	case strings.HasSuffix(p, "/delays"):
		return map[string]any{"minutes": 10, "reason": "traffic"}
	case strings.HasSuffix(p, "/stops/order"):
		return map[string]any{"stop_ids": owner.stops}
	case strings.HasSuffix(p, "/stops"), strings.HasSuffix(p, "/stops/{stopID}"):
		return map[string]any{"name": "Changed", "latitude": 11.02, "longitude": 76.97}
	case strings.HasSuffix(p, "/assignment"):
		return map[string]any{"route_id": owner.ids["routeID"], "pickup_stop_id": owner.stops[0], "drop_stop_id": owner.stops[0]}
	case strings.HasSuffix(p, "/locations"):
		return map[string]any{"points": []any{point(11.01, 76.96, 0)}}
	case ep.method == "PUT":
		// Update: the record's own current representation is a valid body.
		if m := paramRe.FindAllStringSubmatch(p, -1); len(m) > 0 {
			if rep, ok := reps[m[len(m)-1][1]]; ok {
				return rep
			}
		}
	}
	return map[string]any{}
}

// snapshot fingerprints every row that belongs to a school.
func (e *env) snapshot(schoolID string) map[string]string {
	e.t.Helper()
	ctx := context.Background()
	rows, err := e.pool.Query(ctx, `SELECT table_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'school_id' ORDER BY table_name`)
	if err != nil {
		e.t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()
	queries := map[string]string{
		"schools":       `SELECT md5(coalesce(string_agg(t::text, ',' ORDER BY t::text), '')) FROM schools t WHERE id = $1`,
		"user_sessions": `SELECT md5(coalesce(string_agg(t::text, ',' ORDER BY t::text), '')) FROM user_sessions t JOIN users u ON u.id = t.user_id WHERE u.school_id = $1`,
	}
	for _, tbl := range tables {
		queries[tbl] = fmt.Sprintf(`SELECT md5(coalesce(string_agg(t::text, ',' ORDER BY t::text), '')) FROM %q t WHERE school_id = $1`, tbl)
	}
	out := map[string]string{}
	for tbl, q := range queries {
		var sum string
		if err := e.pool.QueryRow(ctx, q, schoolID).Scan(&sum); err != nil {
			e.t.Fatalf("snapshot %s: %v", tbl, err)
		}
		out[tbl] = sum
	}
	if len(tables) < 10 {
		e.t.Fatalf("expected many school-owned tables, found %v", tables)
	}
	return out
}

var allRoles = []models.Role{models.RoleSchoolAdmin, models.RoleTransportManager, models.RoleDriver, models.RoleParent}

func TestTenantIsolationAllEndpoints(t *testing.T) {
	e := newEnv(t)
	// Hundreds of requests from one test client: lift the per-IP and per-user limits.
	e.api.Limiter.Rules.IP.Limit, e.api.Limiter.Rules.User.Limit = 0, 0

	a, b := newTenant(e, "AAA", 1), newTenant(e, "BBB", 2)
	random := map[string]string{"report": "trips"}
	for k := range b.ids {
		if k != "report" {
			random[k] = uuid.NewString()
		}
	}
	// Current representations of B's records (valid PUT bodies), fetched before the snapshot.
	reps := map[string]map[string]any{}
	for param, kind := range map[string]string{"userID": "users", "driverID": "drivers", "busID": "buses",
		"routeID": "routes", "studentID": "students", "parentID": "parents", "tripID": "trips"} {
		r := e.do("GET", b.base+"/"+kind+"/"+b.ids[param], b.tokens[models.RoleSchoolAdmin], nil)
		e.expect(r, 200, "")
		reps[param] = r.data()
	}

	// Positive control: every A token works on A's own data.
	e.expect(e.do("GET", a.base+"/users", a.tokens[models.RoleSchoolAdmin], nil), 200, "")
	e.expect(e.do("GET", a.base+"/drivers", a.tokens[models.RoleTransportManager], nil), 200, "")
	e.expect(e.do("GET", "/api/v1/driver/trips/"+a.ids["tripID"], a.tokens[models.RoleDriver], nil), 200, "")
	e.expect(e.do("GET", "/api/v1/parent/children/"+a.ids["studentID"], a.tokens[models.RoleParent], nil), 200, "")

	before := e.snapshot(b.id)
	eps := endpoints(t, e.srv)
	counts := map[string]int{}

	for _, ep := range eps {
		counts[ep.class]++
		params := paramRe.FindAllStringSubmatch(ep.pattern, -1)
		hasRecordID := false
		for _, m := range params {
			if m[1] != "schoolID" && m[1] != "report" {
				hasRecordID = true
			}
		}
		for _, role := range allRoles {
			tok := a.tokens[role]
			switch ep.class {
			case "super":
				r := e.do(ep.method, fillPath(ep.pattern, "", nil), tok, map[string]any{})
				if r.Status != 403 {
					t.Errorf("%s %s as %s: got %d, want 403", ep.method, ep.pattern, role, r.Status)
				}

			case "school":
				// 1. School B's paths do not exist for school A.
				r := e.do(ep.method, fillPath(ep.pattern, b.id, b.ids), tok, e.bodyFor(ep, b, reps))
				if r.Status != 404 || r.errCode() != "not_found" {
					t.Errorf("%s %s as %s of A on school B: got %d %q, want 404 not_found",
						ep.method, ep.pattern, role, r.Status, r.errCode())
				}
				// 2. B's record IDs under A's path behave exactly like IDs that do not exist.
				if hasRecordID {
					e.compareWithRandom(ep, role, tok, a.id, b, random, reps)
				}

			case "caller":
				if hasRecordID {
					e.compareWithRandom(ep, role, tok, a.id, b, random, reps)
				}
			}
		}
	}

	// App-role routes refuse the other roles.
	for _, ep := range eps {
		for prefix, owner := range map[string]models.Role{"/api/v1/driver/": models.RoleDriver, "/api/v1/parent/": models.RoleParent} {
			if !strings.HasPrefix(ep.pattern, prefix) {
				continue
			}
			for _, role := range allRoles {
				if role == owner {
					continue
				}
				r := e.do(ep.method, fillPath(ep.pattern, a.id, a.ids), a.tokens[role], e.bodyFor(ep, a, nil))
				if r.Status != 403 {
					t.Errorf("%s %s as %s: got %d, want 403", ep.method, ep.pattern, role, r.Status)
				}
			}
		}
	}

	// Live updates: A's driver, parent and staff cannot watch B's trip.
	srv := httptest.NewServer(e.srv)
	t.Cleanup(srv.Close)
	for _, role := range allRoles {
		ws, _, err := e.dialWS(srv, a.tokens[role], "")
		if err != nil {
			t.Fatalf("ws as %s: %v", role, err)
		}
		ws.send(map[string]any{"type": "subscribe", "channel": "trip", "trip_id": b.ids["tripID"]})
		if m := ws.next("error"); m["code"] != "not_found" {
			t.Errorf("ws subscribe to B's trip as %s of A: %v", role, m)
		}
	}

	after := e.snapshot(b.id)
	for tbl, sum := range before {
		if after[tbl] != sum {
			t.Errorf("school B's %s rows changed during the sweep", tbl)
		}
	}
	if counts["school"] < 60 || counts["caller"] < 20 {
		t.Errorf("suspiciously few endpoints swept: %v", counts)
	}
	t.Logf("swept endpoints by class: %v × %d roles", counts, len(allRoles))
}

// compareWithRandom calls the endpoint under A's school with B's record IDs and
// with random IDs: the answers must be the same, and must not be a success.
func (e *env) compareWithRandom(ep endpoint, role models.Role, tok, schoolA string, b *tenant,
	random map[string]string, reps map[string]map[string]any) {
	e.t.Helper()
	body := e.bodyFor(ep, b, reps)
	withB := e.do(ep.method, fillPath(ep.pattern, schoolA, b.ids), tok, body)
	withRandom := e.do(ep.method, fillPath(ep.pattern, schoolA, random), tok, body)
	if withB.Status != withRandom.Status || withB.errCode() != withRandom.errCode() {
		e.t.Errorf("%s %s as %s of A: B's IDs answer %d %q but unknown IDs answer %d %q (reveals B's records)",
			ep.method, ep.pattern, role, withB.Status, withB.errCode(), withRandom.Status, withRandom.errCode())
	}
	if withB.Status < 300 {
		e.t.Errorf("%s %s as %s of A succeeded (%d) with school B's IDs", ep.method, ep.pattern, role, withB.Status)
	}
}

// Records referenced in request bodies must belong to the caller's school too.
func TestCrossTenantReferences(t *testing.T) {
	e := newEnv(t)
	a, b := newTenant(e, "AAA", 1), newTenant(e, "BBB", 2)
	admin := a.tokens[models.RoleSchoolAdmin]
	before := e.snapshot(b.id)

	for _, c := range []struct {
		name, method, path string
		body               map[string]any
	}{
		{"trip with B's route, bus and driver", "POST", "/trips", map[string]any{"trip_date": e.dateIn(a.id, 1),
			"trip_type": "evening_drop", "route_id": b.ids["routeID"], "bus_id": b.ids["busID"], "driver_id": b.ids["driverID"]}},
		{"trip with B's bus", "POST", "/trips", map[string]any{"trip_date": e.dateIn(a.id, 1),
			"trip_type": "evening_drop", "route_id": a.ids["routeID"], "bus_id": b.ids["busID"], "driver_id": a.ids["driverID"]}},
		{"trip with B's driver", "POST", "/trips", map[string]any{"trip_date": e.dateIn(a.id, 1),
			"trip_type": "evening_drop", "route_id": a.ids["routeID"], "bus_id": a.ids["busID"], "driver_id": b.ids["driverID"]}},
		{"student assigned to B's route", "PUT", "/students/" + a.ids["studentID"] + "/assignment",
			map[string]any{"route_id": b.ids["routeID"], "pickup_stop_id": b.stops[0], "drop_stop_id": b.stops[0]}},
		{"student assigned to B's stop on A's route", "PUT", "/students/" + a.ids["studentID"] + "/assignment",
			map[string]any{"route_id": a.ids["routeID"], "pickup_stop_id": b.stops[0], "drop_stop_id": b.stops[0]}},
		{"parent linked to B's student", "POST", "/parents", map[string]any{"name": "X", "mobile": "9300000009",
			"children": []map[string]string{{"student_id": b.ids["studentID"]}}}},
		{"parent updated to B's student", "PUT", "/parents/" + a.ids["parentID"], map[string]any{"name": "X",
			"mobile": "9100000002", "children": []map[string]string{{"student_id": b.ids["studentID"]}}}},
		{"announcement to B's route", "POST", "/announcements", map[string]any{"category": "other", "target": "route",
			"route_id": b.ids["routeID"], "title": "x", "message": "x"}},
		{"stop order with B's stops", "PUT", "/routes/" + a.ids["routeID"] + "/stops/order",
			map[string]any{"stop_ids": b.stops}},
	} {
		r := e.do(c.method, a.base+c.path, admin, c.body)
		if r.Status < 400 || r.Status >= 500 {
			t.Errorf("%s: got %d %v, want a 4xx refusal", c.name, r.Status, r.Body)
		}
	}

	after := e.snapshot(b.id)
	for tbl, sum := range before {
		if after[tbl] != sum {
			t.Errorf("school B's %s rows changed", tbl)
		}
	}
}
