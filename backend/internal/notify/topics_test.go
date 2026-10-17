package notify

import "testing"

const (
	schoolA = "11111111-1111-1111-1111-111111111111"
	schoolB = "22222222-2222-2222-2222-222222222222"
	routeA  = "33333333-3333-3333-3333-333333333333"
	stopA   = "44444444-4444-4444-4444-444444444444"
)

func TestTopicConstruction(t *testing.T) {
	school, err := SchoolTopic(schoolA)
	if err != nil || school != "school_"+schoolA {
		t.Fatalf("SchoolTopic = %q, %v", school, err)
	}
	route, err := RouteTopic(schoolA, routeA)
	if err != nil || route != "school_"+schoolA+"_route_"+routeA {
		t.Fatalf("RouteTopic = %q, %v", route, err)
	}
	stop, err := StopTopic(schoolA, routeA, stopA)
	if err != nil || stop != "school_"+schoolA+"_route_"+routeA+"_stop_"+stopA {
		t.Fatalf("StopTopic = %q, %v", stop, err)
	}
	// Each tier nests the one below it as a strict prefix, but they are three
	// distinct topic strings — a stop event must never collapse onto its route
	// or school topic.
	if school == route || route == stop || school == stop {
		t.Fatalf("tiers must be distinct topics: %q %q %q", school, route, stop)
	}
}

func TestTopicConstructionRejectsNonUUIDs(t *testing.T) {
	cases := []string{"", "not-a-uuid", "11111111-1111-1111-1111-11111111111", "'; DROP TABLE schools;--"}
	for _, bad := range cases {
		if _, err := SchoolTopic(bad); err == nil {
			t.Errorf("SchoolTopic(%q) should have been rejected", bad)
		}
		if _, err := RouteTopic(schoolA, bad); err == nil {
			t.Errorf("RouteTopic(valid, %q) should have been rejected", bad)
		}
		if _, err := StopTopic(schoolA, routeA, bad); err == nil {
			t.Errorf("StopTopic(valid, valid, %q) should have been rejected", bad)
		}
	}
}

// TestTopicIsolation is the collision/tenant-isolation guarantee the Phase 1
// audit flagged as the one that matters most: two schools' topics (and two
// routes', two stops') must never collide, so subscribing to one can never
// reach the other.
func TestTopicIsolation(t *testing.T) {
	aSchool, _ := SchoolTopic(schoolA)
	bSchool, _ := SchoolTopic(schoolB)
	aRoute, _ := RouteTopic(schoolA, routeA)
	bRoute, _ := RouteTopic(schoolB, routeA) // same route UUID, different school: must still differ
	aStop, _ := StopTopic(schoolA, routeA, stopA)
	bStop, _ := StopTopic(schoolB, routeA, stopA)

	topics := map[string]string{"aSchool": aSchool, "bSchool": bSchool, "aRoute": aRoute, "bRoute": bRoute, "aStop": aStop, "bStop": bStop}
	seen := map[string]string{}
	for name, topic := range topics {
		if other, dup := seen[topic]; dup {
			t.Fatalf("%s and %s collide on topic %q", name, other, topic)
		}
		seen[topic] = name
	}
}
