package notify

import (
	"context"
	"errors"
	"testing"
)

// fakeSender is a minimal Sender for classifyDelivery tests.
type fakeSender struct{ devOnly bool }

func (fakeSender) Send(context.Context, Push) error { return nil }
func (f fakeSender) IsDevOnly() bool                { return f.devOnly }

func TestClassifyDelivery(t *testing.T) {
	errFCM := errors.New("fcm: 503 unavailable")

	t.Run("sent", func(t *testing.T) {
		o := classifyDelivery(nil, fakeSender{}, nil, 0)
		if o.status != "sent" || o.lastError != "" {
			t.Fatalf("got %+v", o)
		}
	})

	t.Run("dev-only sender is config_missing, never sent, regardless of attempts", func(t *testing.T) {
		o := classifyDelivery(nil, fakeSender{devOnly: true}, nil, 0)
		if o.status != "config_missing" {
			t.Fatalf("got %+v, want config_missing", o)
		}
		if o.lastError == "" {
			t.Error("config_missing must carry an explanatory error, not a blank one masquerading as success")
		}
		// Still config_missing on a later attempt count — this state is terminal,
		// not part of the retry ladder (nothing changes until the school
		// configures a key).
		o2 := classifyDelivery(nil, fakeSender{devOnly: true}, nil, 10)
		if o2.status != "config_missing" {
			t.Fatalf("got %+v, want config_missing", o2)
		}
	})

	t.Run("resolve error retries like a send error", func(t *testing.T) {
		o := classifyDelivery(errors.New("decrypt school FCM key: boom"), nil, nil, 0)
		if o.status != "pending" {
			t.Fatalf("got %+v, want pending", o)
		}
	})

	t.Run("resolve error exhausts into failed, not retried forever", func(t *testing.T) {
		o := classifyDelivery(errors.New("boom"), nil, nil, len(retryDelays))
		if o.status != "failed" {
			t.Fatalf("got %+v, want failed", o)
		}
	})

	t.Run("send error retries until the ladder is exhausted", func(t *testing.T) {
		for attempts := 0; attempts < len(retryDelays); attempts++ {
			o := classifyDelivery(nil, fakeSender{}, errFCM, attempts)
			if o.status != "pending" {
				t.Fatalf("attempt %d: got %+v, want pending", attempts, o)
			}
		}
		o := classifyDelivery(nil, fakeSender{}, errFCM, len(retryDelays))
		if o.status != "failed" {
			t.Fatalf("got %+v, want failed", o)
		}
	})
}
