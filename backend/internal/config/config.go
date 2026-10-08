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
}

func Load() (*Config, error) {
	_ = godotenv.Load() // optional; real env vars take precedence

	shutdown, err := time.ParseDuration(get("SHUTDOWN_TIMEOUT", "10s"))
	if err != nil {
		return nil, fmt.Errorf("invalid SHUTDOWN_TIMEOUT: %w", err)
	}

	cfg := &Config{
		Env:             get("APP_ENV", "development"),
		HTTPAddr:        ":" + get("PORT", "8085"),
		DatabaseURL:     get("DATABASE_URL", ""),
		RedisURL:        get("REDIS_URL", "redis://localhost:6379/0"),
		CORSOrigins:     splitCSV(get("CORS_ORIGINS", "http://localhost:5180")),
		ShutdownTimeout: shutdown,
		LogLevel:        get("LOG_LEVEL", "info"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(cfg.HTTPAddr, ":")); err != nil {
		return nil, fmt.Errorf("invalid PORT: %w", err)
	}
	return cfg, nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

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
