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
	iidBase   string // topic-management API; empty = iidBaseURL (tests point it at a fake)
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

// iidBaseURL is Firebase's topic-management API (Instance ID batchAdd /
// batchRemove). With a service-account OAuth token it needs the header
// "access_token_auth: true"; without it Google answers 401 to every request.
// The per-registration FCM v1 endpoint (.../registrations/{token}/topicSubscriptions)
// is not usable here: it answers 403 'cloudmessaging.topicSubscriptions.create'
// denied even for a Firebase Admin SDK key with full rights (checked against
// project arwe-school on 2026-10-17, where batchAdd answered 200 for the same token).
const iidBaseURL = "https://iid.googleapis.com/iid/v1"

// iidMaxTokens is Firebase's per-request limit for batchAdd/batchRemove.
const iidMaxTokens = 1000

// topicSubscriptionBatch subscribes (batchAdd) or unsubscribes (batchRemove)
// tokens, in the shape ReconcileToken expects: tokens Firebase reports as gone
// (NOT_FOUND, INVALID_ARGUMENT) go in failed (the caller must not record those
// as subscribed/unsubscribed); any other error aborts and is returned as err,
// leaving not-yet-confirmed tokens for the next reconciliation to retry.
func (s *FCMSender) topicSubscriptionBatch(ctx context.Context, action string, tokens []string, topic string) (failed []string, err error) {
	base := s.iidBase
	if base == "" {
		base = iidBaseURL
	}
	for start := 0; start < len(tokens); start += iidMaxTokens {
		chunk := tokens[start:min(start+iidMaxTokens, len(tokens))]
		gone, err := s.iidCall(ctx, base+":"+action, chunk, topic)
		failed = append(failed, gone...)
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

// iidCall sends one batchAdd/batchRemove request. Firebase answers 200 with one
// result per token, in order: {} on success, {"error": "NOT_FOUND"} etc. otherwise.
func (s *FCMSender) iidCall(ctx context.Context, url string, tokens []string, topic string) (gone []string, err error) {
	body, err := json.Marshal(map[string]any{"to": "/topics/" + topic, "registration_tokens": tokens})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("access_token_auth", "true")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fcm topic subscription: %w", err)
	}
	defer res.Body.Close()
	// Read generously before truncating so every token in the body is redacted whole.
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fcm topic subscription: %s: %s", res.Status, redactTokens(string(msg), tokens))
	}
	var out struct {
		Results []struct {
			Error string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(msg, &out); err != nil || len(out.Results) != len(tokens) {
		return nil, fmt.Errorf("fcm topic subscription: unexpected response: %s", redactTokens(string(msg), tokens))
	}
	var other string
	for i, r := range out.Results {
		switch r.Error {
		case "":
		case "NOT_FOUND", "INVALID_ARGUMENT":
			gone = append(gone, tokens[i])
		default:
			if other == "" {
				other = r.Error
			}
		}
	}
	if other != "" {
		return gone, fmt.Errorf("fcm topic subscription: %s", other)
	}
	return gone, nil
}

// redactTokens strips device tokens from a Firebase reply so log lines
// (ReconcileToken -> notifications.go's slog.Warn) never carry them.
func redactTokens(msg string, tokens []string) string {
	msg = strings.TrimSpace(msg)
	for _, t := range tokens {
		msg = strings.ReplaceAll(msg, t, "<redacted>")
	}
	if len(msg) > 1024 {
		msg = msg[:1024]
	}
	return msg
}

// SubscribeTokens subscribes the given device tokens to topic. The returned
// slice lists tokens Firebase reported as failed — the caller must not record
// those as subscribed.
func (s *FCMSender) SubscribeTokens(ctx context.Context, tokens []string, topic string) (failed []string, err error) {
	return s.topicSubscriptionBatch(ctx, "batchAdd", tokens, topic)
}

// UnsubscribeTokens unsubscribes the given device tokens from topic. The
// returned slice lists tokens Firebase reported as failed — the caller must
// keep tracking those as still subscribed so a later reconciliation retries.
func (s *FCMSender) UnsubscribeTokens(ctx context.Context, tokens []string, topic string) (failed []string, err error) {
	return s.topicSubscriptionBatch(ctx, "batchRemove", tokens, topic)
}
