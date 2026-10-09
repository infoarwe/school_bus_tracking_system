package router_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// upload posts a CSV as multipart form field "file".
func (e *env) upload(path, token, csv string) resp {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "students.csv")
	_, _ = fw.Write([]byte(csv))
	_ = mw.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	out := resp{Status: rec.Code}
	_ = json.Unmarshal(rec.Body.Bytes(), &out.Body)
	return out
}

// otpLogin logs an app user in with the dev OTP.
func (e *env) otpLogin(mobile, app string) string {
	e.t.Helper()
	e.expect(e.do("POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": app}), 200, "")
	r := e.do("POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": app, "code": devOTP})
	e.expect(r, 200, "")
	return r.data()["access_token"].(string)
}

// routeWithStops creates a route and its stops, returning the route ID and stop IDs in order.
func (e *env) routeWithStops(tok, schoolID, code string, pickupOnly bool, names ...string) (string, []string) {
	e.t.Helper()
	base := "/api/v1/schools/" + schoolID + "/routes"
	body := map[string]any{"name": code, "code": code}
	if pickupOnly {
		body["supports_drop"] = false
	}
	r := e.do("POST", base, tok, body)
	e.expect(r, 201, "")
	id := r.data()["id"].(string)
	var stops []string
	for _, n := range names {
		s := e.do("POST", base+"/"+id+"/stops", tok, map[string]any{"name": n, "latitude": 11.01, "longitude": 76.96})
		e.expect(s, 201, "")
		stops = append(stops, s.data()["id"].(string))
	}
	return id, stops
}

func TestStudentAndParentAccess(t *testing.T) {
	e := newEnv(t)
	a, b := e.school("AAA"), e.school("BBB")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	admin, tm, other := e.login("admin@a.test"), e.login("tm@a.test"), e.login("admin@b.test")
	base := "/api/v1/schools/" + a

	st := e.do("POST", base+"/students", admin, map[string]any{
		"admission_no": "a-101", "name": "Asha", "class": "5", "section": "b", "notes": "allergic to peanuts",
	})
	e.expect(st, 201, "")
	if st.data()["admission_no"] != "A-101" || st.data()["section"] != "B" {
		t.Errorf("not normalized: %v", st.data())
	}
	studentID := st.data()["id"].(string)
	e.expect(e.do("POST", base+"/students", admin, map[string]any{"admission_no": "A-101", "name": "Dup"}), 409, "conflict")

	pa := e.do("POST", base+"/parents", admin, map[string]any{
		"name": "Ravi", "mobile": "9000000002",
		"children": []map[string]string{{"student_id": studentID, "relationship": "father"}},
	})
	e.expect(pa, 201, "")
	parentID := pa.data()["id"].(string)
	if kids := pa.data()["children"].([]any); len(kids) != 1 {
		t.Errorf("children = %v", kids)
	}

	// Transport Manager: may view, without admin-only notes; may not change anything.
	view := e.do("GET", base+"/students/"+studentID, tm, nil)
	e.expect(view, 200, "")
	if _, has := view.data()["notes"]; has {
		t.Error("Transport Manager should not see student notes")
	}
	if n := len(view.data()["parents"].([]any)); n != 1 {
		t.Errorf("parents on student = %d", n)
	}
	if adminView := e.do("GET", base+"/students/"+studentID, admin, nil); adminView.data()["notes"] != "allergic to peanuts" {
		t.Errorf("School Admin should see notes: %v", adminView.data()["notes"])
	}
	e.expect(e.do("GET", base+"/parents", tm, nil), 200, "")
	e.expect(e.do("POST", base+"/students", tm, map[string]any{"admission_no": "X", "name": "X"}), 403, "forbidden")
	e.expect(e.do("PUT", base+"/students/"+studentID+"/assignment", tm, map[string]any{}), 403, "forbidden")

	// Another school sees nothing.
	for _, path := range []string{base + "/students/" + studentID, base + "/parents/" + parentID,
		"/api/v1/schools/" + b + "/students/" + studentID, "/api/v1/schools/" + b + "/parents/" + parentID} {
		e.expect(e.do("GET", path, other, nil), 404, "not_found")
	}
	// Linking a student of another school is rejected.
	stB := e.do("POST", "/api/v1/schools/"+b+"/students", other, map[string]any{"admission_no": "B-1", "name": "Bala"})
	e.expect(e.do("PUT", base+"/parents/"+parentID, admin, map[string]any{
		"name": "Ravi", "mobile": "9000000002", "children": []map[string]string{{"student_id": stB.data()["id"].(string)}},
	}), 400, "validation_failed")

	// Deactivating a parent blocks their app login.
	ptok := e.otpLogin("9000000002", "parent")
	e.expect(e.do("PATCH", base+"/parents/"+parentID+"/status", admin, map[string]string{"status": "inactive"}), 200, "")
	e.expect(e.do("GET", "/api/v1/parent/children", ptok, nil), 401, "session_revoked")
}

