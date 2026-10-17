package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Push is one message to a topic. Delivery is topic-only: every subscriber of
// Topic receives it in a single Firebase call — there is no per-device token
// send path in production (see internal/notify/subscriptions.go for how
// devices get subscribed to topics in the first place).
type Push struct {
	Topic string
	Title string
	Body  string
	Data  map[string]string
}

// ErrInvalidToken: the device is gone (app uninstalled, token rotated) — used
// by subscribe/unsubscribe calls, not by Send (which no longer takes a token).
var ErrInvalidToken = errors.New("invalid device token")

type Sender interface {
	Send(ctx context.Context, p Push) error
}

// TopicSubscriber manages which device tokens receive a topic's pushes.
// Implemented by *FCMSender; LogSender (and any other DevOnly sender) does
// not implement it — callers must check before attempting a subscribe/
// unsubscribe call (see notify.IsDevOnly).
type TopicSubscriber interface {
	SubscribeTokens(ctx context.Context, tokens []string, topic string) (failed []string, err error)
	UnsubscribeTokens(ctx context.Context, tokens []string, topic string) (failed []string, err error)
}

// DevOnly marks a Sender that never actually reaches Firebase (LogSender).
// Callers must check this before recording a delivery as "sent": a nil error
// from a dev-only sender means "logged, not delivered", not "delivered".
type DevOnly interface {
	IsDevOnly() bool
}

// IsDevOnly reports whether s is a sender that never contacts Firebase.
func IsDevOnly(s Sender) bool {
	d, ok := s.(DevOnly)
	return ok && d.IsDevOnly()
}

// LogSender writes pushes to the log instead of sending them. Used when no
// Firebase service account is configured (development, tests). It must never
// be mistaken for a real delivery — callers check IsDevOnly first and record
// 'config_missing', not 'sent' (see notify.Worker.SendDue).
type LogSender struct{}

func (LogSender) IsDevOnly() bool { return true }

func (LogSender) Send(_ context.Context, p Push) error {
	slog.Info("push (dev: not sent)", "topic", p.Topic, "title", p.Title, "body", p.Body, "type", p.Data["type"])
	return nil
}

// FCMSender sends through the Firebase Cloud Messaging HTTP v1 API with a
// service account: a school's own Firebase project, or the server-wide fallback key.
type FCMSender struct {
	projectID string
	client    *http.Client
	endpoint  string
}

// NewFCMSender reads a Firebase service-account JSON file (server-wide fallback key).
func NewFCMSender(ctx context.Context, credentialsFile string) (*FCMSender, error) {
	raw, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("read FCM credentials: %w", err)
	}
	return NewFCMSenderJSON(ctx, raw)
}

// NewFCMSenderJSON builds a sender from a service-account JSON (a school's uploaded key).
func NewFCMSenderJSON(ctx context.Context, raw []byte) (*FCMSender, error) {
	creds, err := google.CredentialsFromJSON(ctx, raw, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("parse FCM credentials: %w", err)
	}
	if creds.ProjectID == "" {
		return nil, errors.New("FCM credentials have no project_id")
	}
	// The token source lives as long as the sender, not the request that built it.
	client := oauth2.NewClient(context.Background(), creds.TokenSource)
	client.Timeout = 10 * time.Second
	return &FCMSender{
		projectID: creds.ProjectID,
		client:    client,
		endpoint:  "https://fcm.googleapis.com/v1/projects/" + creds.ProjectID + "/messages:send",
	}, nil
}

// CheckAuth gets an access token: proves the key is valid and not revoked.
func (s *FCMSender) CheckAuth(ctx context.Context) error {
	ts, ok := s.client.Transport.(*oauth2.Transport)
	if !ok {
		return errors.New("unexpected FCM client")
	}
	if _, err := ts.Source.Token(); err != nil {
		return fmt.Errorf("google rejected the key: %w", err)
	}
	return nil
}

// buildFCMMessage builds the FCM HTTP v1 request body for p. Pure and
// network-free (no oauth2/HTTP involved) so it's directly unit-tested for
// payload shape — in particular, that it carries "topic" and never "token"
// (see sender_test.go) — without contacting Firebase or Google OAuth.
func buildFCMMessage(p Push) ([]byte, error) {
	if p.Topic == "" {
		return nil, errors.New("fcm: Push.Topic is required (topic-only delivery)")
	}
	// Android: high priority so stop alerts are timely; the app shows them in its
	// "bus_updates" notification channel. Data carries IDs for deep links.
	return json.Marshal(map[string]any{"message": map[string]any{
		"topic":        p.Topic,
		"notification": map[string]string{"title": p.Title, "body": p.Body},
		"data":         p.Data,
		"android": map[string]any{
			"priority":     "HIGH",
			"notification": map[string]string{"channel_id": "bus_updates"},
		},
	}})
}

