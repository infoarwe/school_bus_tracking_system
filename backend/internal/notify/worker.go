package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// Retry back-off after a failed push; after the last one it is marked failed.
var retryDelays = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}

// Worker sends queued pushes. Safe to run on every API instance: rows are
// claimed with FOR UPDATE SKIP LOCKED.
type Worker struct {
	Store   *store.Store
	Senders Senders
}

// Run polls the queue until ctx ends.
func (w *Worker) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := w.SendDue(ctx); err != nil && ctx.Err() == nil {
				slog.Error("push worker", "err", err)
			}
		}
	}
}

// SendDue sends one batch of due topic pushes and returns how many were handled.
func (w *Worker) SendDue(ctx context.Context) (int, error) {
	batch, err := store.ClaimTopicDeliveries(ctx, w.Store.Pool, 100)
	if err != nil {
		return 0, err
	}
	for _, d := range batch {
		sender, resolveErr := w.Senders.For(ctx, d.SchoolID)
		var sendErr error
		if resolveErr == nil && !IsDevOnly(sender) {
			sendErr = sender.Send(ctx, Push{Topic: d.Topic, Title: d.Title, Body: d.Body, Data: d.Data})
		}
		outcome := classifyDelivery(resolveErr, sender, sendErr, d.Attempts)
		var retry *time.Time
		if outcome.status == "pending" {
			t := time.Now().Add(retryDelays[min(d.Attempts, len(retryDelays)-1)])
			retry = &t
		}
		if err := store.FinishTopicDelivery(ctx, w.Store.Pool, d.ID, outcome.status, outcome.lastError, retry); err != nil {
			return 0, err
		}
	}
	return len(batch), nil
}

type deliveryOutcome struct {
	status    string
	lastError string
}

// classifyDelivery decides a topic_deliveries row's next status. Pure and
// network-free so it's directly unit-tested (see worker_test.go) without a
// DB or a real Sender: resolveErr is whatever w.Senders.For returned, sender
// is its result (nil if resolveErr != nil), sendErr is whatever sender.Send
// returned (always nil if sender is dev-only or resolveErr != nil — SendDue
// never calls Send in that case).
func classifyDelivery(resolveErr error, sender Sender, sendErr error, attempts int) deliveryOutcome {
	switch {
	case resolveErr != nil:
		// Could not even resolve a sender (DB/decrypt error) — transient,
		// retry like any other delivery failure.
		if attempts >= len(retryDelays) {
			return deliveryOutcome{"failed", resolveErr.Error()}
		}
		return deliveryOutcome{"pending", resolveErr.Error()}
	case IsDevOnly(sender):
		// No real Firebase project configured for this school. This is a
		// configuration problem, not a delivery attempt — never recorded as
		// 'sent' merely because the dev-only sender's Send() returned nil
		// (SendDue does not even call it), and never retried on a timer:
		// nothing changes until the school uploads a key, and re-checking
		// that is a separate, future reconciliation path, not this worker.
		return deliveryOutcome{"config_missing", "no Firebase project configured for this school (Settings → Push notifications)"}
	case sendErr == nil:
		return deliveryOutcome{"sent", ""}
	case attempts < len(retryDelays):
		return deliveryOutcome{"pending", sendErr.Error()}
	default:
		return deliveryOutcome{"failed", sendErr.Error()}
	}
}
