# Sprint Plan: School Bus Tracking System V1

Stack: **Go backend** (`backend/`), **React admin web** (`frontend/`), Redis, WebSocket.

**Scope of this team: the web app and the API.** The Android Driver and Parent apps are built by the **mobile team** against the API we publish. Our job is to deliver every mobile-facing endpoint documented in the OpenAPI spec (served at `/docs`), with examples, on a shared environment the mobile team can reach. `MOB` rows are listed only so dependencies are visible; the mobile team owns and ticks them.
Sprint length: **2 weeks** (Sprint 0 is 1 week). Total: about 19 weeks.

Tags: `BE` = Go backend, `FE` = React admin web, `OPS` = infra/devops, `API` = API handoff to the mobile team (docs + examples + working on the shared environment), `MOB` = mobile team (not ours).
Mark a task done by adding ✅ before its ID once it is merged **and** its "Done when" check passes.

Every backend task must follow the CLAUDE.md non-negotiables: school-scoped queries, backend RBAC, and an audit log entry for create/update/delete/assignment actions.

---

## Sprint 0: Setup & Decisions (1 week)

**Goal:** both projects run locally, and the open decisions are closed.

| ID | Tag | Task | Done when |
|---|---|---|---|
| ✅ S0-01 | — | Decide primary DB (recommended: **PostgreSQL**) | Written in CLAUDE.md |
| ✅ S0-02 | — | Mobile stack: owned by the mobile team; this repo delivers web + API only | Written in CLAUDE.md |
| S0-03 | — | Decide maps (Google Maps / Mapbox / OSM+Leaflet), push (**FCM**), SMS OTP provider (MSG91 / Twilio) | Written in CLAUDE.md |
| ✅ S0-04 | BE | Go module init: `cmd/api/main.go`, config loader (env), structured logging, `/health` endpoint | `go run ./cmd/api` serves `/health` |
| ✅ S0-05 | BE | Router (chi or gin), middleware skeleton (request ID, recover, CORS, logging) | Middleware runs on every request |
| ✅ S0-06 | BE | DB connection + migration tool (golang-migrate or goose) | `migrate up/down` works |
| ✅ S0-07 | BE | Redis connection + health check | `/health` reports DB + Redis |
| ✅ S0-08 | OPS | `docker-compose.yml` for Postgres + Redis | `docker compose up` starts both |
| ✅ S0-09 | FE | React (Vite) setup: router, layout shell, API client (axios), env config, UI library choice | App loads with sidebar layout |
| ✅ S0-10 | OPS | Lint/format (golangci-lint, ESLint/Prettier), `.gitignore`, README with run commands | Lint passes locally |
| S0-11 | OPS | CI pipeline (GitHub Actions): build + lint + test for BE and FE | Green CI on PR |
| ✅ S0-12 | — | API conventions doc: REST paths, error format, pagination, auth header | Added to CLAUDE.md |
| ✅ S0-13 | API | OpenAPI 3.1 spec (`backend/api/openapi.yaml`) + Swagger UI at `/docs`; every new endpoint is added to the spec in the same PR | `/docs` shows `/health` |

---

## Sprint 1: Foundation: Auth, Roles, Schools (Modules 1, 2, 25 basics)

**Goal:** users can log in with the correct role, and every request is school-scoped.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S1-01 | BE | Schema: `schools`, `users`, `roles`, `user_sessions`, `audit_logs` | Migration applied |
| S1-02 | BE | Password login for web users (bcrypt) + JWT access/refresh tokens, expiry, logout | Login returns tokens; logout revokes |
| S1-03 | BE | OTP login API (send/verify) for Driver & Parent, with OTP rate limit + expiry | OTP verified against stub/provider |
| S1-04 | BE | Auth middleware: parse token, load user, role, `school_id` into context | Protected route rejects missing/invalid token |
| S1-05 | BE | RBAC middleware: `RequireRole(...)` + tenant scope helper (`school_id` forced on every query) | Test: School Admin A cannot read School B data |
| S1-06 | BE | Account suspension + session/device list + revoke | Suspended user cannot log in |
| S1-07 | BE | Audit log helper `audit.Record(ctx, action, entity, before, after)` | Entries written on create/update/delete |
| S1-08 | BE | Schools CRUD (Super Admin only): profile, contacts, working days, transport config, status | API + tests |
| S1-09 | BE | School Admin & Transport Manager user CRUD (Super Admin / School Admin) | API + tests |
| S1-10 | BE | Seed script: one Super Admin, a demo school, a demo admin | `make seed` works |
| S1-11 | FE | Login page, token storage, refresh handling, logout | Login → dashboard shell |
| S1-12 | FE | Auth context + role-based route guards + role-based menu | Each role sees only its menu items |
| S1-13 | FE | Schools list / create / edit / activate-deactivate (Super Admin) | Full CRUD from UI |
| S1-14 | FE | School users list / create / edit / suspend | Full CRUD from UI |
| S1-15 | BE | 2FA (TOTP) for Super Admin (optional for others) | Super Admin must pass 2FA |
| S1-16 | API | **Handoff 1, Auth:** OTP send/verify, refresh, logout, `GET /me`, error codes; dev OTP stub documented so the mobile team can log in without SMS | Mobile team logs in against our API |

