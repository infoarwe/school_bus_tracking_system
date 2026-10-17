// Command backfill-topics subscribes every existing active parent device
// registration to its correct FCM topics, for schools migrating onto
// topic-based push delivery after upgrading past API 0.17.0.
//
// Safe to run more than once: ReconcileToken only ever applies the diff
// between what's already tracked (device_topic_subscriptions) and what a
// parent's current assignments say it should be, so a repeat run (or a retry
// after this process was killed partway through) picks up exactly where the
// last one left off — no separate progress file or flag needed.
//
//	go run ./cmd/backfill-topics -yes
//
// Requires the same .env / environment as the API (DATABASE_URL,
// DATA_ENCRYPTION_KEY, and either a per-school push key already uploaded or
// FCM_CREDENTIALS_FILE for the server-wide fallback). A school with neither
// is reported, not silently skipped as success — it will get real push once
// it uploads a key, with no further backfill needed (RegisterDevice already
// reconciles on every future login).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/config"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/database"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/secrets"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

func main() {
	yes := flag.Bool("yes", false, "actually run (otherwise just reports how many devices would be reconciled)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()
	db, err := database.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	st := store.New(db)

	box, err := secrets.New(cfg.DataEncryptionKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "DATA_ENCRYPTION_KEY:", err)
		os.Exit(1)
	}

	var fallback notify.Sender = notify.LogSender{}
	if cfg.FCMCredentialsFile != "" {
		fcm, err := notify.NewFCMSender(ctx, cfg.FCMCredentialsFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "server-wide Firebase key:", err)
			os.Exit(1)
		}
		fallback = fcm
	}
	senders := &notify.SchoolSenders{
		Store: st, Secrets: box, Fallback: fallback,
		NewSender: func(ctx context.Context, raw []byte) (notify.Sender, error) { return notify.NewFCMSenderJSON(ctx, raw) },
	}

	tokens, err := store.AllActiveParentDeviceTokens(ctx, db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("env=%s: %d active parent device registrations to reconcile\n", cfg.Env, len(tokens))
	if !*yes {
		fmt.Println("dry run (pass -yes to actually subscribe them)")
		return
	}

	lookup := func(ctx context.Context, userID string) (store.ParentTopicContext, bool, error) {
		return store.ParentTopicInfo(ctx, db, userID)
	}
	reconcile := func(ctx context.Context, schoolID, token string, desired map[string]bool) error {
		return notify.ReconcileToken(ctx, db, senders, schoolID, token, desired)
	}
	r := runBackfill(ctx, tokens, lookup, reconcile, os.Stdout)
	fmt.Printf("done: %d fully reconciled, %d skipped (not an active parent), %d incomplete (re-run to retry)\n",
		r.reconciled, r.skipped, r.failed)
	if r.failed > 0 {
		os.Exit(1) // do not claim success when some devices are still unreconciled
	}
}

// topicLookup and tokenReconciler are narrow seams around the DB/Firebase
// calls runBackfill makes, so its resume/retry/idempotency decision logic is
// unit-testable with fakes (see main_test.go) without a database or Firebase.
type topicLookup func(ctx context.Context, userID string) (store.ParentTopicContext, bool, error)
type tokenReconciler func(ctx context.Context, schoolID, token string, desired map[string]bool) error

type backfillResult struct{ reconciled, skipped, failed int }

// runBackfill processes every token independently and keeps going on error —
// one token's failure never stops the rest, and (since reconcile is always
// ReconcileToken's desired-vs-tracked diff) re-running this exact function
// again later retries only the tokens that didn't fully succeed, making the
// whole backfill naturally idempotent and resumable.
func runBackfill(ctx context.Context, tokens []store.ParentDeviceToken, lookup topicLookup, reconcile tokenReconciler, log io.Writer) backfillResult {
	var r backfillResult
	for _, dt := range tokens {
		info, ok, err := lookup(ctx, dt.UserID)
		if err != nil {
			fmt.Fprintf(log, "user %s: load assignment data failed: %v\n", dt.UserID, err)
			r.failed++
			continue
		}
		if !ok {
			r.skipped++ // not (or no longer) an active parent row
			continue
		}
		desired, err := notify.ComputeDesiredTopics(info)
		if err != nil {
			fmt.Fprintf(log, "user %s: compute topics failed: %v\n", dt.UserID, err)
			r.failed++
			continue
		}
		if err := reconcile(ctx, info.SchoolID, dt.Token, desired); err != nil {
			// Partial failures are safe to retry: run this command again later
			// and only the still-unreconciled diff is retried (see doc comment).
			fmt.Fprintf(log, "user %s: reconcile incomplete, will retry next run: %v\n", dt.UserID, err)
			r.failed++
			continue
		}
		r.reconciled++
	}
	return r
}
