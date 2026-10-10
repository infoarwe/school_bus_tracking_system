package config

import (
	"strings"
	"testing"
)

func TestValidateForAPIProduction(t *testing.T) {
	good := func() *Config {
		return &Config{Env: "production", JWTSecret: strings.Repeat("k", 64), DataEncryptionKey: strings.Repeat("ab", 32),
			RequireSuperAdmin2FA: true, CORSOrigins: []string{"https://sbts.example.org"}}
	}
	if err := good().ValidateForAPI(); err != nil {
		t.Fatalf("valid production config refused: %v", err)
	}
	for name, change := range map[string]func(*Config){
		"placeholder JWT secret": func(c *Config) { c.JWTSecret = "change-me-to-a-random-string-of-at-least-32-chars" },
		"short JWT secret":       func(c *Config) { c.JWTSecret = "short" },
		"zero encryption key":    func(c *Config) { c.DataEncryptionKey = strings.Repeat("0", 64) },
		"wildcard CORS":          func(c *Config) { c.CORSOrigins = []string{"*"} },
		"dev OTP code":           func(c *Config) { c.OTPDevCode = "123456" },
		"2FA off":                func(c *Config) { c.RequireSuperAdmin2FA = false },
	} {
		c := good()
		change(c)
		if err := c.ValidateForAPI(); err == nil {
			t.Errorf("%s: production must refuse to start", name)
		}
	}
	// Development keeps its conveniences.
	dev := good()
	dev.Env, dev.JWTSecret, dev.OTPDevCode = "development", "change-me-to-a-random-string-of-at-least-32-chars", "123456"
	if err := dev.ValidateForAPI(); err != nil {
		t.Errorf("development config refused: %v", err)
	}
}
