# CLAUDE.md

Guidance for Claude (and humans) working on **school_bus_tracking_system** (School Bus Tracking Software, Version 1).

Local project folder: `C:\projects\school_bus_tracking_system`

Source documents (in `uploads/hearth/`): Complete Module List v1, User Roles & Permissions v1, Project Flow v1. When this file and those documents disagree, the documents win; update this file.

## Scope of this repo

This repo holds the **Go API** and the **React admin web** only. The Android Driver and Parent apps are built by a separate **mobile team** against our API. For anything the apps need (auth, driver trips, GPS upload, WebSocket, parent tracking, push), our deliverable is a working endpoint plus its documentation in `backend/api/openapi.yaml`, ready on an environment the mobile team can reach. Handoffs are listed in `docs/SPRINT_PLAN.md` (tag `API`).

## Product in one line

Schools create routes with ordered stops, assign each student to a route and stop, assign a driver + bus + route + trip **per day**, and parents watch the bus live with ETA and automatic stop notifications.

Core flow: **Route → Stops → Student → Parent → Daily Driver/Bus Assignment → Live GPS → ETA → Notifications**

## Tech stack

Stated in the source documents:
- **Backend:** Go (location ingestion, APIs, real-time engine)
- **Live location cache:** Redis (current bus location)
- **Real-time delivery:** WebSocket (to parent app and admin web)
- **Mobile apps:** Android (Driver app, Parent app), with background GPS and push notifications. **Built by a separate mobile team, not in this repo.**
- **Admin:** Web app (Super Admin, School Admin, Transport Manager)

Chosen:
- Primary database: PostgreSQL 17 (pgx/v5), migrations with goose (SQL files embedded in the binary)
- Backend libraries: chi router, go-redis v9, log/slog
- Admin web: React 19 + TypeScript + Vite, Ant Design, React Router, axios, in `frontend/`
- Push provider: FCM (planned for Sprint 7)

Not yet decided (fill in when chosen):
- Android language/framework: owned by the mobile team (not in this repo)
- OTP/SMS provider: `TODO` (needed in Sprint 1, S1-03; stub until chosen)
- Maps/routing provider: `TODO` (needed in Sprint 2, S2-11)
- Hosting/deployment: `TODO`

## Repository layout

- `backend/`: Go API
  - `cmd/api`: HTTP server entry point; `cmd/migrate`: migration runner
  - `internal/config`: env config; `internal/database`: Postgres, Redis, `migrations/*.sql`
  - `internal/httpx`: shared JSON response/error helpers; `internal/middleware`; `internal/handlers`; `internal/router`; `internal/models`
- `frontend/`: React admin web (`src/{components,context,layouts,pages,routes,services,styles,utils}`); `src/routes/menu.tsx` lists the sidebar pages
- `backend/api/openapi.yaml`: the API contract, embedded and served at `/docs` (Swagger UI) and `/openapi.yaml`
- `docker-compose.yml`: local Postgres + Redis
- `docs/SPRINT_PLAN.md`: sprint and task plan; tick tasks there as they complete

## Commands

Local ports: API **8085**, web **5180**, Postgres **5433** (host), Redis **6379**. The defaults 8080/5173/5432 are taken on the main dev machine by other services.

```sh
docker compose up -d                       # Postgres + Redis
cd backend && cp .env.example .env         # first time only
go run ./cmd/migrate up                    # also: down, status, redo
go run ./cmd/api                           # http://localhost:8085/health, API docs at /docs
go test ./... && go vet ./...
cd frontend && npm install && npm run dev  # http://localhost:5180 (proxies /api, /health)
npm run lint && npm run format:check && npm run build
```

## Roles

| Role | Platform | Scope |
|---|---|---|
| Super Admin | Web | Global. Manages schools/tenants, school admins, global settings, subscriptions, audit logs. Mandatory 2FA recommended. |
| School Admin | Web | One school. Students, parents, routes, stops, buses, drivers, assignments, announcements, reports. |
| Transport Manager | Web | One school, transport only. Drivers, buses, routes, daily assignments, live monitoring, trip override, delays, emergencies, route notifications. No high-level school config; student data limited to transport needs. |
| Driver | Android | Own assigned trips only. Login, confirm/start/end trip, share GPS, report delay/breakdown/emergency. |
| Parent | Android | Linked children only. Track bus, ETA, status, notifications, switch child. Cannot change any assignment. |
| Student | (record) | Data record linked to parent, route, stop. No login in V1. |

