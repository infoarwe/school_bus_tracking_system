package notify

import (
	"context"
	"fmt"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// Notify stores a notification for the given recipients (the inbox — still
// per-user/per-child, unaffected by the topic migration: it's a pull model,
// not push) and queues exactly one topic-targeted push. With a DedupeKey it
// fires at most once: a repeat returns created=false.
//
// The topic is chosen from n.RouteID/n.StopID (set by the caller, which
// already knows the trip/route/stop context):
//   - n.StopID set: the stop topic (approaching/reached/crossed only)
//   - else n.RouteID set: the route topic (bus_started, trip_completed, delay,
//     breakdown, route announcements)
//   - else: the school topic (whole-school announcements)
//
// n.StaffOnly skips push entirely (inbox only) — staff have no supported
// topic subscription today (see internal/notify/subscriptions.go's doc on
// ParentTopicInfo: topics are computed from parent assignments only).
func Notify(ctx context.Context, q store.DBTX, n store.NotificationInput, riders []store.Rider) (id string, created bool, err error) {
	if len(riders) == 0 && !n.StaffOnly {
		return "", false, nil // nobody to tell; do not burn the dedupe key either
	}
	if n.Data == nil {
		n.Data = map[string]string{}
	}
	n.Data["type"] = n.Type
	if id, created, err = store.InsertNotification(ctx, q, n); err != nil || !created {
		return id, created, err
	}
	n.Data["notification_id"] = id
	if len(riders) > 0 {
		if err := store.InsertRecipients(ctx, q, id, riders); err != nil {
			return "", false, err
		}
	}
	if n.StaffOnly {
		return id, true, nil // no parent-facing topic exists for this notification type
	}

	topic, err := notificationTopic(n)
	if err != nil {
		return "", false, err
	}
	if err := store.InsertTopicDelivery(ctx, q, id, topic); err != nil {
		return "", false, err
	}
	return id, true, nil
}

func notificationTopic(n store.NotificationInput) (string, error) {
	if n.StopID != nil && *n.StopID != "" {
		if n.RouteID == nil || *n.RouteID == "" {
			return "", fmt.Errorf("notify: StopID set without RouteID")
		}
		return StopTopic(n.SchoolID, *n.RouteID, *n.StopID)
	}
	if n.RouteID != nil && *n.RouteID != "" {
		return RouteTopic(n.SchoolID, *n.RouteID)
	}
	return SchoolTopic(n.SchoolID)
}
