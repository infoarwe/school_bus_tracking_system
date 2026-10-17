package notify

import (
	"context"
	"errors"
	"fmt"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// ComputeDesiredTopics turns a parent's school + their linked active
// children's current route/stop assignments into the full set of topics a
// device of theirs should be subscribed to. Derived entirely from
// store.ParentTopicInfo (i.e. from student_assignments), never from anything
// a client reports — the one authorization rule that matters here (rule 1/2):
// a parent's topic set can only ever be what their own, already-validated
// assignment data says it is.
func ComputeDesiredTopics(info store.ParentTopicContext) (map[string]bool, error) {
	desired := map[string]bool{}
	school, err := SchoolTopic(info.SchoolID)
	if err != nil {
		return nil, fmt.Errorf("school topic: %w", err)
	}
	desired[school] = true
	for _, a := range info.Assignments {
		route, err := RouteTopic(info.SchoolID, a.RouteID)
		if err != nil {
			return nil, fmt.Errorf("route topic: %w", err)
		}
		desired[route] = true
		for _, stopID := range []*string{a.PickupStopID, a.DropStopID} {
			if stopID == nil || *stopID == "" {
				continue
			}
			stop, err := StopTopic(info.SchoolID, a.RouteID, *stopID)
			if err != nil {
				return nil, fmt.Errorf("stop topic: %w", err)
			}
			desired[stop] = true
		}
	}
	return desired, nil
}

// ErrNoTopicSupport: the resolved sender cannot manage subscriptions (neither
// a real FCM project nor something that implements TopicSubscriber).
var ErrNoTopicSupport = errors.New("sender does not support topic subscription")

// ReconcileToken diffs token's tracked topics (our ledger) against desired and
// calls Firebase to subscribe/unsubscribe exactly the difference, updating the
// ledger only for calls Firebase actually confirmed. A dev-only sender (no
// Firebase project configured for this school) is a silent no-op: there is
// nothing to subscribe to, and that is reported truthfully at send time
// (status 'config_missing'), not here. Returns the first error encountered,
// after still attempting every other topic in the diff — a partial failure
// leaves the ledger accurately reflecting whatever did succeed, so the next
// reconciliation (next login, next assignment change) retries only the rest.
func ReconcileToken(ctx context.Context, q store.DBTX, senders Senders, schoolID, token string, desired map[string]bool) error {
	sender, err := senders.For(ctx, schoolID)
	if err != nil {
		return fmt.Errorf("resolve sender: %w", err)
	}
	if IsDevOnly(sender) {
		return nil
	}
	ts, ok := sender.(TopicSubscriber)
	if !ok {
		return ErrNoTopicSupport
	}

	actual, err := store.TrackedTopics(ctx, q, token)
	if err != nil {
		return fmt.Errorf("load tracked topics: %w", err)
	}

	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	for topic := range desired {
		if actual[topic] {
			continue
		}
		failed, err := ts.SubscribeTokens(ctx, []string{token}, topic)
		if err != nil {
			note(fmt.Errorf("subscribe %s: %w", topic, err))
			continue
		}
		if len(failed) > 0 {
			note(fmt.Errorf("subscribe %s: firebase reported failure", topic))
			continue
		}
		note(store.AddTopicSubscription(ctx, q, token, topic))
	}
	for topic := range actual {
		if desired[topic] {
			continue
		}
		failed, err := ts.UnsubscribeTokens(ctx, []string{token}, topic)
		if err != nil {
			note(fmt.Errorf("unsubscribe %s: %w", topic, err))
			continue
		}
		if len(failed) > 0 {
			note(fmt.Errorf("unsubscribe %s: firebase reported failure", topic))
			continue
		}
		note(store.RemoveTopicSubscription(ctx, q, token, topic))
	}
	return firstErr
}

// UnsubscribeAllForToken removes every topic token is tracked as subscribed
// to — called before deleting a device_tokens row (logout / unregister), so a
// removed device never keeps receiving pushes that no longer reach it. Like
// ReconcileToken, a partial failure leaves the ledger accurate for what did
// and did not succeed.
func UnsubscribeAllForToken(ctx context.Context, q store.DBTX, senders Senders, schoolID, token string) error {
	return ReconcileToken(ctx, q, senders, schoolID, token, map[string]bool{})
}
