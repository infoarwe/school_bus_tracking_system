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
	// Students and parents: managed by Super Admin and School Admin; Transport Manager may only view.
	studentAdmins = middleware.RequireRoles(models.RoleSuperAdmin, models.RoleSchoolAdmin)
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
		// Live tracking WebSocket: authenticates itself (?access_token=), see handlers/ws.go.
		r.Get("/ws", a.LiveSocket)

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

				// Driver and Parent apps: scoped to the caller, no school ID in the path.
				r.With(middleware.RequireRoles(models.AppRoles...)).Post("/devices", a.RegisterDevice)
				r.With(middleware.RequireRoles(models.AppRoles...)).Post("/devices/unregister", a.UnregisterDevice)
				r.Route("/driver", func(r chi.Router) {
					r.Use(middleware.RequireRoles(models.RoleDriver))
					r.Get("/me", a.DriverMe)
					r.Get("/trips", a.DriverTrips)
					r.Get("/trips/{tripID}", a.DriverTrip)
					r.Post("/trips/{tripID}/confirm", a.DriverConfirmTrip)
					r.Post("/trips/{tripID}/start", a.DriverStartTrip)
					r.Post("/trips/{tripID}/end", a.DriverEndTrip)
					r.Post("/trips/{tripID}/locations", a.DriverPostLocations)
					r.Get("/trips/{tripID}/progress", a.DriverTripProgress)
					r.Post("/trips/{tripID}/delays", a.DriverReportDelay)
					r.Post("/trips/{tripID}/emergency", a.DriverReportEmergency)
				})
				r.Route("/parent", func(r chi.Router) {
					r.Use(middleware.RequireRoles(models.RoleParent))
					r.Get("/children", a.ParentChildren)
					r.Get("/children/{studentID}", a.ParentChild)
					r.Get("/children/{studentID}/trips", a.ParentChildTrips)
					r.Get("/children/{studentID}/live", a.ParentChildLive)
					r.Get("/notifications", a.ParentNotifications)
					r.Post("/notifications/read", a.MarkNotificationsRead)
				})

				r.With(superAdmin).Get("/audit-logs", a.PlatformAuditLogs)
				r.With(superAdmin).Get("/schools", a.ListSchools)
				r.With(superAdmin).Post("/schools", a.CreateSchool)

				// Everything school-owned lives under /schools/{schoolID}; SchoolScope
				// enforces tenant isolation for all of it.
				r.Route("/schools/{schoolID}", func(r chi.Router) {
					r.Use(middleware.SchoolScope)

					r.With(webRoles).Get("/", a.GetSchool)
					r.With(superAdmin).Put("/", a.UpdateSchool)
					r.With(superAdmin).Patch("/status", a.SetSchoolStatus)
					r.With(webRoles).Get("/settings/maps", a.GetMapsSettings)
					r.With(userAdmins).Put("/settings/maps", a.UpdateMapsSettings)
					r.With(userAdmins).Get("/settings/push", a.GetPushSettings)
					r.With(userAdmins).Put("/settings/push", a.UpdatePushSettings)
					r.With(userAdmins).Delete("/settings/push", a.DeletePushSettings)
					r.With(userAdmins).Post("/settings/push/test", a.TestPushSettings)
					r.With(webRoles).Get("/settings/tracking", a.GetTrackingSettings)
					r.With(userAdmins).Put("/settings/tracking", a.UpdateTrackingSettings)
					r.With(webRoles).Get("/live", a.LiveSnapshot)

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
						r.Get("/routes/{routeID}/students", a.RouteStudents)

						// Read-only for Transport Managers (student data limited to transport needs).
						r.Get("/students", a.ListStudents)
						r.Get("/students/classes", a.StudentClasses)
						r.Get("/students/{studentID}", a.GetStudent)
						r.Get("/students/{studentID}/assignments", a.StudentAssignmentHistory)
						r.Get("/parents", a.ListParents)
						r.Get("/parents/{parentID}", a.GetParent)

						// Daily trip assignment: all three web roles (permission matrix).
						r.Get("/trips", a.ListTrips)
						r.Post("/trips", a.CreateTrip)
						r.Post("/trips/copy", a.CopyTrips)
						r.Get("/trips/{tripID}", a.GetTrip)
						r.Get("/trips/{tripID}/progress", a.TripProgress)
						r.Post("/trips/{tripID}/delays", a.StaffReportDelay)
						r.Get("/trips/{tripID}/track", a.TripTrack)

						// Dashboard, reports, audit log (S8).
						r.Get("/dashboard", a.Dashboard)
						r.Get("/reports", a.ListReports)
						r.Get("/reports/{report}", a.RunReport)
						r.Get("/audit-logs", a.SchoolAuditLogs)

						// Alerts, and announcements (whole-school ones: admins only, checked in the handler).
						r.Get("/alerts", a.SchoolAlerts)
						r.Post("/emergencies/{emergencyID}/acknowledge", a.UpdateEmergency("acknowledged"))
						r.Post("/emergencies/{emergencyID}/resolve", a.UpdateEmergency("resolved"))
						r.Get("/announcements", a.ListAnnouncements)
						r.Post("/announcements", a.CreateAnnouncement)
						r.Post("/announcements/{announcementID}/cancel", a.CancelAnnouncement)
						r.Put("/trips/{tripID}", a.UpdateTrip)
						r.Post("/trips/{tripID}/cancel", a.CancelTrip)
						r.Post("/trips/{tripID}/override", a.OverrideTrip)
					})

					r.Group(func(r chi.Router) {
						r.Use(studentAdmins)

						r.Post("/students", a.CreateStudent)
						r.Post("/students/import", a.ImportStudents)
						r.Put("/students/{studentID}", a.UpdateStudent)
						r.Patch("/students/{studentID}/status", a.SetStudentStatus)
						r.Put("/students/{studentID}/assignment", a.AssignStudent)
						r.Delete("/students/{studentID}/assignment", a.UnassignStudent)

						r.Post("/parents", a.CreateParent)
						r.Put("/parents/{parentID}", a.UpdateParent)
						r.Patch("/parents/{parentID}/status", a.SetParentStatus)
					})
				})
			})
		})
	})

	return r
}
