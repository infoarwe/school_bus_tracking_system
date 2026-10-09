package router_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
)

// fakePush records pushes instead of calling FCM.
type fakePush struct {
	mu   sync.Mutex
	sent []notify.Push
	fail map[string]error // token → error to return instead of sending
}

func (f *fakePush) Send(_ context.Context, p notify.Push) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[p.Token]; err != nil {
		return err
	}
	f.sent = append(f.sent, p)
	return nil
}

// flushPushes runs the push worker until the queue is empty and returns what was sent.
func (e *env) flushPushes() []notify.Push {
	e.t.Helper()
	for {
		n, err := e.worker.SendDue(context.Background())
		if err != nil {
			e.t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	e.pushes.mu.Lock()
	defer e.pushes.mu.Unlock()
	out := e.pushes.sent
	e.pushes.sent = nil
	return out
}

// pushesTo returns "type:title|body" for each push to a device token.
func pushesTo(ps []notify.Push, token string) []string {
	var out []string
	for _, p := range ps {
		if p.Token == token {
			out = append(out, p.Data["type"]+":"+p.Title+"|"+p.Body)
		}
	}
	return out
}

func (e *env) registerDevice(tok, device string) {
	e.t.Helper()
	e.expect(e.do("POST", "/api/v1/devices", tok, map[string]string{"token": device, "platform": "android"}), 204, "")
}

type notifyFixture struct {
	*tripFixture
	route            string
	stops            []string
	parentA, parentB string // tokens: A's child uses Bravo, B's child uses Charlie
	deviceA, deviceB string
	childA, childB   string
	tripID           string
}

func newNotifyFixture(e *env) *notifyFixture {
	f := &notifyFixture{tripFixture: newTripFixture(e)}
	f.route, f.stops = e.compactRoute(f.tripFixture)
	f.childA = f.student
	e.do("PUT", f.base+"/students/"+f.childA+"/assignment", f.admin, map[string]any{"route_id": f.route, "pickup_stop_id": f.stops[1], "drop_stop_id": f.stops[1]})
	f.childB = e.do("POST", f.base+"/students", f.admin, map[string]any{"admission_no": "B-1", "name": "Bala"}).data()["id"].(string)
	e.do("PUT", f.base+"/students/"+f.childB+"/assignment", f.admin, map[string]any{"route_id": f.route, "pickup_stop_id": f.stops[2], "drop_stop_id": f.stops[2]})
	e.expect(e.do("POST", f.base+"/parents", f.admin, map[string]any{"name": "Meena", "mobile": "9000000002",
		"children": []map[string]string{{"student_id": f.childA}}}), 201, "")
	e.expect(e.do("POST", f.base+"/parents", f.admin, map[string]any{"name": "Raj", "mobile": "9000000003",
		"children": []map[string]string{{"student_id": f.childB}}}), 201, "")
	f.parentA, f.parentB = e.otpLogin("9000000002", "parent"), e.otpLogin("9000000003", "parent")
	f.deviceA, f.deviceB = "fcm-token-parent-A-0000000001", "fcm-token-parent-B-0000000002"
	e.registerDevice(f.parentA, f.deviceA)
	e.registerDevice(f.parentB, f.deviceB)
	f.tripID = e.do("POST", f.base+"/trips", f.tm, f.trip(f.today, "morning_pickup", f.route, f.bus1, f.driver1)).data()["id"].(string)
	return f
}

func TestAutomaticNotifications(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)

	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/start", f.driver1Tok, nil)
	sent := e.flushPushes()
	if a := pushesTo(sent, f.deviceA); len(a) != 1 || !strings.HasPrefix(a[0], "bus_started:Asha · Bus started|") {
		t.Fatalf("bus started to A: %v", a)
	}

	at := e.drive(f.tripFixture, f.tripID, 11.000, 11.0045, time.Now()) // through Alpha, into Bravo
	sent = e.flushPushes()
	a := pushesTo(sent, f.deviceA)
	if len(a) != 2 || !strings.Contains(a[0], "Bus is approaching your stop. Estimated arrival:") || a[1] != "bus_reached:Asha · Bus at your stop|Bus reached your stop." {
		t.Fatalf("A at Bravo: %v", a)
	}
	// Parent B's child uses Charlie: nothing about Bravo (or Alpha, which nobody uses).
	if b := pushesTo(sent, f.deviceB); len(b) != 0 {
		t.Errorf("B got Bravo pushes: %v", b)
	}

	// B logs out: their phone must not receive anything after that.
	e.expect(e.do("POST", "/api/v1/auth/logout", f.parentB, nil), 204, "")
	e.drive(f.tripFixture, f.tripID, 11.00475, 11.0085, at) // past Bravo and Charlie, into school
	sent = e.flushPushes()
	if a := pushesTo(sent, f.deviceA); len(a) != 2 || !strings.HasPrefix(a[0], "bus_crossed:") || !strings.HasPrefix(a[1], "school_reached:") {
		t.Errorf("A after Bravo: %v", a)
	}
	if b := pushesTo(sent, f.deviceB); len(b) != 0 {
		t.Errorf("logged-out device got pushes: %v", b)
	}
	// ...but B's inbox still has the Charlie notifications.
	var bInbox int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM notification_recipients r JOIN notifications n ON n.id = r.notification_id
		WHERE n.type IN ('bus_approaching','bus_reached','bus_crossed') AND r.student_id = $1`, f.childB).Scan(&bInbox)
	if bInbox != 3 {
		t.Errorf("B's inbox stop notifications = %d, want 3", bInbox)
	}

	e.expect(e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/end", f.driver1Tok, nil), 200, "")
	if a := pushesTo(e.flushPushes(), f.deviceA); len(a) != 1 || !strings.HasPrefix(a[0], "trip_completed:") {
		t.Errorf("trip completed: %v", a)
	}

	// Each event was notified exactly once.
	var dupes int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM (SELECT dedupe_key FROM notifications
		WHERE trip_id = $1 GROUP BY dedupe_key HAVING count(*) > 1) d`, f.tripID).Scan(&dupes)
	if dupes != 0 {
		t.Error("duplicate notifications")
	}
}