func TestStudentAssignmentRules(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	admin := e.login("admin@a.test")
	base := "/api/v1/schools/" + a

	route1, stops1 := e.routeWithStops(admin, a, "RS-01", false, "Gandhipuram", "Peelamedu")
	route2, stops2 := e.routeWithStops(admin, a, "RS-02", true, "Ukkadam")
	st := e.do("POST", base+"/students", admin, map[string]any{"admission_no": "A-1", "name": "Asha"})
	id := st.data()["id"].(string)
	assign := base + "/students/" + id + "/assignment"

	// A stop from another route is rejected (rule 5).
	e.expect(e.do("PUT", assign, admin, map[string]any{"route_id": route1, "pickup_stop_id": stops2[0], "drop_stop_id": stops1[0]}), 400, "validation_failed")
	// RS-01 runs both trips, so both stops are needed.
	e.expect(e.do("PUT", assign, admin, map[string]any{"route_id": route1, "pickup_stop_id": stops1[0]}), 400, "validation_failed")
	// RS-02 is pickup-only: a drop stop is not allowed.
	e.expect(e.do("PUT", assign, admin, map[string]any{"route_id": route2, "pickup_stop_id": stops2[0], "drop_stop_id": stops2[0]}), 400, "validation_failed")

	ok := e.do("PUT", assign, admin, map[string]any{"route_id": route1, "pickup_stop_id": stops1[0], "drop_stop_id": stops1[1]})
	e.expect(ok, 200, "")
	as := ok.data()["assignment"].(map[string]any)
	if as["route_code"] != "RS-01" || as["pickup_stop"].(map[string]any)["name"] != "Gandhipuram" {
		t.Errorf("assignment = %v", as)
	}
	// Same assignment again adds no history; a change does.
	e.do("PUT", assign, admin, map[string]any{"route_id": route1, "pickup_stop_id": stops1[0], "drop_stop_id": stops1[1]})
	e.expect(e.do("PUT", assign, admin, map[string]any{"route_id": route2, "pickup_stop_id": stops2[0]}), 200, "")
	hist := e.do("GET", base+"/students/"+id+"/assignments", admin, nil)
	if n := len(hist.list()); n != 2 {
		t.Fatalf("history entries = %d, want 2", n)
	}
	if hist.list()[0].(map[string]any)["ended_at"] != nil || hist.list()[1].(map[string]any)["ended_at"] == nil {
		t.Errorf("newest should be open, older closed: %v", hist.list())
	}

	// The route page lists the student; their stop cannot be deleted.
	rs := e.do("GET", base+"/routes/"+route2+"/students", admin, nil)
	if len(rs.list()) != 1 {
		t.Errorf("route students = %v", rs.list())
	}
	e.expect(e.do("DELETE", base+"/routes/"+route2+"/stops/"+stops2[0], admin, nil), 409, "stop_in_use")

	// Filters: assigned / by route.
	if n := len(e.do("GET", base+"/students?route_id="+route2, admin, nil).list()); n != 1 {
		t.Errorf("route filter = %d", n)
	}
	if n := len(e.do("GET", base+"/students?assigned=no", admin, nil).list()); n != 0 {
		t.Errorf("unassigned filter = %d", n)
	}

	// Opting out of transport ends the assignment, and blocks new ones.
	e.expect(e.do("PUT", base+"/students/"+id, admin, map[string]any{"admission_no": "A-1", "name": "Asha", "transport_status": "not_using"}), 200, "")
	if got := e.do("GET", base+"/students/"+id, admin, nil).data()["assignment"]; got != nil {
		t.Errorf("assignment should end: %v", got)
	}
	e.expect(e.do("PUT", assign, admin, map[string]any{"route_id": route2, "pickup_stop_id": stops2[0]}), 400, "validation_failed")
	e.expect(e.do("DELETE", base+"/routes/"+route2+"/stops/"+stops2[0], admin, nil), 204, "")
}

func TestParentAndDriverAppEndpoints(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	admin := e.login("admin@a.test")
	base := "/api/v1/schools/" + a

	route, stops := e.routeWithStops(admin, a, "RS-01", false, "Gandhipuram", "Peelamedu")
	mine := e.do("POST", base+"/students", admin, map[string]any{"admission_no": "A-1", "name": "Asha"}).data()["id"].(string)
	notMine := e.do("POST", base+"/students", admin, map[string]any{"admission_no": "A-2", "name": "Bala"}).data()["id"].(string)
	e.do("PUT", base+"/students/"+mine+"/assignment", admin, map[string]any{"route_id": route, "pickup_stop_id": stops[1], "drop_stop_id": stops[1]})
	e.expect(e.do("POST", base+"/parents", admin, map[string]any{
		"name": "Ravi", "mobile": "9000000002", "children": []map[string]string{{"student_id": mine, "relationship": "father"}},
	}), 201, "")

	ptok := e.otpLogin("9000000002", "parent")
	kids := e.do("GET", "/api/v1/parent/children", ptok, nil)
	e.expect(kids, 200, "")
	if len(kids.list()) != 1 || kids.list()[0].(map[string]any)["name"] != "Asha" {
		t.Fatalf("parent sees %v", kids.list())
	}
	detail := e.do("GET", "/api/v1/parent/children/"+mine, ptok, nil)
	e.expect(detail, 200, "")
	if n := len(detail.data()["route_stops"].([]any)); n != 2 {
		t.Errorf("route_stops = %d", n)
	}
	if pick := detail.data()["assignment"].(map[string]any)["pickup_stop"].(map[string]any); pick["name"] != "Peelamedu" {
		t.Errorf("pickup stop = %v", pick)
	}
	// Other students do not exist for this parent (rule 6), and admin APIs are closed.
	e.expect(e.do("GET", "/api/v1/parent/children/"+notMine, ptok, nil), 404, "not_found")
	e.expect(e.do("GET", base+"/students", ptok, nil), 403, "forbidden")
	e.expect(e.do("GET", "/api/v1/driver/me", ptok, nil), 403, "forbidden")

	e.expect(e.do("POST", base+"/drivers", admin, map[string]any{"name": "Kumar", "mobile": "9000000001"}), 201, "")
	dtok := e.otpLogin("9000000001", "driver")
	me := e.do("GET", "/api/v1/driver/me", dtok, nil)
	e.expect(me, 200, "")
	if me.data()["name"] != "Kumar" {
		t.Errorf("driver me = %v", me.data())
	}
	e.expect(e.do("GET", "/api/v1/parent/children", dtok, nil), 403, "forbidden")
}