// Send delivers to every current subscriber of p.Topic in one call — there is
// no per-device token send path (see Push's doc comment).
func (s *FCMSender) Send(ctx context.Context, p Push) error {
	body, err := buildFCMMessage(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fcm: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	return fmt.Errorf("fcm: %s: %s", res.Status, strings.TrimSpace(string(msg)))
}

// topicSubscriptionCall manages one token's subscription to one topic via the
// current FCM v1 topic-subscription REST API (POST .../topicSubscriptions to
// subscribe, DELETE .../topicSubscriptions/{topic} to unsubscribe) — the
// per-registration successor to the legacy Instance ID batchAdd/batchRemove
// API (iid.googleapis.com), which Google's front end now rejects outright
// with a generic 401 "Unauthorized" HTML page for every request, regardless
// of credential validity (confirmed by a live diagnostic call; see the audit
// this fix came from). There is no batch form in the new API — every real
// caller in this codebase already only ever passes one token at a time (see
// SubscribeTokens/UnsubscribeTokens below), so looping per-token here changes
// nothing about the calling code's shape.
func (s *FCMSender) topicSubscriptionCall(ctx context.Context, method, token, topic string) error {
	url := "https://fcm.googleapis.com/v1/projects/" + s.projectID + "/registrations/" + token + "/topicSubscriptions"
	if method == http.MethodPost {
		url += "?topic_name=" + topic
	} else {
		url += "/" + topic
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fcm topic subscription: %w", err)
	}
	defer res.Body.Close()
	// Subscribing when already subscribed reports 409 - the desired state
	// (subscribed) already holds, so this counts as success, not a failure.
	if res.StatusCode == http.StatusOK || (method == http.MethodPost && res.StatusCode == http.StatusConflict) {
		return nil
	}
	// Read generously (Firebase error bodies are small JSON, well under this)
	// before truncating, so every occurrence of token below is caught whole —
	// truncating first could cut a repeated occurrence mid-token and leave a
	// readable fragment behind.
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	if res.StatusCode == http.StatusNotFound {
		return ErrInvalidToken
	}
	// Firebase's error body echoes the request resource path, which contains
	// token verbatim (sometimes more than once, e.g. in both "message" and
	// "metadata") — strip every occurrence so a reconcile-failure log line
	// (ReconcileToken -> notifications.go's slog.Warn) never carries the
	// device's FCM token.
	sanitized := strings.ReplaceAll(strings.TrimSpace(string(msg)), token, "<redacted>")
	if len(sanitized) > 1024 {
		sanitized = sanitized[:1024]
	}
	return fmt.Errorf("fcm topic subscription: %s: %s", res.Status, sanitized)
}

// topicSubscriptionBatch calls topicSubscriptionCall for each token, in the
// shape ReconcileToken expects: tokens the API reports as gone go in failed
// (the caller must not record those as subscribed/unsubscribed); the first
// other error aborts the loop and is returned as err, leaving any
// not-yet-attempted tokens untouched for the next reconciliation to retry.
func (s *FCMSender) topicSubscriptionBatch(ctx context.Context, method string, tokens []string, topic string) (failed []string, err error) {
	for _, token := range tokens {
		callErr := s.topicSubscriptionCall(ctx, method, token, topic)
		switch {
		case callErr == nil:
		case errors.Is(callErr, ErrInvalidToken):
			failed = append(failed, token)
		default:
			return failed, callErr
		}
	}
	return failed, nil
}

// SubscribeTokens subscribes the given device tokens to topic. The returned
// slice lists tokens Firebase reported as failed — the caller must not record
// those as subscribed.
func (s *FCMSender) SubscribeTokens(ctx context.Context, tokens []string, topic string) (failed []string, err error) {
	return s.topicSubscriptionBatch(ctx, http.MethodPost, tokens, topic)
}

// UnsubscribeTokens unsubscribes the given device tokens from topic. The
// returned slice lists tokens Firebase reported as failed — the caller must
// keep tracking those as still subscribed so a later reconciliation retries.
func (s *FCMSender) UnsubscribeTokens(ctx context.Context, tokens []string, topic string) (failed []string, err error) {
	return s.topicSubscriptionBatch(ctx, http.MethodDelete, tokens, topic)
}
