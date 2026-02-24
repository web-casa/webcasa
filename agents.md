# AGENTS.md — AI Developer Context

> This file provides context for AI coding assistants working on this codebase.

## Project Overview

WebCasa is a web-based management panel for the Caddy reverse proxy server, similar to Nginx Proxy Manager. It generates Caddyfile configurations from a SQLite database and manages the Caddy process lifecycle.

## Architecture

- **Pattern**: Caddyfile generation + `caddy reload` (not Admin API)
- **Backend**: Go 1.26 + Gin + GORM + SQLite
- **Frontend**: React 19 + Vite 7 + Radix UI Themes + Zustand + CodeMirror 6
- **Distribution**: Single binary (frontend embedded at build time) or Docker

## Key Design Decisions

1. **Caddyfile over Admin API** — Human-readable config, file-based persistence, simpler rollback (backup before write), `caddy reload` is graceful (zero-downtime)
2. **Atomic writes** — Write temp → validate → backup → rename. Never partial writes.
3. **Caddy binary optional** — The panel works without Caddy installed (skips validation, shows config preview). Useful for UI development.
4. **Single service user** — `webcasa` system user runs both the panel and controls Caddy via CLI commands.
5. **Multi-user with roles** — Users have `admin` or `viewer` roles. All mutations are audit-logged.

## Directory Map

```
main.go                          → Entry point, route registration, SPA serving
internal/config/config.go        → Env var config loading (WEBCASA_* vars)
internal/model/model.go          → All GORM models + request DTOs
internal/database/database.go    → SQLite init, auto-migrate, WAL mode
internal/auth/auth.go            → JWT generate/parse/middleware, bcrypt helpers
internal/caddy/renderer.go       → Host[] → Caddyfile text (strings.Builder)
internal/caddy/manager.go        → Caddy process: start/stop/reload, format/validate, atomic file write
internal/service/host.go         → Business logic: CRUD + ApplyConfig + import/export
internal/handler/auth.go         → /api/auth/* endpoints
internal/handler/host.go         → /api/hosts/* CRUD endpoints (with audit logging)
internal/handler/caddy.go        → /api/caddy/* process control + Caddyfile editor API
internal/handler/log.go          → /api/logs log viewing + download
internal/handler/cert.go         → /api/hosts/:id/cert SSL cert upload/delete
internal/handler/export.go       → /api/config/export|import
internal/handler/user.go         → /api/users/* multi-user CRUD
internal/handler/audit.go        → /api/audit/logs audit log viewer
internal/handler/dashboard.go    → /api/dashboard/stats aggregated stats
internal/handler/dns_provider.go → /api/dns-providers/* DNS API provider CRUD
web/src/main.jsx                 → React entry, Radix Theme wrapper
web/src/App.jsx                  → React Router setup, auth guard
web/src/api/index.js             → Axios client, JWT interceptors
web/src/stores/auth.js           → Zustand auth state (login/logout/token)
web/src/pages/Login.jsx          → Login + first-time setup page
web/src/pages/Layout.jsx         → Sidebar nav layout
web/src/pages/Dashboard.jsx      → Enhanced stats cards (host/TLS/system info)
web/src/pages/HostList.jsx       → Host table + create/edit dialog (4 types, tabbed form)
web/src/pages/Logs.jsx           → Log viewer with search/filter
web/src/pages/Settings.jsx       → Caddy control, Caddyfile preview, import/export
web/src/pages/Users.jsx          → Multi-user management (CRUD + roles)
web/src/pages/AuditLogs.jsx      → Audit log viewer with pagination
web/src/pages/CaddyfileEditor.jsx→ CodeMirror 6 editor with format/validate/save
install.sh                       → One-click install for major Linux distros
```

## Data Flow: Creating a Host

```
POST /api/hosts → handler.Create → service.Create
  → GORM insert into hosts + upstreams tables
  → audit log entry via WriteAuditLog()
  → service.ApplyConfig()
    → service.List() (reload all hosts from DB)
    → caddy.RenderCaddyfile(hosts, config, dnsProviders)
    → manager.WriteCaddyfile(content)
      → write to .tmp file
      → exec: caddy validate (if binary exists)
      → backup old file to backups/
      → os.Rename (atomic)
    → manager.Reload() (if caddy is running)
      → exec: caddy reload --config <path>
```

## Database Schema (SQLite, GORM auto-migrated)