func TestStudentImport(t *testing.T) {
	e := newEnv(t)
	a := e.school("AAA")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	admin := e.login("admin@a.test")
	e.routeWithStops(admin, a, "RS-01", false, "Gandhipuram", "Peelamedu")
	importURL := "/api/v1/schools/" + a + "/students/import"

	bad := "Admission No,Student Name,Class,Section,Parent Name,Parent Mobile,Route Code,Pickup Stop\n" +
		"A-1,Asha,5,A,Ravi,12345,RS-01,Gandhipuram\n" +
		"A-2,Bala,5,A,Meena,9000000003,RS-99,1\n" +
		",NoNumber,5,A,,,,\n"
	r := e.upload(importURL, admin, bad)
	e.expect(r, 200, "")
	if r.data()["imported"] != false || len(r.data()["errors"].([]any)) != 3 {
		t.Fatalf("expected 3 row errors and nothing imported: %v", r.data())
	}
	// Failed rows must not be counted, even if they got partway before failing.
	if r.data()["students_created"] != float64(0) || r.data()["parents_created"] != float64(0) {
		t.Errorf("failed rows were counted: %v", r.data())
	}
	if n := len(e.do("GET", "/api/v1/schools/"+a+"/students", admin, nil).list()); n != 0 {
		t.Fatalf("a failed import must save nothing, found %d students", n)
	}

	// Siblings share a parent; one stop given means pickup = drop; stop by number works.
	good := "admission_no,student_name,class,section,parent_name,parent_mobile,parent_relationship,parent2_name,parent2_mobile,route_code,pickup_stop,drop_stop\n" +
		"A-1,Asha,5,A,Ravi,9000000002,father,Meena,9000000003,RS-01,Peelamedu,\n" +
		"A-2,Bala,3,B,Ravi,9000000002,father,,,RS-01,1,2\n"
	dry := e.upload(importURL+"?dry_run=true", admin, good)
	if dry.data()["imported"] != false || len(dry.data()["errors"].([]any)) != 0 || dry.data()["students_created"] != float64(2) {
		t.Fatalf("dry run: %v", dry.data())
	}
	if n := len(e.do("GET", "/api/v1/schools/"+a+"/students", admin, nil).list()); n != 0 {
		t.Fatal("dry run must not save")
	}
	done := e.upload(importURL, admin, good)
	d := done.data()
	if d["imported"] != true || d["students_created"] != float64(2) || d["parents_created"] != float64(2) ||
		d["parent_links"] != float64(3) || d["assignments_set"] != float64(2) {
		t.Fatalf("import result: %v", d)
	}
	ravi := e.do("GET", "/api/v1/schools/"+a+"/parents?q=Ravi", admin, nil).list()[0].(map[string]any)
	if n := len(ravi["children"].([]any)); n != 2 {
		t.Errorf("Ravi should have 2 children, has %d", n)
	}
	asha := e.do("GET", "/api/v1/schools/"+a+"/students?q=Asha", admin, nil).list()[0].(map[string]any)["assignment"].(map[string]any)
	if asha["pickup_stop"].(map[string]any)["name"] != "Peelamedu" || asha["drop_stop"].(map[string]any)["name"] != "Peelamedu" {
		t.Errorf("Asha's stops: %v", asha)
	}

	// Re-importing the same file changes nothing.
	again := e.upload(importURL, admin, good).data()
	if again["students_created"] != float64(0) || again["students_updated"] != float64(0) || again["assignments_set"] != float64(0) {
		t.Errorf("re-import should be a no-op: %v", again)
	}

	// Transport Managers cannot import.
	e.expect(e.upload(importURL, e.login("tm@a.test"), good), 403, "forbidden")
	e.expect(e.upload(importURL, admin, "name\nx\n"), 400, "validation_failed")
}