---

## Sprint 2: Transport Master: Drivers, Buses, Routes, Stops (Modules 3–6)

**Goal:** an admin can build a full route with ordered stops on a map.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S2-01 | BE | Schema: `drivers`, `buses`, `routes`, `stops` (lat, lng, sequence, pickup_time, drop_time, geofence_radius) | Migration applied |
| S2-02 | BE | Drivers CRUD: profile, mobile, identity details, status (active/inactive/suspended); links to a Driver user for OTP login | API + tests |
| S2-03 | BE | Buses CRUD: vehicle number (unique per school), capacity, status (active/inactive/maintenance), GPS device info | API + tests |
| S2-04 | BE | Routes CRUD: name, code, starting point, trip type support, status | API + tests |
| S2-05 | BE | Stops CRUD under a route + **reorder endpoint** (sequence kept consistent in a transaction) | Reorder keeps sequence 1..N with no gaps |
| S2-06 | BE | Validation: lat/lng range, geofence radius min/max, unique stop sequence | Invalid input → 400 with field errors |
| S2-07 | FE | Shared table component: search, filter, pagination, status badge | Reused on all master pages |
| S2-08 | FE | Drivers pages (list / form / status change) | CRUD from UI |
| S2-09 | FE | Buses pages | CRUD from UI |
| S2-10 | FE | Routes pages | CRUD from UI |
| S2-11 | FE | Stop management: map picker to set lat/lng, drag-to-reorder list, geofence circle on map | Stops visible in order on map |
| S2-12 | BE+FE | Permissions: Transport Manager has full access here, Driver/Parent get none via web | RBAC tests pass |
| S2-13 | OPS | Shared dev API environment reachable by the mobile team (HTTPS URL, seeded demo school, test driver + parent accounts) | Mobile team calls `/health` from a device |

---

## Sprint 3: Student Master: Students, Parents, Assignment (Modules 7–9)

**Goal:** each student is linked to parents and to one route + stop.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S3-01 | BE | Schema: `students`, `parents`, `parent_students` (many-to-many), `student_assignments` (route, pickup stop, drop stop, effective dates) | Migration applied |
| S3-02 | BE | Students CRUD: name, admission no (unique per school), class, section, status, transport status | API + tests |
| S3-03 | BE | Parents CRUD: guardian details, mobile (unique, OTP identity), link/unlink students | API + tests |
| S3-04 | BE | Student assignment API: validate that the stop belongs to the route; keep history on change | Wrong route/stop pair → 400 |
| S3-05 | BE | Transport Manager sees a **limited** student view (name, class, stop only) | Response excludes non-transport fields |
| S3-06 | BE | Bulk import students + parents (CSV) with row-level error report | 500-row CSV imports with error list |
| S3-07 | FE | Students pages + parent linking UI | CRUD from UI |
| S3-08 | FE | Parents pages | CRUD from UI |
| S3-09 | FE | Assignment UI: pick route → dropdown shows **only that route's stops** | Matches rule 5 |
| S3-10 | FE | Route view: list of students per stop | Visible per route |
| S3-11 | FE | CSV import screen with downloadable template | Works end to end |
| S3-12 | API | **Handoff 2, Master data reads:** driver profile, bus, route + ordered stops; parent → linked children → route/stop | Endpoints in `/docs` with examples |

---

## Sprint 4: Daily Trip Assignment + Driver APIs (Modules 10, 11, 17)

