package middleware

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// EchoRequestID sends the request's ID back as X-Request-ID (after chimw.RequestID), so a
// user reporting an error can quote it and it can be found in the server log.
func EchoRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := chimw.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-ID", id)
		}
		next.ServeHTTP(w, r)
	})
}
