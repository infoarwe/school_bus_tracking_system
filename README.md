# School Bus Tracking System

Schools create routes with ordered stops, assign students to a route and stop, assign a driver + bus + route per day, and parents watch the bus live with ETA and stop notifications.

- `backend/`: Go API
- `frontend/`: React admin web (Vite + TypeScript + Ant Design)
- `docs/SPRINT_PLAN.md`: sprint and task plan
- `CLAUDE.md`: product rules, roles and conventions

## Prerequisites

Go 1.25+, Node 22+, Docker Desktop.

## Run locally

```sh
# 1. Postgres (host port 5433) + Redis (6379)
docker compose up -d

# 2. Backend: http://localhost:8085
cd backend
cp .env.example .env
go run ./cmd/migrate up
go run ./cmd/api

# 3. Frontend: http://localhost:5180 (proxies /api and /health to the backend)
cd frontend
npm install
npm run dev
```

API docs (for the mobile team): http://localhost:8085/docs

Check: `curl http://localhost:8085/health` returns `{"data":{"status":"ok","checks":{"postgres":"up","redis":"up"}}}`.

## Common commands

| Task | Backend (`backend/`) | Frontend (`frontend/`) |
|---|---|---|
| Run | `go run ./cmd/api` | `npm run dev` |
| Test | `go test ./...` | — |
| Lint | `go vet ./...` (+ `golangci-lint run` if installed) | `npm run lint` |
| Format | `gofmt -w .` | `npm run format` |
| Build | `go build ./...` | `npm run build` |
| Migrate | `go run ./cmd/migrate up` / `down` / `status` | — |

New migration: add `backend/internal/database/migrations/NNNNN_name.sql` with `-- +goose Up` and `-- +goose Down` sections.
