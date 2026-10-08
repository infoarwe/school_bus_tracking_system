package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

const (
	purposeAccess = "access"
	purposeMFA    = "mfa"
	issuer        = "sbts"
)

var ErrInvalidToken = errors.New("invalid token")

// Claims inside an access token. Authorization still re-checks the session and
// user in the database on every request, so suspension takes effect immediately.
type Claims struct {
	Role      models.Role `json:"role"`
	SchoolID  *string     `json:"school_id,omitempty"`
	SessionID string      `json:"sid,omitempty"`
	Purpose   string      `json:"purpose"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret     []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	MFATTL     time.Duration
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), AccessTTL: accessTTL, RefreshTTL: refreshTTL, MFATTL: 5 * time.Minute}
}

func (m *TokenManager) IssueAccess(u *models.User, sessionID string) (string, error) {
	return m.sign(Claims{
		Role: u.Role, SchoolID: u.SchoolID, SessionID: sessionID, Purpose: purposeAccess,
		RegisteredClaims: m.registered(u.ID, m.AccessTTL),
	})
}

// IssueMFA returns a short-lived token proving the password step passed; it is
// exchanged for real tokens once the TOTP code is verified.
func (m *TokenManager) IssueMFA(u *models.User) (string, error) {
	return m.sign(Claims{Role: u.Role, Purpose: purposeMFA, RegisteredClaims: m.registered(u.ID, m.MFATTL)})
}

func (m *TokenManager) ParseAccess(token string) (*Claims, error) {
	return m.parse(token, purposeAccess)
}
func (m *TokenManager) ParseMFA(token string) (*Claims, error) { return m.parse(token, purposeMFA) }

func (m *TokenManager) registered(subject string, ttl time.Duration) jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Subject:   subject,
		Issuer:    issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
}

func (m *TokenManager) sign(c Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
}

func (m *TokenManager) parse(token, purpose string) (*Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || c.Purpose != purpose || c.Subject == "" {
		return nil, ErrInvalidToken
	}
	return &c, nil
}

// NewRefreshToken returns an opaque random token and the hash to store.
func NewRefreshToken() (plain, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("random: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(b)
	return plain, HashToken(plain), nil
}

// HashToken is used for refresh tokens and OTP codes: never store them in plain text.
func HashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
