// Command api runs the School Bus Tracking HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // school time zones work on hosts without a tz database

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/config"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/database"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/handlers"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/jobs"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/ratelimit"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/router"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/secrets"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/storage"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

func main() {
	// `api healthcheck`: exit 0 if this container's API answers /health (Docker HEALTHCHECK;
	// the runtime image has no shell or curl).
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateForAPI(); err != nil {
		return err
	}
	setupLogger(cfg)
	if cfg.OTPDevCode != "" {
		slog.Warn("OTP_DEV_CODE is set: every OTP sent is this fixed code (development only)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	rdb, err := database.NewRedis(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	st := store.New(db)
	box, err := secrets.New(cfg.DataEncryptionKey)
	if err != nil {
		return fmt.Errorf("DATA_ENCRYPTION_KEY: %w", err)
	}

	files, err := storage.New(cfg.StorageDriver, cfg.UploadDir)
	if err != nil {
		return err
	}
	slog.Info("file storage ready", "driver", cfg.StorageDriver, "dir", cfg.UploadDir)

	var limiter *ratelimit.Limiter
	if cfg.RateLimitEnabled {
		limiter = ratelimit.New(rdb, cfg.RateLimitRules())
	} else {
		slog.Warn("rate limiting is OFF (RATE_LIMIT_ENABLED=false)")
	}

	live := tracking.NewLive(rdb)
	hub := tracking.NewHub(live)
	go hub.Run(ctx)
	go jobs.StaleSweep(ctx, st, live, 30*time.Second)
	go jobs.LocationRetention(ctx, st, time.Hour)

	// Fallback for schools that have not uploaded their own Firebase key.
	var fallback notify.Sender = notify.LogSender{}
	if cfg.FCMCredentialsFile != "" {
		fcm, err := notify.NewFCMSender(ctx, cfg.FCMCredentialsFile)
		if err != nil {
			return err
		}
		fallback = fcm
		slog.Info("server-wide Firebase key loaded (used by schools without their own)", "credentials", cfg.FCMCredentialsFile)
	} else {
		slog.Info("no server-wide Firebase key: schools without their own key (Settings → Push) only log pushes")
	}
	worker := &notify.Worker{Store: st, Senders: &notify.SchoolSenders{
		Store: st, Secrets: box, Fallback: fallback,
		NewSender: func(ctx context.Context, raw []byte) (notify.Sender, error) { return notify.NewFCMSenderJSON(ctx, raw) },
	}}
	go worker.Run(ctx, 2*time.Second)

	api := &handlers.API{
		Store:                st,
		Tokens:               auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL),
		SMS:                  auth.LogSender{}, // TODO(S0-03): real SMS provider
		Secrets:              box,
		Live:                 live,
		Hub:                  hub,
		ETA:                  tracking.NewGoogleRoutes(),
		CheckPushKey:         handlers.CheckFCMKey,
		WSOriginPatterns:     originHosts(cfg.CORSOrigins),
		Limiter:              limiter,
		Files:                files,
		MaxLogoBytes:         cfg.MaxLogoBytes,
		OTPTTL:               cfg.OTPTTL,
		OTPDevCode:           cfg.OTPDevCode,
		RequireSuperAdmin2FA: cfg.RequireSuperAdmin2FA,
	}
	go jobs.Announcements(ctx, 30*time.Second, api.SendDueAnnouncements)

	handler := router.New(router.Deps{
		CORSOrigins: cfg.CORSOrigins,
		Health: &handlers.HealthHandler{Checks: map[string]handlers.Pinger{
			"postgres": db.Ping,
			"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		}},
		API:                  api,
		RequireSuperAdmin2FA: cfg.RequireSuperAdmin2FA,
		TrustProxyHeaders:    cfg.TrustProxyHeaders,
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func healthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", res.StatusCode)
		return 1
	}
	return 0
}

// originHosts turns CORS origins ("http://localhost:5180") into WebSocket origin
// patterns ("localhost:5180"). The apps send no Origin header and are always allowed.
func originHosts(origins []string) []string {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}

func setupLogger(cfg *config.Config) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}

	var h slog.Handler = slog.NewTextHandler(os.Stdout, opts)
	if cfg.IsProduction() {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(h))
}