**Goal:** an admin assigns today's trips, and the driver logs in, sees the trip, starts it and ends it.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S4-01 | BE | Schema: `trips` (date, driver, bus, route, trip_type MORNING_PICKUP/EVENING_DROP, status, started_at, ended_at), `trip_status_history` | Migration applied |
| S4-02 | BE | Create daily assignment: block conflicts (same driver or bus twice on the same date + trip type; inactive bus/driver) | Conflict → 409 |
| S4-03 | BE | Copy assignments from previous day / week (bulk) | One click copies a day |
| S4-04 | BE | Trip lifecycle state machine: Scheduled → Confirmed → Started → Completed / Cancelled, plus admin override | Invalid transition → 400 |
| S4-05 | BE | Driver APIs: today's trips (own only), confirm, start, end | Driver cannot see others' trips |
| S4-06 | FE | Daily assignment board: date picker, morning/evening tabs, create/edit/cancel | Full flow from UI |
| S4-07 | FE | Trip list + trip detail with status history; admin override action | Visible + override audited |
| S4-08 | MOB | Driver app setup (project, navigation, API client, secure token storage) | Builds on device |
| S4-09 | MOB | Driver OTP login | Logs in with real OTP / stub |
| S4-10 | MOB | Today's trips screen → trip detail (route, ordered stops, bus) → Confirm / Start / End | Status updates in admin web |
| S4-11 | API | **Handoff 3, Driver trips:** today's trips, trip detail, confirm/start/end, status values and allowed transitions | Mobile team completes a trip on dev |

---

## Sprint 5: Live GPS + WebSocket (Modules 13, 14)

**Goal:** an admin watches a moving bus on a map in real time.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S5-01 | BE | Location ingest API `POST /trips/{id}/locations` (batch points: lat, lng, accuracy, speed, heading, ts); only allowed while the trip is Started and by its driver | Others → 403 |
| S5-02 | BE | Filter bad points (low accuracy, impossible jumps, old timestamps) | Unit tests with noisy data |
| S5-03 | BE | Write current location to Redis (`bus:{trip_id}:loc`) + last_seen; publish via Redis pub/sub | Redis value updates |
| S5-04 | BE | Location history table (only when retention is enabled) + retention cleanup job | Old rows purged per setting |
| S5-05 | BE | WebSocket server: token auth on connect, subscribe to a trip/school channel with **role + school + ownership check** | Parent cannot subscribe to another route |
| S5-06 | BE | Fan-out hub (Redis pub/sub → WS clients), heartbeats, reconnect support | Survives client reconnect |
| S5-07 | BE | Stale/offline detection job (no point in N seconds → OFFLINE event) | Bus marked stale on admin map |
| S5-08 | MOB | Driver background location service (foreground notification), runs only while a trip is Started; offline queue + batch upload | Points arrive with screen off |
| S5-09 | FE | Live map page: all active buses, click a bus for trip info, auto-update via WS | Moves without refresh |
| S5-10 | OPS | GPS simulator script (replays a route path) for testing without a phone | `go run ./tools/simulate` moves a bus |
| S5-11 | API | **Handoff 4, Live tracking:** location batch upload contract (fields, batching, offline replay rules), WebSocket protocol doc (auth, subscribe, message types, heartbeat, reconnect), GPS simulator for their testing | Mobile team uploads points and sees them on admin map |

---

## Sprint 6: Geofence, ETA, Parent APIs (Modules 12, 15, 16)

**Goal:** a parent sees their child's bus, ETA and stop status live.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S6-01 | BE | Geofence engine: Upcoming / Approaching / Reached / Crossed per stop, with hysteresis (needs N consecutive points; ignores low accuracy) | Simulator test: no false Reached on GPS jitter |
| S6-02 | BE | Stop events table + publish stop events over WS | Events stored + pushed |
| S6-03 | BE | ETA + distance: bus position → child's stop (along remaining stops; maps API or haversine × speed fallback) | ETA shown in minutes + km |
| S6-04 | BE | Parent APIs: my children, child's route/stop/today's trips, current trip state (scoped to linked children only) | Cannot access unlinked student → 403 |
| S6-05 | MOB | Parent app setup + OTP login | Logs in |
| S6-06 | MOB | Children list + **switch child** (refreshes route, stop, bus, ETA, status, notifications) | Switch reloads all data |
| S6-07 | MOB | Live tracking screen: map, bus marker, my stop, status line `Bus: X \| Status \| ETA \| Distance`, Morning/Evening clearly labelled | Live update via WS |
| S6-08 | FE | Admin trip detail: stop-by-stop status timeline live | Updates live |
| S6-09 | API | **Handoff 5, Parent tracking:** children list, child trip state, ETA/distance, stop status messages over WebSocket | Parent app shows live ETA on dev |

---

