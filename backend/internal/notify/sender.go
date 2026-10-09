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

// Push is one message to one device.
type Push struct {
	Token string
	Title string
	Body  string
	Data  map[string]string
}

// ErrInvalidToken: the device is gone (app uninstalled, token rotated). Do not retry.
var ErrInvalidToken = errors.New("invalid device token")

type Sender interface {
	Send(ctx context.Context, p Push) error
}

// LogSender writes pushes to the log instead of sending them. Used when no
// Firebase service account is configured (development, tests).
type LogSender struct{}

func (LogSender) Send(_ context.Context, p Push) error {
	slog.Info("push (dev: not sent)", "title", p.Title, "body", p.Body, "type", p.Data["type"])
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

func (s *FCMSender) Send(ctx context.Context, p Push) error {
	// Android: high priority so stop alerts are timely; the app shows them in its
	// "bus_updates" notification channel. Data carries IDs for deep links.
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"token":        p.Token,
		"notification": map[string]string{"title": p.Title, "body": p.Body},
		"data":         p.Data,
		"android": map[string]any{
			"priority":     "HIGH",
			"notification": map[string]string{"channel_id": "bus_updates"},
		},
	}})
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
	// UNREGISTERED (404) or an invalid token (400 INVALID_ARGUMENT): drop the device.
	if res.StatusCode == http.StatusNotFound ||
		(res.StatusCode == http.StatusBadRequest && strings.Contains(string(msg), "registration token")) {
		return ErrInvalidToken
	}
	return fmt.Errorf("fcm: %s: %s", res.Status, strings.TrimSpace(string(msg)))
}