Hierarchy: Super Admin → School Admin → Transport Manager → Driver. Parent sees only linked Students → Route → Stop → Bus Trip.

### Permission matrix

| Feature | Super | School | Transport | Driver | Parent |
|---|---|---|---|---|---|
| Manage Schools | ✓ | — | — | — | — |
| Manage School Admins | ✓ | — | — | — | — |
| Manage Students | ✓ | ✓ | View | — | View linked |
| Manage Parents | ✓ | ✓ | View | — | Own |
| Manage Routes / Stops | ✓ | ✓ | ✓ | View | View assigned |
| Manage Buses | ✓ | ✓ | ✓ | View assigned | View assigned |
| Manage Drivers | ✓ | ✓ | ✓ | Own profile | — |
| Daily Trip Assignment | ✓ | ✓ | ✓ | View | View |
| Live Bus Tracking | ✓ | ✓ | ✓ | Own trip | Linked child |
| Trip History | ✓ | ✓ | ✓ | Own trips | Linked child |
| Delay / Emergency | ✓ | ✓ | ✓ | Report | View |
| School Notification | ✓ | ✓ | — | — | Receive |
| Route Notification | ✓ | ✓ | ✓ | — | Receive |
| Global Settings | ✓ | Limited | — | — | — |
| Audit Logs | ✓ | School | Transport | Own activity | — |

### Authentication

- Parent: mobile number + OTP.
- Driver: mobile number + OTP, or secure staff auth.
- School Admin / Transport Manager: secure login, OTP/2FA optional.
- Super Admin: strong auth, 2FA recommended as mandatory.
- All: token expiry, logout, device/session control, account suspension.

## Non-negotiable rules

These come straight from the documents. Code that breaks one is a bug.

1. **Multi-tenant isolation.** Every operational user belongs to one school, except Super Admin. Every query and API is scoped by school. A School Admin can never see another school's data.
2. **RBAC is enforced on the backend.** Hiding UI is not enough; every protected API (and every WebSocket subscription) validates role **and** school scope.
3. **Daily assignment, never permanent.** `Driver + Bus + Route + Date + Trip Type = Daily Assignment`. There is no mandatory permanent Driver → Bus → Route mapping. Assignments are separate from master records.
4. **Morning Pickup and Evening Drop are separate trip types**, with their own assignments and clearly distinguished in the parent app.
5. **Routes have ordered stops.** A student belongs to exactly one route and a specific stop (pickup and drop stop as applicable). When assigning, only stops of the selected route are offered. Parents never pick routes.
6. **Parents see only linked children**, and live location only for the relevant child's trip/route. Never expose other students' personal data.
7. **Drivers see only their assigned/current trips.** They cannot pick students or edit routes or student assignments. GPS is shared only while a trip is active.
8. **Stop status is GPS + geofence driven.** Upcoming / Approaching / Reached / Crossed are computed automatically, not set by hand.
9. **ETA is from the current bus position to the child's assigned stop.**
10. **Audit log** all important create/update/delete/assignment actions. Super Admin critical actions require confirmation and are recorded.

## Domain model (fields from the documents)

- **School**: profile, contacts, working days, transport config, status.
- **Route**: name, number/code, starting point, school, status, direction/trip type support.
- **Stop**: name, latitude, longitude, sequence, pickup time, drop time, geofence radius.
- **Bus**: vehicle number, capacity, status (active / inactive / maintenance), optional GPS device info.
- **Driver**: profile, mobile number, identity details, status (active / inactive / suspended).
- **Student**: name, student ID/admission number, class, section, status, transport status.
- **Parent**: guardian details, mobile number (OTP identity), linked to one or more students.
- **StudentAssignment**: student → route + pickup stop + drop stop, with history.
- **DailyAssignment / Trip**: date, driver, bus, route, trip type (Morning Pickup / Evening Drop), status lifecycle, history.
- **Notification / Announcement**: title, message, optional attachment, target (Entire School or Specific Route), scheduled or manual, delivery status, history.
- **DelayEvent**: trip, minutes, reason (Traffic, Bus Breakdown, Road Block, Weather, Driver Issue, Other).
- **AuditLog**.