- `users` — id, username, password (bcrypt), role (admin/viewer), timestamps
- `dns_providers` — id, name, provider (cloudflare/alidns/tencentcloud/route53), config (JSON), is_default, timestamps
- `hosts` — id, domain (unique), host_type (proxy/redirect/static/php), enabled, tls_enabled, tls_mode (auto/dns/wildcard/custom/off), dns_provider_id (FK), http_redirect, websocket, redirect_url, redirect_code, custom_cert_path, custom_key_path, compression, cache_enabled, cache_ttl, cors_enabled, cors_origins, cors_methods, cors_headers, security_headers, error_page_path, custom_directives, root_path, directory_browse, php_fastcgi, index_files, timestamps
- `upstreams` — id, host_id (FK CASCADE), address, weight, sort_order
- `routes` — id, host_id (FK CASCADE), path, upstream_id, sort_order
- `custom_headers` — id, host_id (FK CASCADE), direction, operation, name, value, sort_order
- `access_rules` — id, host_id (FK CASCADE), rule_type (allow/deny), ip_range (CIDR), sort_order
- `basic_auths` — id, host_id (FK CASCADE), username, password_hash (bcrypt)
- `audit_logs` — id, user_id, username, action, target, target_id, detail, ip, created_at

All child tables cascade on delete with the parent host.

## Host Types

| Type | Description | Key Fields |
|------|-------------|------------|
| `proxy` (default) | Reverse proxy to upstream servers | `upstreams[]` |
| `redirect` | 301/302 redirect to target URL | `redirect_url`, `redirect_code` |
| `static` | Static file hosting | `root_path`, `directory_browse`, `index_files` |
| `php` | PHP site via FastCGI | `root_path`, `php_fastcgi`, `index_files` |

The renderer dispatches to `renderProxyHost()`, `renderRedirect()`, `renderStaticHost()`, or `renderPHPHost()`.

## Per-Host Options (Batch 2)

| Feature | Model Field | Caddyfile Output |
|---------|-------------|-----------------|
| 响应压缩 | `compression` | `encode gzip zstd` |
| CORS 跨域 | `cors_enabled/origins/methods/headers` | Preflight + response headers |
| 安全响应头 | `security_headers` | HSTS, X-Frame-Options, CSP, etc |
| 自定义错误页 | `error_page_path` | `handle_errors { ... }` |
| 自定义指令 | `custom_directives` | Raw Caddy config |

## TLS Modes

| Mode | Description |
|------|-------------|
| `auto` (default) | Let's Encrypt HTTP challenge |
| `dns` | DNS Challenge via DNS provider |
| `wildcard` | Wildcard cert via DNS Challenge |
| `custom` | User-uploaded cert/key |
| `off` | No TLS (HTTP only) |

## API Authentication

- Public endpoints: `POST /api/auth/setup`, `POST /api/auth/login`, `GET /api/auth/need-setup`
- All other endpoints require `Authorization: Bearer <jwt>` header
- JWT: HS256, 24h expiry, signed with `WEBCASA_JWT_SECRET` env var
- Auth middleware in `internal/auth/auth.go` sets `user_id` and `username` in Gin context

## Environment Variables

All prefixed with `WEBCASA_`:
- `PORT` (default: 39921)
- `DATA_DIR` (default: ./data)
- `DB_PATH` (default: data/webcasa.db)
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
1. Add field to `model.Host` struct + `HostCreateRequest` DTO
2. GORM auto-migrates on restart
3. Update `caddy.RenderCaddyfile()` in `renderer.go`
4. Update `service.Create/Update` to handle the new field
5. Update frontend form in `HostList.jsx`

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
curl -X POST http://localhost:39921/api/auth/setup \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'
```

## Gotchas

- **SQLite CGO**: `CGO_ENABLED=1` is required for the SQLite driver (mattn/go-sqlite3)
- **Caddy not found**: The panel gracefully handles missing Caddy binary — it skips validation but still writes the Caddyfile
- **CORS**: Uses `AllowAllOrigins: true` because the Go server serves both API and frontend
- **Frontend embed**: In production, `web/dist/` is served by the Go binary. In dev, Vite runs on :5173 and proxies `/api` to :39921
- **Host domain uniqueness**: Domain field has a unique index; duplicates are rejected at service layer
- **Bool pointer fields**: `Enabled`, `TLSEnabled`, etc. are `*bool` to distinguish nil (use GORM default) from explicit false
- **BasicAuth passwords**: Stored as bcrypt hashes; never pre-filled in edit forms, only replaced when new values are submitted
- **Custom certs**: Stored in `DATA_DIR/certs/<domain>/cert.pem|key.pem`, key file permissions set to 0600
- **DNS Provider secrets**: Config stored as JSON in DB, masked to `***` in list API response
- **Audit logging**: All host/caddy/user mutations emit audit log entries via `WriteAuditLog()` helper
