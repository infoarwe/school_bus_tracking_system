// Package config loads application settings from environment variables.
// A .env file in the working directory is loaded first, if present.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env             string // development | staging | production
	HTTPAddr        string
	DatabaseURL     string
	RedisURL        string
	CORSOrigins     []string
	ShutdownTimeout time.Duration
	LogLevel        string

	JWTSecret            string
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	OTPTTL               time.Duration
	OTPDevCode           string // fixed OTP for dev/testing; refused in production
	RequireSuperAdmin2FA bool

	// DataEncryptionKey (64 hex chars) encrypts stored third-party secrets such as
	// each school's Google Maps server key. Changing it makes stored secrets unreadable.
	DataEncryptionKey string

	// FCMCredentialsFile is the Firebase service-account JSON (a separate secret file, never in
	// .env or git). Default: secrets/firebase-service-account.json if it exists. None: pushes are only logged.
	FCMCredentialsFile string
}

func Load() (*Config, error) {
	_ = godotenv.Load() // optional; real env vars take precedence

	var errs []string
	duration := func(key, fallback string) time.Duration {
		d, err := time.ParseDuration(get(key, fallback))
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid %s: %v", key, err))
		}
		return d
	}
	boolean := func(key string, fallback bool) bool {
		v, ok := os.LookupEnv(key)
		if !ok || v == "" {
			return fallback
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid %s: %v", key, err))
		}
		return b
	}

	cfg := &Config{
		Env:             get("APP_ENV", "development"),
		HTTPAddr:        ":" + get("PORT", "8085"),
		DatabaseURL:     get("DATABASE_URL", ""),
		RedisURL:        get("REDIS_URL", "redis://localhost:6379/0"),
		CORSOrigins:     splitCSV(get("CORS_ORIGINS", "http://localhost:5180")),
		ShutdownTimeout: duration("SHUTDOWN_TIMEOUT", "10s"),
		LogLevel:        get("LOG_LEVEL", "info"),

		JWTSecret:            get("JWT_SECRET", ""),
		AccessTokenTTL:       duration("ACCESS_TOKEN_TTL", "15m"),
		RefreshTokenTTL:      duration("REFRESH_TOKEN_TTL", "720h"),
		OTPTTL:               duration("OTP_TTL", "5m"),
		OTPDevCode:           get("OTP_DEV_CODE", ""),
		RequireSuperAdmin2FA: boolean("REQUIRE_SUPER_ADMIN_2FA", true),
		DataEncryptionKey:    get("DATA_ENCRYPTION_KEY", ""),
		FCMCredentialsFile:   fcmCredentialsFile(),
	}

	if cfg.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(cfg.HTTPAddr, ":")); err != nil {
		errs = append(errs, fmt.Sprintf("invalid PORT: %v", err))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return cfg, nil
}

// ValidateForAPI checks the settings only the API server needs (not migrate/seed).
func (c *Config) ValidateForAPI() error {
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("config: JWT_SECRET must be at least 32 characters")
	}
	if len(c.DataEncryptionKey) != 64 {
		return fmt.Errorf("config: DATA_ENCRYPTION_KEY must be 64 hex characters (generate with: openssl rand -hex 32)")
	}
	if c.IsProduction() && c.DataEncryptionKey == strings.Repeat("0", 64) {
		return fmt.Errorf("config: set a real DATA_ENCRYPTION_KEY in production")
	}
	if c.IsProduction() && c.OTPDevCode != "" {
		return fmt.Errorf("config: OTP_DEV_CODE must not be set in production")
	}
	if c.IsProduction() && !c.RequireSuperAdmin2FA {
		return fmt.Errorf("config: REQUIRE_SUPER_ADMIN_2FA cannot be disabled in production")
	}
	return nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

// DefaultFCMCredentialsFile is where the Firebase key is looked for when
// FCM_CREDENTIALS_FILE is not set (see backend/secrets/README.md).
const DefaultFCMCredentialsFile = "secrets/firebase-service-account.json"

func fcmCredentialsFile() string {
	if v := get("FCM_CREDENTIALS_FILE", ""); v != "" {
		return v
	}
	if _, err := os.Stat(DefaultFCMCredentialsFile); err == nil {
		return DefaultFCMCredentialsFile
	}
	return ""
}

func get(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
