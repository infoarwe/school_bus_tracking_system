// Package jobs runs the API's background work. Each job is safe to run on every
// API instance at once.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

// StaleSweep marks buses with no GPS for longer than their school's limit (S5-07).
func StaleSweep(ctx context.Context, st *store.Store, live *tracking.Live, every time.Duration) {
	tick(ctx, every, func() {
		limits, err := store.StaleLimits(ctx, st.Pool)
		if err != nil {
			slog.Error("stale sweep: load limits", "err", err)
			return
		}
		err = live.SweepStale(ctx, time.Now(), func(schoolID string) time.Duration {
			if d, ok := limits[schoolID]; ok {
				return d
			}
			return 2 * time.Minute
		})
		if err != nil {
			slog.Error("stale sweep", "err", err)
		}
	})
}

// LocationRetention deletes GPS history older than each school's retention (S5-04).
func LocationRetention(ctx context.Context, st *store.Store, every time.Duration) {
	tick(ctx, every, func() {
		n, err := store.DeleteExpiredLocations(ctx, st.Pool, 5000)
		if err != nil {
			slog.Error("location retention", "err", err)
			return
		}
		if n > 0 {
			slog.Info("location retention: deleted old GPS points", "rows", n)
		}
	})
}

// Announcements sends scheduled announcements when they are due.
func Announcements(ctx context.Context, every time.Duration, sendDue func(context.Context) error) {
	tick(ctx, every, func() {
		if err := sendDue(ctx); err != nil {
			slog.Error("announcements", "err", err)
		}
	})
}

func tick(ctx context.Context, every time.Duration, fn func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}
