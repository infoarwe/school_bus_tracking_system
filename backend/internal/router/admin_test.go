package router_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// completedTrip runs a trip with GPS, stop events, a delay and notifications:
// data for every report.
func (e *env) completedTrip(f *notifyFixture) {
	e.t.Helper()
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/start", f.driver1Tok, nil)
	e.drive(f.tripFixture, f.tripID, 11.000, 11.0085, time.Now())
	e.expect(e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/delays", f.driver1Tok,
		map[string]any{"minutes": 10, "reason": "traffic", "note": `=HYPERLINK("x")`}), 201, "")
	e.expect(e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/end", f.driver1Tok, nil), 200, "")
	e.flushPushes()
}

func TestReportsAll(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	e.completedTrip(f)

	list := e.do("GET", f.base+"/reports", f.tm, nil)
	e.expect(list, 200, "")
	if n := len(list.list()); n != 8 {
		t.Fatalf("reports = %d, want 8", n)
	}
	q := "?from=" + f.today + "&to=" + f.today
	minRows := map[string]int{"trips": 1, "bus_journeys": 1, "delays": 1, "notifications": 1, "location_history": 30}
	for _, r := range list.list() {
		key := r.(map[string]any)["key"].(string)
		query := q
		if key == "location_history" {
			query = "?page_size=100&trip_id=" + f.tripID
		}
		res := e.do("GET", f.base+"/reports/"+key+query, f.tm, nil)
		e.expect(res, 200, "")
		rows := res.data()["rows"].([]any)
		cols := res.data()["report"].(map[string]any)["columns"].([]any)
		if len(rows) > 0 && len(rows[0].([]any)) != len(cols) {
			t.Errorf("%s: %d values for %d columns", key, len(rows[0].([]any)), len(cols))
		}
		if len(rows) < minRows[key] {
			t.Errorf("%s: %d rows, want at least %d", key, len(rows), minRows[key])
		}
	}
	trip := e.do("GET", f.base+"/reports/trips"+q, f.tm, nil).data()["rows"].([]any)[0].([]any)
	if trip[5] != "completed" || trip[10] != "0" || trip[11] != "1" {
		t.Errorf("trip row = %v", trip)
	}

	// CSV: Excel byte-order mark, header row, spreadsheet formula neutralised.
	req := httptest.NewRequest("GET", f.base+"/reports/delays"+q+"&format=csv", nil)
	req.Header.Set("Authorization", "Bearer "+f.tm)
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	csv := string(body)
	if rec.Code != 200 || !strings.HasPrefix(csv, "\xEF\xBB\xBFReported at,Trip,Route") {
		t.Fatalf("csv = %d %q", rec.Code, csv[:min(80, len(csv))])
	}
	if !strings.Contains(csv, `"'=HYPERLINK(""x"")"`) {
		t.Errorf("formula not neutralised: %s", csv)
	}

	// Validation and isolation.
	e.expect(e.do("GET", f.base+"/reports/trips?from=2026-02-01&to=2026-01-01", f.tm, nil), 400, "validation_failed")
	e.expect(e.do("GET", f.base+"/reports/location_history", f.tm, nil), 400, "validation_failed")
	e.expect(e.do("GET", f.base+"/reports/nope"+q, f.tm, nil), 404, "not_found")
	b := e.school("BBB")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	other := e.login("admin@b.test")
	if rows := e.do("GET", "/api/v1/schools/"+b+"/reports/location_history?trip_id="+f.tripID, other, nil).data()["rows"].([]any); len(rows) != 0 {
		t.Error("another school's trip must not appear in its reports")
	}
	e.expect(e.do("GET", f.base+"/reports/trips"+q, other, nil), 404, "not_found")
}

func TestDashboardAndReplay(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/start", f.driver1Tok, nil)
	e.drive(f.tripFixture, f.tripID, 11.000, 11.002, time.Now())
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/delays", f.driver1Tok, map[string]any{"minutes": 5, "reason": "traffic"})

	d := e.do("GET", f.base+"/dashboard", f.tm, nil)
	e.expect(d, 200, "")
	data := d.data()
	tot := data["totals"].(map[string]any)
	if tot["buses_active"] != float64(2) || tot["buses_in_maintenance"] != float64(1) || tot["students_on_transport"] != float64(2) ||
		tot["routes_active"] != float64(3) || tot["drivers_active"] != float64(2) {
		t.Errorf("totals = %v", tot)
	}
	if data["live"].(map[string]any)["on_road"] != float64(1) || data["delayed_trips"] != float64(1) {
		t.Errorf("live/delays = %v %v", data["live"], data["delayed_trips"])
	}
	if data["trips"].(map[string]any)["morning_pickup"].(map[string]any)["started"] != float64(1) {
		t.Errorf("trips = %v", data["trips"])
	}
	var compact map[string]any
	for _, r := range data["routes"].([]any) {
		if r.(map[string]any)["code"] == "RS-09" {
			compact = r.(map[string]any)
		}
	}
	if compact == nil || compact["morning_status"] != "started" || compact["students"] != float64(2) || compact["delays"] != float64(1) {
		t.Errorf("route summary = %v", compact)
	}

	if n := len(e.do("GET", f.base+"/trips/"+f.tripID+"/track", f.tm, nil).list()); n < 8 {
		t.Errorf("replay points = %d", n)
	}
}

func TestAuditLogViewer(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	e.completedTrip(f)

	all := e.do("GET", f.base+"/audit-logs?page_size=100", f.admin, nil)
	e.expect(all, 200, "")
	actions := map[string]bool{}
	for _, row := range all.list() {
		actions[row.(map[string]any)["action"].(string)] = true
	}
	for _, want := range []string{"student.create", "parent.create", "trip.create", "trip.started", "trip.delay_report", "auth.login"} {
		if !actions[want] {
			t.Errorf("school admin should see %s", want)
		}
	}
	// Transport Manager: transport entries only.
	for _, row := range e.do("GET", f.base+"/audit-logs?page_size=100", f.tm, nil).list() {
		switch et := row.(map[string]any)["entity_type"].(string); et {
		case "student", "parent", "user", "school":
			t.Errorf("Transport Manager saw a %s entry", et)
		}
	}
	for _, row := range e.do("GET", f.base+"/audit-logs?action=trip.", f.admin, nil).list() {
		if !strings.HasPrefix(row.(map[string]any)["action"].(string), "trip.") {
			t.Errorf("action filter leaked %v", row)
		}
	}
	// Another school sees nothing of this school; the platform-wide log is Super Admin only.
	b := e.school("BBB")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	e.expect(e.do("GET", f.base+"/audit-logs", e.login("admin@b.test"), nil), 404, "not_found")
	e.expect(e.do("GET", "/api/v1/audit-logs", f.admin, nil), 403, "forbidden")
}