Example route: School → Gandhipuram → Peelamedu → Hope College → Singanallur → School.
Example assignment: 07-Oct-2026, Driver Kumar + Bus TN-38-AB-1234 + Route RS-01 + Morning Pickup.

## End-to-end flow

1. Admin creates routes and adds ordered stops.
2. Admin registers students, assigns route then stop.
3. Admin registers drivers and buses.
4. Admin/Transport Manager creates the daily assignment (driver + bus + route + trip).
5. Driver app: login → see today's trip → confirm → start trip.
6. Driver GPS → Go backend → Redis current location → WebSocket → Parent app + Admin web.
7. GPS + geofence → ETA and Upcoming / Approaching / Reached / Crossed per stop.
8. Trip events → automatic push notifications.
9. Admin sends school-wide or route-specific announcements as needed.
10. Driver ends trip.

## Real-time and tracking

- Background location service on the driver app while a trip is active.
- Backend ingests locations, writes current location to Redis, fans out over authenticated WebSockets.
- Parent and admin maps update without manual refresh. Show location, status, distance, ETA, optional speed (e.g. `Bus: TN-38-AB-1234 | Status: Moving | ETA: 8 minutes | Distance: 2.4 km`).
- Track last-seen timestamp; flag stale/offline buses.
- Handle GPS accuracy issues and temporary signal loss without firing false Reached/Crossed events.
- Live location history kept only where retention is enabled.

## Notifications

Automatic: Bus Started, Bus Approaching, Bus Reached Stop, Bus Crossed Stop, Bus Delayed, School Reached, Trip Completed.

Manual: School Announcement, Route Announcement, Bus Breakdown, Traffic Delay, Pickup Change, Emergency Message, Holiday, Other.

Reference copy:
- "Bus Started from School"
- "Bus is approaching your stop. Estimated arrival: 8 minutes."
- "Bus reached your stop."
- "Bus has crossed your stop."
- "Bus Delayed by 15 Minutes. Please expect a delay in reaching your pickup location."
- "Important Bus Update: Your school bus has experienced a breakdown. Please wait for further instructions from the school."
- "Parents are requested to pick up their children from school."

Targeting: Entire School (all parents across routes) or Specific Route (parents of students on that route). Delay/breakdown notifications go to affected-route parents.

## Parent app: multiple children

After login, list all linked children. Switching child must refresh route, stop, bus, ETA, trip status and notifications for that child.

## Modules (V1)

| # | Module | Platform | Priority |
|---|---|---|---|
| 1 | Authentication & User Management | Web + Apps | Core |
| 2 | School Management | Admin Web | Core |
| 3 | Driver Management | Admin Web | Core |
| 4 | Bus / Vehicle Management | Admin Web | Core |
| 5 | Route Management | Admin Web | Core |
| 6 | Stop Management | Admin Web | Core |
| 7 | Student Management | Admin Web | Core |
| 8 | Parent Management | Admin Web | Core |
| 9 | Student Route & Stop Assignment | Admin Web | Core |
| 10 | Daily Trip Assignment | Admin Web | Core |
| 11 | Driver Mobile App | Android | Core |
| 12 | Parent Mobile App | Android | Core |
| 13 | Live GPS Tracking | Backend + Apps + Web | Core |
| 14 | Real-Time WebSocket | Backend | Core |
| 15 | ETA & Distance | Backend + Apps | Core |
| 16 | Geofence & Stop Detection | Backend | Core |
| 17 | Trip & Journey Management | Web + Apps | Core |
| 18 | Push Notifications | Backend + Apps | Core |
| 19 | School / Route Announcements | Admin Web + Parent | Core |
| 20 | Delay & Breakdown Management | Web + Apps | Core |
| 21 | Dashboard | Admin Web | Core |
| 22 | Reports & History | Admin Web | Core |
| 23 | Audit Logs | Admin Web | Important |
| 24 | Settings & Configuration | Admin Web | Important |
| 25 | Security & Access Control | All | Core |

