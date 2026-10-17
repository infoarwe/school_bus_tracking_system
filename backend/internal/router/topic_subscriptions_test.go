package router_test

// Additional topic-subscription coverage identified as gaps against the
// original 16-item test requirement list (see the Phase 2 implementation
// report). These tests use only the existing fakes/test-DB harness: no real
// Firebase, no migrations beyond what newEnv already applies idempotently,
// no backfill command, no deploy.

import (
	"context"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

func trackedTopics(t *testing.T, e *env, token string) map[string]bool {
	t.Helper()
	got, err := store.TrackedTopics(context.Background(), e.pool, token)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestTenantIsolationOfComputedTopics: two schools with structurally
// identical routes/stops (same names, same shape) must never produce
// overlapping topic sets or let one school's device end up tracking the
// other's topics — the UUIDs differ, so the strings can't collide, but this
// proves it end-to-end through the real registration/reconciliation path
// rather than just the topic-string-builder (already covered by
// internal/notify.TestTopicIsolation).
// isoSchool is a minimal, single-school setup (one route/stop, one student,
// one parent, one registered device) for cross-tenant comparisons — built by
// hand rather than via newNotifyFixture/newTripFixture, which hardcode a
// single school code and admin email and so cannot be instantiated twice in
// the same test.
type isoSchool struct {
	school, route, stop, device string
}

func newIsoSchool(e *env, code, adminEmail, parentMobile string) isoSchool {
	e.t.Helper()
	schoolID := e.school(code)
	e.user(&schoolID, models.RoleSchoolAdmin, adminEmail, "")
	admin := e.login(adminEmail)
	route, stops := e.routeWithStops(admin, schoolID, code+"-R1", false, "Gandhipuram", "Peelamedu")
	base := "/api/v1/schools/" + schoolID
	student := e.do("POST", base+"/students", admin, map[string]any{"admission_no": code + "-S1", "name": "Student " + code}).data()["id"].(string)
	e.expect(e.do("PUT", base+"/students/"+student+"/assignment", admin,
		map[string]any{"route_id": route, "pickup_stop_id": stops[0], "drop_stop_id": stops[0]}), 200, "")
	e.expect(e.do("POST", base+"/parents", admin, map[string]any{"name": "Parent " + code, "mobile": parentMobile,
		"children": []map[string]string{{"student_id": student}}}), 201, "")
	parentTok := e.otpLogin(parentMobile, "parent")
	device := "fcm-token-iso-school-" + code + "-0000001"
	e.registerDevice(parentTok, device)
	return isoSchool{school: schoolID, route: route, stop: stops[0], device: device}
}

func TestTenantIsolationOfComputedTopics(t *testing.T) {
	e := newEnv(t)
	a := newIsoSchool(e, "ISOA", "admin-isoa@test.example", "9100000001")
	b := newIsoSchool(e, "ISOB", "admin-isob@test.example", "9100000002")

	topicsA := trackedTopics(t, e, a.device)
	topicsB := trackedTopics(t, e, b.device)
	if len(topicsA) == 0 || len(topicsB) == 0 {
		t.Fatal("expected both devices to have tracked topics from registration")
	}
	for topic := range topicsA {
		if topicsB[topic] {
			t.Errorf("school A's device is tracking a topic school B's device also tracks: %q", topic)
		}
	}
	schoolTopicA, _ := notify.SchoolTopic(a.school)
	schoolTopicB, _ := notify.SchoolTopic(b.school)
	routeTopicA, _ := notify.RouteTopic(a.school, a.route)
	stopTopicA, _ := notify.StopTopic(a.school, a.route, a.stop)
	if !topicsA[schoolTopicA] || !topicsA[routeTopicA] || !topicsA[stopTopicA] {
		t.Errorf("A's device missing its own topics: %v", topicsA)
	}
	if topicsA[schoolTopicB] {
		t.Error("A's device must never track B's school topic")
	}
	if !topicsB[schoolTopicB] {
		t.Error("B's device should track B's school topic")
	}
}

// TestAssignmentChangeReconcilesTopics: moving a student to a different route
// must subscribe the new route/stop topics and unsubscribe the old ones —
// the parent's device should never keep hearing about a route their child no
// longer rides.
func TestAssignmentChangeReconcilesTopics(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)

	oldRoute, _ := notify.RouteTopic(f.school, f.route)
	oldStop, _ := notify.StopTopic(f.school, f.route, f.stops[1])
	before := trackedTopics(t, e, f.deviceA)
	if !before[oldRoute] || !before[oldStop] {
		t.Fatalf("expected A to start on the original route/stop: %v", before)
	}

	newRoute, newStops := e.routeWithStops(f.admin, f.school, "RS-99", false, "North", "South")
	e.expect(e.do("PUT", f.base+"/students/"+f.childA+"/assignment", f.admin,
		map[string]any{"route_id": newRoute, "pickup_stop_id": newStops[0], "drop_stop_id": newStops[0]}), 200, "")

	after := trackedTopics(t, e, f.deviceA)
	newRouteTopic, _ := notify.RouteTopic(f.school, newRoute)
	newStopTopic, _ := notify.StopTopic(f.school, newRoute, newStops[0])
	if !after[newRouteTopic] || !after[newStopTopic] {
		t.Errorf("A should now be subscribed to the new route/stop: %v", after)
	}
	if after[oldRoute] {
		t.Error("A should no longer be subscribed to the old route topic")
	}
	if after[oldStop] {
		t.Error("A should no longer be subscribed to the old stop topic")
	}
}

// TestUnlinkOneOfTwoSharedChildren: two children of the same parent on the
// same route/stop; unlinking one must not drop the topic the other still
// needs.
func TestUnlinkOneOfTwoSharedChildren(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	route, stops := e.compactRoute(f)
	childA := f.student
	e.do("PUT", f.base+"/students/"+childA+"/assignment", f.admin, map[string]any{"route_id": route, "pickup_stop_id": stops[1], "drop_stop_id": stops[1]})
	childB := e.do("POST", f.base+"/students", f.admin, map[string]any{"admission_no": "SH-1", "name": "Shared"}).data()["id"].(string)
	e.do("PUT", f.base+"/students/"+childB+"/assignment", f.admin, map[string]any{"route_id": route, "pickup_stop_id": stops[1], "drop_stop_id": stops[1]})

	p := e.do("POST", f.base+"/parents", f.admin, map[string]any{"name": "Both", "mobile": "9000000009",
		"children": []map[string]string{{"student_id": childA}, {"student_id": childB}}})
	e.expect(p, 201, "")
	parentID := p.data()["id"].(string)
	parentTok := e.otpLogin("9000000009", "parent")
	device := "fcm-token-shared-parent-0000000009"
	e.registerDevice(parentTok, device)

	routeTopic, _ := notify.RouteTopic(f.school, route)
	stopTopic, _ := notify.StopTopic(f.school, route, stops[1])
	if got := trackedTopics(t, e, device); !got[routeTopic] || !got[stopTopic] {
		t.Fatalf("expected both route and stop topics before unlink: %v", got)
	}

	// Unlink child A only (keep child B, who is on the same route/stop).
	e.expect(e.do("PUT", f.base+"/parents/"+parentID, f.admin, map[string]any{"name": "Both", "mobile": "9000000009",
		"children": []map[string]string{{"student_id": childB}}}), 200, "")

	after := trackedTopics(t, e, device)
	if !after[routeTopic] {
		t.Error("route topic must stay subscribed: child B still rides it")
	}
	if !after[stopTopic] {
		t.Error("stop topic must stay subscribed: child B still uses it")
	}

	// Now unlink the remaining child too: everything but the school topic goes.
	e.expect(e.do("PUT", f.base+"/parents/"+parentID, f.admin, map[string]any{"name": "Both", "mobile": "9000000009",
		"children": []map[string]string{}}), 200, "")
	final := trackedTopics(t, e, device)
	if final[routeTopic] || final[stopTopic] {
		t.Errorf("no linked child left: route/stop topics should be gone: %v", final)
	}
}

// TestTokenRotationReconciles: a new token for the same parent (simulating
// FCM rotating it, e.g. after a reinstall) must end up with the same, full,
// correctly-derived topic set as the original — independent of whatever the
// old token is still tracking.
func TestTokenRotationReconciles(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	rotated := "fcm-token-parent-A-rotated-0000000099"
	e.registerDevice(f.parentA, rotated)

	want := trackedTopics(t, e, f.deviceA)
	got := trackedTopics(t, e, rotated)
	if len(want) == 0 {
		t.Fatal("original device has no tracked topics to compare against")
	}
	for topic := range want {
		if !got[topic] {
			t.Errorf("rotated token missing topic the original has: %q", topic)
		}
	}
	for topic := range got {
		if !want[topic] {
			t.Errorf("rotated token has an unexpected extra topic: %q", topic)
		}
	}
}

// TestUnsubscribeFailureNotRecorded: if Firebase reports failure when
// unsubscribing (e.g. during logout), the ledger must still show the topic as
// tracked — the opposite of TestSubscribeFailureNotRecorded, same principle:
// never represent a failed Firebase call as having succeeded.
func TestUnsubscribeFailureNotRecorded(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	schoolTopic, _ := notify.SchoolTopic(f.school)
	if got := trackedTopics(t, e, f.deviceB); !got[schoolTopic] {
		t.Fatal("expected B to already be subscribed to the school topic")
	}

	e.pushes.failUnsubscribe = map[string]bool{f.deviceB: true}
	e.expect(e.do("POST", "/api/v1/auth/logout", f.parentB, nil), 204, "")

	// Logout must still succeed from the caller's point of view (best-effort,
	// never blocks logout), but the ledger must not have been cleared for a
	// call Firebase actually rejected.
	after := trackedTopics(t, e, f.deviceB)
	if !after[schoolTopic] {
		t.Error("a rejected unsubscribe must leave the ledger tracking the topic, so a future reconciliation retries it")
	}
}

// TestReconciliationIsIdempotent: reconciling twice in a row with nothing
// changed must not attempt to re-subscribe topics already tracked (no
// redundant Firebase calls, and no errors from doing it again).
func TestReconciliationIsIdempotent(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	before := e.pushes.subscribeCalls
	// Re-registering the same token with nothing about the parent's
	// assignments changed should find everything already tracked.
	e.registerDevice(f.parentA, f.deviceA)
	if e.pushes.subscribeCalls != before {
		t.Errorf("subscribe calls grew from %d to %d on a no-op reconciliation", before, e.pushes.subscribeCalls)
	}
}

// TestConfigMissingNotRecordedAsSent exercises the real notify.SchoolSenders
// resolution path (not the test harness's notify.Static fake) for a school
// with no Firebase key uploaded and no server-wide fallback configured: the
// worker must record 'config_missing', never 'sent'. No Firebase or network
// call happens here — LogSender is a pure no-op.
func TestConfigMissingNotRecordedAsSent(t *testing.T) {
	e := newEnv(t)
	f := newNotifyFixture(e)
	realSenders := &notify.SchoolSenders{
		Store: store.New(e.pool), Secrets: testSecrets(t), Fallback: notify.LogSender{},
		NewSender: func(ctx context.Context, raw []byte) (notify.Sender, error) { return notify.NewFCMSenderJSON(ctx, raw) },
	}
	worker := &notify.Worker{Store: store.New(e.pool), Senders: realSenders}

	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/confirm", f.driver1Tok, nil)
	e.do("POST", "/api/v1/driver/trips/"+f.tripID+"/start", f.driver1Tok, nil)
	// Drain with the REAL senders resolution (f.school has no push key configured).
	for {
		n, err := worker.SendDue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}

	var status string
	routeTopic, _ := notify.RouteTopic(f.school, f.route)
	if err := e.pool.QueryRow(context.Background(),
		`SELECT status FROM topic_deliveries WHERE topic = $1 ORDER BY created_at DESC LIMIT 1`, routeTopic).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "config_missing" {
		t.Errorf("status = %q, want config_missing (no Firebase key for this school)", status)
	}
}

// TestStaffOnlyNeverDispatchesATopic: a staff-only notification (route
// claim/release) must create inbox rows but never a topic_deliveries row —
// it has no parent-facing topic, and must never fall through to the route
// topic (which would leak it to parents).
func TestStaffOnlyNeverDispatchesATopic(t *testing.T) {
	e := newEnv(t)
	f := newTripFixture(e)
	routeID, _ := e.compactRoute(f)
	adminUserID := e.do("GET", "/api/v1/auth/me", f.admin, nil).data()["user"].(map[string]any)["id"].(string)
	admin := store.Rider{UserID: adminUserID}
	key := "test-staff-only-dedupe"
	id, created, err := notify.Notify(context.Background(), e.pool, store.NotificationInput{
		SchoolID: f.school, Type: "route_claimed", Title: "x", Body: "y",
		RouteID: &routeID, DedupeKey: &key, StaffOnly: true,
	}, []store.Rider{admin})
	if err != nil || !created {
		t.Fatalf("Notify: created=%v err=%v", created, err)
	}

	var recipients, deliveries int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM notification_recipients WHERE notification_id = $1`, id).Scan(&recipients)
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM topic_deliveries WHERE notification_id = $1`, id).Scan(&deliveries)
	if recipients == 0 {
		t.Error("staff-only notification should still create inbox recipient rows")
	}
	if deliveries != 0 {
		t.Errorf("staff-only notification must never create a topic_deliveries row, got %d", deliveries)
	}
}
