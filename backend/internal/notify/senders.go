package notify

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/secrets"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// Senders picks the sender for a school's pushes.
type Senders interface {
	For(ctx context.Context, schoolID string) (Sender, error)
}

// Static uses one sender for every school (tests, or a single shared app).
type Static struct{ Sender Sender }

func (s Static) For(context.Context, string) (Sender, error) { return s.Sender, nil }

// SchoolSenders sends each school's pushes through that school's own Firebase
// project (uploaded in Settings → Push notifications). A school without its own
// key uses Fallback: the server-wide key file if configured, else the log.
type SchoolSenders struct {
	Store    *store.Store
	Secrets  *secrets.Box
	Fallback Sender
	// NewSender builds a sender from a service-account JSON (NewFCMSenderJSON; replaceable in tests).
	NewSender func(ctx context.Context, credentialsJSON []byte) (Sender, error)

	mu    sync.Mutex
	cache map[string]cachedSender // by school; rebuilt when the key changes
}

type cachedSender struct {
	updatedAt time.Time
	sender    Sender
}

func (s *SchoolSenders) For(ctx context.Context, schoolID string) (Sender, error) {
	ps, err := store.GetPushSettings(ctx, s.Store.Pool, schoolID)
	if err != nil {
		return nil, err
	}
	if ps.CredentialsEncrypted == nil || ps.UpdatedAt == nil {
		return s.Fallback, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cache[schoolID]; ok && c.updatedAt.Equal(*ps.UpdatedAt) {
		return c.sender, nil
	}
	raw, err := s.Secrets.Decrypt(ps.CredentialsEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt school FCM key: %w", err)
	}
	sender, err := s.NewSender(ctx, []byte(raw))
	if err != nil {
		return nil, fmt.Errorf("school FCM key: %w", err)
	}
	if s.cache == nil {
		s.cache = map[string]cachedSender{}
	}
	s.cache[schoolID] = cachedSender{updatedAt: *ps.UpdatedAt, sender: sender}
	return sender, nil
}

// ServiceAccount is what we check in an uploaded Firebase key.
type ServiceAccount struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

// ErrBadServiceAccount explains why an uploaded file is not a usable Firebase key.
type ErrBadServiceAccount struct{ Reason string }

func (e *ErrBadServiceAccount) Error() string { return e.Reason }

// ParseServiceAccount validates a Firebase service-account JSON without calling Google.
func ParseServiceAccount(raw []byte) (*ServiceAccount, error) {
	var sa ServiceAccount
	if err := json.Unmarshal(raw, &sa); err != nil {
		return nil, &ErrBadServiceAccount{"The file is not valid JSON. Upload the .json key downloaded from Firebase."}
	}
	switch {
	case sa.Type != "service_account":
		return nil, &ErrBadServiceAccount{`This is not a service-account key ("type" must be "service_account"). In Firebase: Project settings → Service accounts → Generate new private key.`}
	case sa.ProjectID == "":
		return nil, &ErrBadServiceAccount{"The key has no project_id."}
	case !strings.Contains(sa.ClientEmail, "@"):
		return nil, &ErrBadServiceAccount{"The key has no client_email."}
	}
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, &ErrBadServiceAccount{"The key's private_key is missing or damaged."}
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		return nil, &ErrBadServiceAccount{"The key's private_key cannot be read."}
	}
	return &sa, nil
}

// IsBadServiceAccount reports whether err is a validation problem with the uploaded file.
func IsBadServiceAccount(err error) (*ErrBadServiceAccount, bool) {
	var e *ErrBadServiceAccount
	ok := errors.As(err, &e)
	return e, ok
}
