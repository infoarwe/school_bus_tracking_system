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
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

type Deps struct {
	CORSOrigins          []string
	Health               *handlers.HealthHandler
	API                  *handlers.API
	RequireSuperAdmin2FA bool
}

var (
	superAdmin = middleware.RequireRoles(models.RoleSuperAdmin)
	webRoles   = middleware.RequireRoles(models.WebRoles...)
	userAdmins = middleware.RequireRoles(models.RoleSuperAdmin, models.RoleSchoolAdmin)
	// Transport master data: Super Admin, School Admin and Transport Manager (permission matrix).
	transportStaff = webRoles
)

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

	a := d.API
	r.Route("/api/v1", func(r chi.Router) {
		// Public: login flows.
		r.Post("/auth/login", a.Login)
		r.Post("/auth/login/2fa", a.Login2FA)
		r.Post("/auth/otp/send", a.SendOTP)
		r.Post("/auth/otp/verify", a.VerifyOTP)
		r.Post("/auth/refresh", a.Refresh)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate(a.Tokens, a.Store))

			// Allowed before a Super Admin has set up 2FA.
			r.Get("/auth/me", a.Me)
			r.Post("/auth/logout", a.Logout)
			r.With(webRoles).Post("/auth/2fa/setup", a.SetupTOTP)
			r.With(webRoles).Post("/auth/2fa/enable", a.EnableTOTP)

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireSuperAdmin2FA(d.RequireSuperAdmin2FA))

				r.Get("/auth/sessions", a.ListSessions)
				r.Delete("/auth/sessions/{sessionID}", a.RevokeSession)

				r.With(superAdmin).Get("/schools", a.ListSchools)
				r.With(superAdmin).Post("/schools", a.CreateSchool)

				// Everything school-owned lives under /schools/{schoolID}; SchoolScope
				// enforces tenant isolation for all of it.
				r.Route("/schools/{schoolID}", func(r chi.Router) {
					r.Use(middleware.SchoolScope)

					r.With(webRoles).Get("/", a.GetSchool)
					r.With(superAdmin).Put("/", a.UpdateSchool)
					r.With(superAdmin).Patch("/status", a.SetSchoolStatus)

					r.Route("/users", func(r chi.Router) {
						r.Use(userAdmins)
						r.Get("/", a.ListSchoolUsers)
						r.Post("/", a.CreateSchoolUser)
						r.Get("/{userID}", a.GetSchoolUser)
						r.Put("/{userID}", a.UpdateSchoolUser)
						r.Patch("/{userID}/status", a.SetSchoolUserStatus)
						r.Post("/{userID}/password", a.ResetSchoolUserPassword)
					})

					r.Group(func(r chi.Router) {
						r.Use(transportStaff)

						r.Get("/drivers", a.ListDrivers)
						r.Post("/drivers", a.CreateDriver)
						r.Get("/drivers/{driverID}", a.GetDriver)
						r.Put("/drivers/{driverID}", a.UpdateDriver)
						r.Patch("/drivers/{driverID}/status", a.SetDriverStatus)

						r.Get("/buses", a.ListBuses)
						r.Post("/buses", a.CreateBus)
						r.Get("/buses/{busID}", a.GetBus)
						r.Put("/buses/{busID}", a.UpdateBus)
						r.Patch("/buses/{busID}/status", a.SetBusStatus)

						r.Get("/routes", a.ListRoutes)
						r.Post("/routes", a.CreateRoute)
						r.Get("/routes/{routeID}", a.GetRoute)
						r.Put("/routes/{routeID}", a.UpdateRoute)
						r.Patch("/routes/{routeID}/status", a.SetRouteStatus)
						r.Post("/routes/{routeID}/stops", a.CreateStop)
						r.Put("/routes/{routeID}/stops/order", a.ReorderStops)
						r.Put("/routes/{routeID}/stops/{stopID}", a.UpdateStop)
						r.Delete("/routes/{routeID}/stops/{stopID}", a.DeleteStop)
					})
				})
			})
		})
	})

	return r
}
