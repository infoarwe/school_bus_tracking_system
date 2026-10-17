package notify

import (
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// TestNotificationTopicTierSelection is the direct, pure-function test of the
// stop-isolation guarantee: an event for stop X must resolve to stop X's own
// topic, never to the route-wide topic (which would broadcast it to every
// stop on the route) and never to a different stop's topic.
func TestNotificationTopicTierSelection(t *testing.T) {
	route2 := "33333333-3333-3333-3333-333333333333"
	stopX := "44444444-4444-4444-4444-444444444444"
	stopY := "55555555-5555-5555-5555-555555555555"

	wantStopX, _ := StopTopic(schoolA, route2, stopX)
	wantStopY, _ := StopTopic(schoolA, route2, stopY)
	wantRoute, _ := RouteTopic(schoolA, route2)
	wantSchool, _ := SchoolTopic(schoolA)

	t.Run("stop event targets its own stop topic", func(t *testing.T) {
		got, err := notificationTopic(store.NotificationInput{SchoolID: schoolA, RouteID: &route2, StopID: &stopX})
		if err != nil {
			t.Fatal(err)
		}
		if got != wantStopX {
			t.Errorf("got %q, want %q", got, wantStopX)
		}
		if got == wantStopY {
			t.Error("stop X resolved to stop Y's topic")
		}
		if got == wantRoute {
			t.Error("a stop event must never fall back to the route-wide topic")
		}
	})

	t.Run("route event (no stop) targets the route topic", func(t *testing.T) {
		got, err := notificationTopic(store.NotificationInput{SchoolID: schoolA, RouteID: &route2})
		if err != nil {
			t.Fatal(err)
		}
		if got != wantRoute {
			t.Errorf("got %q, want %q", got, wantRoute)
		}
	})

	t.Run("no route, no stop: school topic", func(t *testing.T) {
		got, err := notificationTopic(store.NotificationInput{SchoolID: schoolA})
		if err != nil {
			t.Fatal(err)
		}
		if got != wantSchool {
			t.Errorf("got %q, want %q", got, wantSchool)
		}
	})

	t.Run("stop set without route is a programming error, not a silent route-wide send", func(t *testing.T) {
		if _, err := notificationTopic(store.NotificationInput{SchoolID: schoolA, StopID: &stopX}); err == nil {
			t.Error("expected an error, not a fallback topic")
		}
	})
}