## Sprint 7: Notifications, Announcements, Delays (Modules 18, 19, 20)

**Goal:** parents get automatic and manual notifications.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S7-01 | BE | Device token registration API (parent + driver) | Token stored per device |
| S7-02 | BE | Push service (FCM) + notification queue/worker with retry + delivery status | Delivery status recorded |
| S7-03 | BE | Auto notifications from trip/stop events: Started, Approaching (with ETA), Reached, Crossed, Delayed, School Reached, Completed; no duplicates per stop per trip | Each fires once |
| S7-04 | BE | Announcements: title, message, attachment, target (Entire School / Specific Route), manual or scheduled; history | Route target reaches only that route's parents |
| S7-05 | BE | Delay & breakdown: driver reports (minutes + reason), admin reports; notify affected-route parents | Parents of route get "Bus Delayed by 15 Minutes…" |
| S7-06 | BE | Emergency report from driver → alert to admins in real time | Admin sees alert instantly |
| S7-07 | MOB | Driver: report delay / breakdown / emergency screens | Works end to end |
| S7-08 | MOB | Parent: push handling, notification inbox, per-child filter | Notifications listed |
| S7-09 | FE | Announcement composer + scheduled list + history with delivery stats | Full flow |
| S7-10 | FE | Alerts panel (delays, breakdowns, emergencies) on live map | Live alerts |
| S7-11 | API | **Handoff 6, Notifications:** device token API, FCM payload format (type, data keys, child/trip IDs for deep links), notification inbox API, delay/breakdown/emergency report APIs | Push received on test device |

---

## Sprint 8: Dashboard, Reports, Audit Logs, Settings (Modules 21–24)

**Goal:** admins have an overview, history and accountability.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S8-01 | BE | Dashboard API: totals (buses, drivers, students, active routes), live buses, trips started/completed, delayed, offline, route-wise summary | Single endpoint, fast |
| S8-02 | FE | Dashboard page with cards + route summary + alerts | Matches module 21 |
| S8-03 | BE | Reports: trip, bus journey, driver trip, route performance, delay, assignment, notification, location history; filters + CSV export | Each report exports CSV |
| S8-04 | FE | Reports pages with filters, table, export | All 8 reports |
| S8-05 | FE | Trip replay (location history on map) | Replays a completed trip |
| S8-06 | BE+FE | Audit log viewer: Super = all, School = own school, Transport = transport actions | Scope tests pass |
| S8-07 | BE+FE | Settings: global (Super Admin), school-level limited settings (geofence default, approaching distance, retention days, stale timeout) | Settings take effect |
| S8-08 | BE | Super Admin critical actions require confirmation (e.g., delete school) | Confirm step + audited |

---

## Sprint 9: Hardening, Testing, Release

**Goal:** V1 is production-ready.

| ID | Tag | Task | Done when |
|---|---|---|---|
| S9-01 | BE | API rate limiting (per IP + per user), OTP abuse protection | Limits enforced |
| S9-02 | BE | Tenant isolation test suite: every endpoint × every role × cross-school | All pass |
| S9-03 | BE | Load test: N buses × 1 point every 5 s + M WS clients | Meets target latency |
| S9-04 | ALL | Security review: input validation, secrets, HTTPS/WSS, CORS, token storage | Issues fixed |
| S9-05 | MOB | Battery/permission handling, Android background location policies, Play Store prep | Approved build |
| S9-06 | OPS | Staging + production deploy, backups, monitoring/alerting, logs | Deployed |
| S9-07 | ALL | UAT with one pilot school: full flow Route → … → Trip end | Sign-off |
| S9-08 | — | User docs: admin guide, driver guide, parent FAQ | Shared |

---

## Mobile team timing

The mobile team builds each feature in the sprint **after** its handoff (e.g. Handoff 3 in Sprint 4 → driver trip screens in Sprint 5). Agree the endpoint shapes with them at the start of the sprint, and share the `/docs` link plus a changelog of API changes at the end of every sprint. Never make breaking changes to a handed-off endpoint without telling them; add a new version or field instead.

---

## Milestones

| After sprint | Demo |
|---|---|
| 1 | Login by role, schools managed |
| 3 | Full master data: routes, stops, students, parents assigned |
| 4 | Daily trip assigned → driver APIs ready (mobile team: start/end trip in app) |
| 5 | **Bus moving live on admin map** |
| 6 | **Parent APIs + live ETA ready** (with mobile team: core product works) |
| 7 | Notifications live |
| 9 | V1 release |
