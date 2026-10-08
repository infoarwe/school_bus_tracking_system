package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	up := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("unreachable") }

	tests := []struct {
		name       string
		checks     map[string]Pinger
		wantStatus int
		wantBody   string
	}{
		{"all up", map[string]Pinger{"postgres": up, "redis": up}, http.StatusOK, "ok"},
		{"redis down", map[string]Pinger{"postgres": up, "redis": down}, http.StatusServiceUnavailable, "degraded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &HealthHandler{Checks: tt.checks}
			rec := httptest.NewRecorder()
			h.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body struct {
				Data healthResponse `json:"data"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Data.Status != tt.wantBody {
				t.Errorf("body status = %q, want %q", body.Data.Status, tt.wantBody)
			}
		})
	}
}
