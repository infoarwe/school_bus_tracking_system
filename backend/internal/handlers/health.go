// Package handlers holds the HTTP handlers for the API.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
)

// Pinger is anything with a health check: the Postgres pool and Redis client both fit.
type Pinger func(ctx context.Context) error

type HealthHandler struct {
	Checks map[string]Pinger
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Health reports "ok" only when every dependency answers within 2 seconds.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	resp := healthResponse{Status: "ok", Checks: map[string]string{}}
	for name, ping := range h.Checks {
		if err := ping(ctx); err != nil {
			resp.Status = "degraded"
			resp.Checks[name] = "down"
			continue
		}
		resp.Checks[name] = "up"
	}

	status := http.StatusOK
	if resp.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	httpx.JSON(w, status, resp)
}