func TestParentInbox(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	// A school-wide announcement (about no child) and a trip start (about the child).
	e.expect(e.do("POST", f.base+"/announcements", f.admin, map[string]any{"category": "holiday", "target": "school",
		"title": "Holiday", "message": "School is closed on Friday."}), 201, "")
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/start", f.driver1Tok, nil)

	inbox := e.do("GET", "/api/v1/parent/notifications", f.parentA, nil)
	e.expect(inbox, 200, "")
	items := inbox.list()
	if len(items) != 2 || inbox.Body["meta"].(map[string]any)["unread"] != float64(2) {
		t.Fatalf("inbox = %v meta=%v", items, inbox.Body["meta"])
	}
	first := items[0].(map[string]any)
	if first["type"] != "bus_started" || first["student_name"] != "Asha" {
		t.Errorf("newest = %v", first)
	}
	// Filter by child: the child's own plus school-wide ones.
	if n := len(e.do("GET", "/api/v1/parent/notifications?student_id="+f.childA, f.parentA, nil).list()); n != 2 {
		t.Errorf("filtered = %d", n)
	}
	// Another parent's child is not a valid filter.
	e.expect(e.do("GET", "/api/v1/parent/notifications?student_id="+f.childB, f.parentA, nil), 404, "not_found")

	// Marking read only affects the caller's own items.
	id := first["id"].(string)
	e.expect(e.do("POST", "/api/v1/parent/notifications/read", f.parentB, map[string]any{"ids": []string{id}}), 204, "")
	if u := e.do("GET", "/api/v1/parent/notifications", f.parentA, nil).Body["meta"].(map[string]any)["unread"]; u != float64(2) {
		t.Errorf("another parent marked A's item read: unread=%v", u)
	}
	e.expect(e.do("POST", "/api/v1/parent/notifications/read", f.parentA, map[string]any{"ids": []string{id}}), 204, "")
	if u := e.do("GET", "/api/v1/parent/notifications", f.parentA, nil).Body["meta"].(map[string]any)["unread"]; u != float64(1) {
		t.Errorf("unread after mark = %v", u)
	}
	e.expect(e.do("GET", "/api/v1/parent/notifications", f.tm, nil), 403, "forbidden")
}

