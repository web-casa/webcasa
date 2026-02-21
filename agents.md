# AGENTS.md — AI Developer Context

> This file provides context for AI coding assistants working on this codebase.

## Project Overview

CaddyPanel is a web-based management panel for the Caddy reverse proxy server, similar to Nginx Proxy Manager. It generates Caddyfile configurations from a SQLite database and manages the Caddy process lifecycle.

## Architecture

- **Pattern**: Caddyfile generation + `caddy reload` (not Admin API)
- **Backend**: Go + Gin + GORM + SQLite
- **Frontend**: React 19 + Vite 6 + Radix UI Themes + Tailwind CSS v4 + Zustand
- **Distribution**: Single binary (frontend embedded at build time) or Docker

## Key Design Decisions

1. **Caddyfile over Admin API** — Human-readable config, file-based persistence, simpler rollback (backup before write), `caddy reload` is graceful (zero-downtime)
2. **Atomic writes** — Write temp → validate → backup → rename. Never partial writes.
3. **Caddy binary optional** — The panel works without Caddy installed (skips validation, shows config preview). Useful for UI development.
4. **Single service user** — `caddypanel` system user runs both the panel and controls Caddy via CLI commands.

## Directory Map

```
main.go                          → Entry point, route registration, SPA serving
internal/config/config.go        → Env var config loading (CADDYPANEL_* vars)
internal/model/model.go          → All GORM models + request DTOs
internal/database/database.go    → SQLite init, auto-migrate, WAL mode
internal/auth/auth.go            → JWT generate/parse/middleware, bcrypt helpers
internal/caddy/renderer.go       → Host[] → Caddyfile text (strings.Builder)
internal/caddy/manager.go        → Caddy process: start/stop/reload, atomic file write
internal/service/host.go         → Business logic: CRUD + ApplyConfig + import/export
internal/handler/auth.go         → /api/auth/* endpoints
internal/handler/host.go         → /api/hosts/* CRUD endpoints
internal/handler/caddy.go        → /api/caddy/* process control
internal/handler/log.go          → /api/logs log viewing + download
internal/handler/export.go       → /api/config/export|import
web/src/main.jsx                 → React entry, Radix Theme wrapper
web/src/App.jsx                  → React Router setup, auth guard
web/src/api/index.js             → Axios client, JWT interceptors
web/src/stores/auth.js           → Zustand auth state (login/logout/token)
web/src/pages/Login.jsx          → Login + first-time setup page
web/src/pages/Layout.jsx         → Sidebar nav layout
web/src/pages/Dashboard.jsx      → Stats cards overview
web/src/pages/HostList.jsx       → Host table + create/edit dialog + delete confirm
web/src/pages/Logs.jsx           → Log viewer with search/filter
web/src/pages/Settings.jsx       → Caddy control, Caddyfile preview, import/export
install.sh                       → One-click install for major Linux distros
```

## Data Flow: Creating a Host

```
POST /api/hosts → handler.Create → service.Create
  → GORM insert into hosts + upstreams tables
  → service.ApplyConfig()
    → service.List() (reload all hosts from DB)
    → caddy.RenderCaddyfile(hosts, config)
    → manager.WriteCaddyfile(content)
      → write to .tmp file
      → exec: caddy validate (if binary exists)
      → backup old file to backups/
      → os.Rename (atomic)
    → manager.Reload() (if caddy is running)
      → exec: caddy reload --config <path>
```

## Database Schema (SQLite, GORM auto-migrated)

- `users` — id, username, password (bcrypt), timestamps
- `hosts` — id, domain (unique), enabled, tls_enabled, http_redirect, websocket, timestamps
- `upstreams` — id, host_id (FK CASCADE), address, weight, sort_order
- `routes` — id, host_id (FK CASCADE), path, upstream_id, sort_order
- `custom_headers` — id, host_id (FK CASCADE), direction, operation, name, value, sort_order
- `access_rules` — id, host_id (FK CASCADE), rule_type (allow/deny), ip_range (CIDR), sort_order

All child tables cascade on delete with the parent host.

## API Authentication

- Public endpoints: `POST /api/auth/setup`, `POST /api/auth/login`, `GET /api/auth/need-setup`
- All other endpoints require `Authorization: Bearer <jwt>` header
- JWT: HS256, 24h expiry, signed with `CADDYPANEL_JWT_SECRET` env var
- Auth middleware in `internal/auth/auth.go` sets `user_id` and `username` in Gin context

## Environment Variables

All prefixed with `CADDYPANEL_`:
- `PORT` (default: 8080)
- `DATA_DIR` (default: ./data)
- `DB_PATH` (default: data/caddypanel.db)
- `JWT_SECRET` (default: insecure dev value — MUST change in production)
- `CADDY_BIN` (default: "caddy" from PATH)
- `CADDYFILE_PATH` (default: data/Caddyfile)
- `LOG_DIR` (default: data/logs)
- `ADMIN_API` (default: http://localhost:2019)

## Common Tasks

### Adding a new API endpoint
1. Add handler method in `internal/handler/`
2. Register route in `main.go` (protected or public group)
3. Add API function in `web/src/api/index.js`
4. Use in React component

### Adding a new Host feature (e.g., rate limiting)
1. Add field to `model.Host` struct + optional child model
2. GORM auto-migrates on restart
3. Update `caddy.RenderCaddyfile()` in `renderer.go`
4. Update `service.Create/Update` to handle the new field
5. Update `HostCreateRequest` DTO
6. Update frontend form in `HostList.jsx`

### Adding a new page
1. Create `web/src/pages/NewPage.jsx`
2. Add route in `web/src/App.jsx`
3. Add sidebar link in `web/src/pages/Layout.jsx` (navItems array)

## Testing

```bash
# Backend
go test ./... -v

# Frontend build check
cd web && npm run build

# Manual API test
curl -X POST http://localhost:8080/api/auth/setup \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'
```

## Gotchas

- **SQLite CGO**: `CGO_ENABLED=1` is required for the SQLite driver (mattn/go-sqlite3)
- **Caddy not found**: The panel gracefully handles missing Caddy binary — it skips validation but still writes the Caddyfile
- **CORS**: Uses `AllowAllOrigins: true` because the Go server serves both API and frontend
- **Frontend embed**: In production, `web/dist/` is served by the Go binary. In dev, Vite runs on :5173 and proxies `/api` to :8080
- **Host domain uniqueness**: Domain field has a unique index; duplicates are rejected at service layer
