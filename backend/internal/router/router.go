// Package router wires middleware and handlers into the HTTP routes.
package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/handlers"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/middleware"
)

type Deps struct {
	CORSOrigins []string
	Health      *handlers.HealthHandler
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recover)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Route not found.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	})

	r.Get("/health", d.Health.Health)
	r.Get("/openapi.yaml", handlers.OpenAPISpec)
	r.Get("/docs", handlers.Docs)

	r.Route("/api/v1", func(r chi.Router) {
		// Sprint 1+: auth, schools, users, ... are mounted here.
	})

	return r
}
