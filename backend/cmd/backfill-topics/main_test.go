package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

func tok(userID, token string) store.ParentDeviceToken {
	return store.ParentDeviceToken{UserID: userID, Token: token}
}

// TestRunBackfillCountsAndContinuesOnError: one token's failure must not stop
// the rest, and the three outcomes (reconciled / skipped / failed) must be
// counted accurately — this is the dry-run-free decision logic the real
// command's -yes path executes, exercised here with fakes only (no DB, no
// Firebase, no real backfill run).
func TestRunBackfillCountsAndContinuesOnError(t *testing.T) {
	tokens := []store.ParentDeviceToken{tok("u1", "t1"), tok("u2", "t2"), tok("u3", "t3"), tok("u4", "t4")}
	lookup := func(_ context.Context, userID string) (store.ParentTopicContext, bool, error) {
		switch userID {
		case "u1":
			return store.ParentTopicContext{SchoolID: schoolA}, true, nil
		case "u2":
			return store.ParentTopicContext{}, false, nil // no longer an active parent
		case "u3":
			return store.ParentTopicContext{}, false, errors.New("db unavailable")
		default:
			return store.ParentTopicContext{SchoolID: schoolA}, true, nil
		}
	}
	reconcile := func(_ context.Context, schoolID, token string, desired map[string]bool) error {
		if token == "t4" {
			return errors.New("firebase rejected this token")
		}
		return nil
	}
	var log bytes.Buffer
	r := runBackfill(context.Background(), tokens, lookup, reconcile, &log)

	if r.reconciled != 1 {
		t.Errorf("reconciled = %d, want 1 (only u1)", r.reconciled)
	}
	if r.skipped != 1 {
		t.Errorf("skipped = %d, want 1 (u2, not an active parent)", r.skipped)
	}
	if r.failed != 2 {
		t.Errorf("failed = %d, want 2 (u3 lookup error, u4 reconcile error)", r.failed)
	}
	if log.Len() == 0 {
		t.Error("failures should be logged so an operator can see what needs a re-run")
	}
}

// TestRunBackfillIsIdempotent: re-running with the same inputs after a
// transient failure clears up must successfully pick up exactly what failed
// before, without needing any separate progress-tracking state (runBackfill
// itself holds none between calls — ReconcileToken's own ledger is what makes
// re-running safe in the real command).
func TestRunBackfillIsIdempotent(t *testing.T) {
	tokens := []store.ParentDeviceToken{tok("u1", "t1")}
	lookup := func(_ context.Context, userID string) (store.ParentTopicContext, bool, error) {
		return store.ParentTopicContext{SchoolID: schoolA}, true, nil
	}

	failing := true
	var attempts int
	reconcile := func(_ context.Context, schoolID, token string, desired map[string]bool) error {
		attempts++
		if failing {
			return errors.New("transient firebase error")
		}
		return nil
	}

	var log bytes.Buffer
	first := runBackfill(context.Background(), tokens, lookup, reconcile, &log)
	if first.failed != 1 || first.reconciled != 0 {
		t.Fatalf("first run: %+v, want 1 failed", first)
	}

	failing = false // the transient condition clears (e.g. Firebase recovers)
	second := runBackfill(context.Background(), tokens, lookup, reconcile, &log)
	if second.failed != 0 || second.reconciled != 1 {
		t.Fatalf("second run: %+v, want fully reconciled", second)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2 (one per run, no internal retry loop hiding state)", attempts)
	}

	// A third, fully-successful re-run (simulating running the command again
	// after it already succeeded) must not error or behave differently.
	third := runBackfill(context.Background(), tokens, lookup, reconcile, &log)
	if third.failed != 0 || third.reconciled != 1 {
		t.Fatalf("third (repeat) run: %+v, want fully reconciled again", third)
	}
}

func TestRunBackfillEmptyTokenList(t *testing.T) {
	var log bytes.Buffer
	r := runBackfill(context.Background(), nil,
		func(context.Context, string) (store.ParentTopicContext, bool, error) {
			return store.ParentTopicContext{}, true, nil
		},
		func(context.Context, string, string, map[string]bool) error { return nil }, &log)
	if r.reconciled != 0 || r.skipped != 0 || r.failed != 0 {
		t.Errorf("got %+v, want all zero", r)
	}
}

const schoolA = "11111111-1111-1111-1111-111111111111"
