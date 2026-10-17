package notify

import (
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

func ptr(s string) *string { return &s }

func TestComputeDesiredTopicsSingleChild(t *testing.T) {
	routeB := "55555555-5555-5555-5555-555555555555"
	stopB := "66666666-6666-6666-6666-666666666666"
	info := store.ParentTopicContext{
		SchoolID:    schoolA,
		Assignments: []store.ChildAssignment{{RouteID: routeB, PickupStopID: ptr(stopB), DropStopID: ptr(stopB)}},
	}
	got, err := ComputeDesiredTopics(info)
	if err != nil {
		t.Fatal(err)
	}
	school, _ := SchoolTopic(schoolA)
	route, _ := RouteTopic(schoolA, routeB)
	stop, _ := StopTopic(schoolA, routeB, stopB)
	want := map[string]bool{school: true, route: true, stop: true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for topic := range want {
		if !got[topic] {
			t.Errorf("missing topic %q", topic)
		}
	}
}

// TestComputeDesiredTopicsSharedRoute: two children of the same parent on the
// same route must not produce duplicate topics (a map is naturally dedup'd,
// but the route/stop topic count must still be exactly one each — this guards
// against ever switching the implementation to something that double-counts
// and then calls Subscribe twice, which is harmless but would be a smell).
func TestComputeDesiredTopicsSharedRouteAndStop(t *testing.T) {
	route := "77777777-7777-7777-7777-777777777777"
	stop := "88888888-8888-8888-8888-888888888888"
	info := store.ParentTopicContext{
		SchoolID: schoolA,
		Assignments: []store.ChildAssignment{
			{RouteID: route, PickupStopID: ptr(stop), DropStopID: ptr(stop)},
			{RouteID: route, PickupStopID: ptr(stop), DropStopID: ptr(stop)}, // second child, same route+stop
		},
	}
	got, err := ComputeDesiredTopics(info)
	if err != nil {
		t.Fatal(err)
	}
	// school + route + stop = exactly 3, however many children share them.
	if len(got) != 3 {
		t.Fatalf("got %d topics, want 3 (school+route+stop, deduped): %v", len(got), got)
	}
}

// TestComputeDesiredTopicsDistinctChildren: one child's route/stop must never
// disappear because another child on a different route was computed too, and
// a route that only supports one direction (pickup-only: no drop stop) must
// not produce a bogus empty-string stop topic.
func TestComputeDesiredTopicsDistinctChildren(t *testing.T) {
	routeX, stopX := "99999999-9999-9999-9999-999999999999", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	routeY, pickupY, dropY := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "cccccccc-cccc-cccc-cccc-cccccccccccc", "dddddddd-dddd-dddd-dddd-dddddddddddd"
	info := store.ParentTopicContext{
		SchoolID: schoolA,
		Assignments: []store.ChildAssignment{
			{RouteID: routeX, PickupStopID: ptr(stopX), DropStopID: nil}, // pickup-only route
			{RouteID: routeY, PickupStopID: ptr(pickupY), DropStopID: ptr(dropY)},
		},
	}
	got, err := ComputeDesiredTopics(info)
	if err != nil {
		t.Fatal(err)
	}
	school, _ := SchoolTopic(schoolA)
	rX, _ := RouteTopic(schoolA, routeX)
	sX, _ := StopTopic(schoolA, routeX, stopX)
	rY, _ := RouteTopic(schoolA, routeY)
	sPickupY, _ := StopTopic(schoolA, routeY, pickupY)
	sDropY, _ := StopTopic(schoolA, routeY, dropY)
	for _, topic := range []string{school, rX, sX, rY, sPickupY, sDropY} {
		if !got[topic] {
			t.Errorf("missing topic %q in %v", topic, got)
		}
	}
	if len(got) != 6 {
		t.Fatalf("got %d topics, want 6: %v", len(got), got)
	}
}

func TestComputeDesiredTopicsNoAssignment(t *testing.T) {
	// A linked child with no current route/stop assignment: only the school
	// topic (so a parent still gets whole-school announcements before their
	// child is assigned a route).
	info := store.ParentTopicContext{SchoolID: schoolA}
	got, err := ComputeDesiredTopics(info)
	if err != nil {
		t.Fatal(err)
	}
	school, _ := SchoolTopic(schoolA)
	if len(got) != 1 || !got[school] {
		t.Fatalf("got %v, want only %q", got, school)
	}
}