func TestDelaysAndEmergencies(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	srv := httptest.NewServer(e.srv)
	defer srv.Close()
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/start", f.driver1Tok, nil)
	e.flushPushes()

	staff, _, _ := e.dialWS(srv, f.tm, "")
	staff.send(map[string]any{"type": "subscribe", "channel": "school"})
	staff.next("subscribed")
	parentWS, _, _ := e.dialWS(srv, f.parentA, "")
	parentWS.send(map[string]any{"type": "subscribe", "channel": "trip", "trip_id": f.tripID})
	parentWS.next("subscribed")

	delays := "/api/v1/driver/trips/" + f.tripID + "/delays"
	e.expect(e.do("POST", delays, f.driver1Tok, map[string]any{"minutes": 0, "reason": "traffic"}), 400, "validation_failed")
	d := e.do("POST", delays, f.driver1Tok, map[string]any{"minutes": 15, "reason": "traffic", "note": "Signal not working"})
	e.expect(d, 201, "")
	sent := e.flushPushes()
	want := "bus_delayed:Asha · Bus delayed|Bus Delayed by 15 Minutes. Please expect a delay in reaching your pickup location."
	if a := pushesTo(sent, f.deviceA); len(a) != 1 || a[0] != want {
		t.Errorf("delay push to A: %v", a)
	}
	if len(pushesTo(sent, f.deviceB)) != 1 {
		t.Error("every parent on the trip should hear about a delay")
	}
	if m := staff.next("alert"); m["alert"].(map[string]any)["kind"] != "delay" {
		t.Errorf("staff alert: %v", m)
	}

	// Breakdown, reported by the Transport Manager.
	e.expect(e.do("POST", f.base+"/trips/"+f.tripID+"/delays", f.tm, map[string]any{"minutes": 30, "reason": "bus_breakdown"}), 201, "")
	if a := pushesTo(e.flushPushes(), f.deviceA); len(a) != 1 || !strings.Contains(a[0], "Your school bus has experienced a breakdown") {
		t.Errorf("breakdown push: %v", a)
	}
	staff.next("alert")

	// Emergency: staff are alerted live; parents are not.
	em := e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/emergency", f.driver1Tok, map[string]any{"message": "Child unwell", "latitude": 11.001, "longitude": 77.0})
	e.expect(em, 201, "")
	if m := staff.next("alert"); m["alert"].(map[string]any)["kind"] != "emergency" {
		t.Errorf("emergency alert: %v", m)
	}
	if len(pushesTo(e.flushPushes(), f.deviceA)) != 0 {
		t.Error("emergencies must not be pushed to parents automatically")
	}
	// The parent watching the trip got the delay pushes but must never get staff alerts.
	parentWS.send(map[string]any{"type": "ping"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, data, err := parentWS.c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"type":"alert"`) {
			t.Fatal("a parent received a staff alert")
		}
		if strings.Contains(string(data), `"pong"`) {
			break
		}
	}

	alerts := e.do("GET", f.base+"/alerts", f.tm, nil)
	if n := len(alerts.list()); n != 3 {
		t.Errorf("alerts = %d, want 3", n)
	}
	emID := em.data()["id"].(string)
	e.expect(e.do("POST", f.base+"/emergencies/"+emID+"/acknowledge", f.tm, nil), 200, "")
	e.expect(e.do("POST", f.base+"/emergencies/"+emID+"/acknowledge", f.tm, nil), 409, "invalid_transition")
	resolved := e.do("POST", f.base+"/emergencies/"+emID+"/resolve", f.tm, nil)
	if resolved.data()["status"] != "resolved" {
		t.Errorf("resolve: %v", resolved.data())
	}
	// Another driver cannot report on this trip.
	e.expect(e.do("POST", delays, f.driver2Tok, map[string]any{"minutes": 5, "reason": "traffic"}), 404, "not_found")
}

func TestAnnouncements(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	other, _ := e.routeWithStops(f.admin, f.school, "RS-77", false, "Elsewhere")
	ann := f.base + "/announcements"

	// Whole school: admins only.
	e.expect(e.do("POST", ann, f.tm, map[string]any{"category": "holiday", "target": "school", "title": "Holiday", "message": "Closed"}), 403, "forbidden")
	sent := e.do("POST", ann, f.admin, map[string]any{"category": "holiday", "target": "school", "title": "Holiday",
		"message": "Parents are requested to pick up their children from school.", "attachment_url": "https://school.example/circular.pdf"})
	e.expect(sent, 201, "")
	if sent.data()["status"] != "sent" || sent.data()["stats"].(map[string]any)["recipients"] != float64(2) {
		t.Fatalf("school announcement: %v", sent.data())
	}
	ps := e.flushPushes()
	if len(pushesTo(ps, f.deviceA)) != 1 || len(pushesTo(ps, f.deviceB)) != 1 {
		t.Errorf("school-wide pushes: %v", ps)
	}

	// Route: Transport Managers may; only that route's parents get it.
	e.expect(e.do("POST", ann, f.tm, map[string]any{"category": "route_announcement", "target": "route", "route_id": other,
		"title": "Route change", "message": "New stop"}), 201, "")
	if ps := e.flushPushes(); len(ps) != 0 {
		t.Errorf("nobody rides RS-77, yet pushes went out: %v", ps)
	}
	e.expect(e.do("POST", ann, f.tm, map[string]any{"category": "traffic_delay", "target": "route", "route_id": f.route,
		"title": "Traffic", "message": "Expect delays"}), 201, "")
	if ps := e.flushPushes(); len(ps) != 2 {
		t.Errorf("route announcement pushes = %d, want 2", len(ps))
	}

	// Scheduled: not sent until due; can be cancelled before.
	later := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s1 := e.do("POST", ann, f.admin, map[string]any{"category": "other", "target": "school", "title": "Later", "message": "x", "scheduled_at": later})
	s2 := e.do("POST", ann, f.admin, map[string]any{"category": "other", "target": "school", "title": "Later 2", "message": "y", "scheduled_at": later})
	if s1.data()["status"] != "scheduled" || len(e.flushPushes()) != 0 {
		t.Fatalf("scheduled: %v", s1.data())
	}
	e.expect(e.do("POST", ann+"/"+s1.data()["id"].(string)+"/cancel", f.admin, nil), 200, "")
	e.expect(e.do("POST", ann+"/"+s1.data()["id"].(string)+"/cancel", f.admin, nil), 409, "invalid_transition")
	// Make s2 due, then run the scheduler.
	_, _ = e.pool.Exec(context.Background(), `UPDATE announcements SET scheduled_at = now() - interval '1 second' WHERE id = $1`, s2.data()["id"])
	if err := e.api.SendDueAnnouncements(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(e.flushPushes()) != 2 {
		t.Error("due scheduled announcement was not sent")
	}
	list := e.do("GET", ann, f.tm, nil)
	if n := len(list.list()); n != 5 {
		t.Errorf("history = %d, want 5", n)
	}
	e.expect(e.do("POST", ann, f.admin, map[string]any{"category": "other", "target": "route", "title": "x", "message": "y"}), 400, "validation_failed")
}

func TestPushRetryAndInvalidToken(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	e.pushes.fail = map[string]error{f.deviceA: errors.New("fcm: 503 unavailable"), f.deviceB: notify.ErrInvalidToken}
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/start", f.driver1Tok, nil)
	e.flushPushes()

	status := func(token string) (st string, attempts int) {
		_ = e.pool.QueryRow(context.Background(), `SELECT status, attempts FROM push_deliveries WHERE token = $1`, token).Scan(&st, &attempts)
		return
	}
	// A temporary failure is retried later; an invalid token is dropped for good.
	if st, n := status(f.deviceA); st != "pending" || n != 1 {
		t.Errorf("A: %s after %d attempts, want pending retry", st, n)
	}
	if st, _ := status(f.deviceB); st != "invalid" {
		t.Errorf("B: %s, want invalid", st)
	}
	var devices int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM device_tokens WHERE token = $1`, f.deviceB).Scan(&devices)
	if devices != 0 {
		t.Error("an invalid token must be deleted")
	}

	// When the retry is due and FCM works again, it is delivered.
	e.pushes.fail = nil
	_, _ = e.pool.Exec(context.Background(), `UPDATE push_deliveries SET next_attempt_at = now() WHERE token = $1`, f.deviceA)
	if len(pushesTo(e.flushPushes(), f.deviceA)) != 1 {
		t.Error("retry was not sent")
	}
	if st, n := status(f.deviceA); st != "sent" || n != 2 {
		t.Errorf("A: %s after %d attempts", st, n)
	}
}
