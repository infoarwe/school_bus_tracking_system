package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
)

// Recover turns a panic into a 500 JSON error and logs the stack trace.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.Error("panic recovered",
					"request_id", chimw.GetReqID(r.Context()),
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				httpx.Error(w, http.StatusInternalServerError, "internal_error", "Something went wrong.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
