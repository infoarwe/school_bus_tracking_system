package notify

import (
	"context"
	"errors"
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

// SendDue sends one batch of due pushes and returns how many were handled.
func (w *Worker) SendDue(ctx context.Context) (int, error) {
	batch, err := store.ClaimDeliveries(ctx, w.Store.Pool, 100)
	if err != nil {
		return 0, err
	}
	for _, d := range batch {
		sender, err := w.Senders.For(ctx, d.SchoolID)
		if err == nil {
			err = sender.Send(ctx, Push{Token: d.Token, Title: d.Title, Body: d.Body, Data: d.Data})
		}
		switch {
		case err == nil:
			err = store.FinishDelivery(ctx, w.Store.Pool, d.ID, "sent", "", nil)
		case errors.Is(err, ErrInvalidToken):
			_ = store.DeleteDeviceToken(ctx, w.Store.Pool, d.Token)
			err = store.FinishDelivery(ctx, w.Store.Pool, d.ID, "invalid", err.Error(), nil)
		case d.Attempts < len(retryDelays):
			retry := time.Now().Add(retryDelays[d.Attempts])
			err = store.FinishDelivery(ctx, w.Store.Pool, d.ID, "pending", err.Error(), &retry)
		default:
			err = store.FinishDelivery(ctx, w.Store.Pool, d.ID, "failed", err.Error(), nil)
		}
		if err != nil {
			return 0, err
		}
	}
	return len(batch), nil
}