Dashboard shows: totals (buses, drivers, students, active routes), live buses, trips started/completed, delayed buses, offline/stale buses, route-wise summary, alerts.

Reports: trip, bus journey, driver trip, route performance, delay, assignment, notification and location history.

Security extras: secure WebSocket auth, API rate limiting and abuse protection, configurable data retention and privacy.

## Build order

Follow the documented dependency flow:

1. Foundation: Users → Roles → Schools
2. Transport master: Drivers → Buses → Routes → Stops
3. Student master: Students → Parents → Route → Stop Assignment
4. Operations: Date → Driver + Bus + Route → Trip
5. Tracking: Driver GPS → Go → Redis → WebSocket → Parent/Web
6. Intelligence: Geofence → Stop Event → ETA → Trip Status
7. Communication: Trip Event → Push Notification
8. Administration: Dashboard → Reports → Audit Logs

## Out of scope for V1

Advanced analytics, attendance automation and additional integrations. Do not build these until the core tracking flow is stable. Students have no login in V1.

## Coding conventions

### API

- **Every endpoint change updates `backend/api/openapi.yaml` in the same change** (path, request/response schemas, error codes, an example), and adds a line to its Changelog. `go test ./api` validates the spec. Mobile-facing endpoints are a contract: no breaking changes without telling the mobile team; add fields or a new version instead.
- REST under `/api/v1`, plural nouns, kebab-case paths (`/api/v1/daily-trips`). `/health` sits outside the version prefix.
- JSON fields in `snake_case`. Timestamps are RFC 3339 UTC; calendar dates (trip date) are `YYYY-MM-DD`.
- Success: `{"data": ...}`. Lists: `{"data": [...], "meta": {"page", "page_size", "total"}}`, with `?page=1&page_size=20` (max 100).
- Error: `{"error": {"code": "snake_case_code", "message": "Human text", "fields": {"field": "reason"}}}`. Always use the helpers in `backend/internal/httpx`.
- Status codes: 400 validation, 401 unauthenticated, 403 wrong role/school, 404 not found **or out of tenant scope** (never reveal another school's records exist), 409 conflict, 429 rate limited, 500 unexpected.
- Auth (from Sprint 1): `Authorization: Bearer <access token>`.
- Every request gets an `X-Request-ID`; include it in logs.

### Backend

- `gofmt`/`goimports`, `go vet`, golangci-lint (`backend/.golangci.yml`).
- Handlers stay thin: decode → validate → call service → respond with `httpx`.
- Pass `context.Context` through to every DB/Redis call. Wrap errors with `fmt.Errorf("...: %w", err)`.
- Schema changes only via new goose migrations; never edit an applied one. Every table has `id uuid default gen_random_uuid()`, `created_at`, `updated_at` (with the `set_updated_at()` trigger), and `school_id` when tenant-owned.
- Table-driven tests with `httptest`.

### Frontend

- TypeScript strict; ESLint + Prettier (no semicolons, single quotes, 100 cols).
- All HTTP goes through `src/services/api.ts`; errors arrive as `ApiError` (`status`, `code`, `fields`).
- One file per page in `src/pages`, registered in `src/routes`. Ant Design components; avoid custom CSS unless needed.

## School Bus Tracking Software – Tech Stack
Driver Mobile App – Android + Kotlin
Parent Mobile App – Android + Kotlin
Web Admin Panel – React + TypeScript
Backend API – Go (Golang)
Real-Time Communication – WebSocket
Live Location Cache – Redis
Main Database – PostgreSQL
Push Notifications – Firebase Cloud Messaging (FCM)
Map & GPS – Google Maps API
Background Location – Android Foreground Service
Authentication – JWT
Server – Linux + Nginx
Deployment – Docker
API Format – REST API + WebSocket
Monitoring/Logs – Prometheus + Grafana
Core stack:
Android Kotlin + React + Go + WebSocket + Redis + PostgreSQL + FCM + Google Maps.
