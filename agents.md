# AGENTS.md — AI Developer Context

> This file provides context for AI coding assistants working on this codebase.

## Project Overview

WebCasa (https://web.casa) is an AI-First lightweight server control panel built on Caddy. It starts as a reverse proxy management panel (Lite) and extends to a full server management platform (Pro) through a compile-time plugin system.

**Current version**: 0.9.x (see `VERSION` file)

## Architecture

- **Pattern**: Plugin-based architecture with compile-time registration (not `.so`)
- **Backend**: Go 1.26+ / Gin / GORM / SQLite (WAL mode)
- **Frontend**: React 19 / Vite 7 / Radix UI Themes / Tailwind CSS / Zustand / react-i18next
- **Distribution**: Single binary + `web/dist/` static files, or Docker
- **Core**: Caddy reverse proxy management via Caddyfile generation + `caddy reload`

## Key Design Decisions

1. **Caddyfile over Admin API** — Human-readable config, file-based persistence, atomic writes (temp → validate → backup → rename), `caddy reload` is graceful (zero-downtime)
2. **Plugin system** — Go Interface + compile-time registration. Plugins get: API routes, DB tables (prefixed `plugin_{id}_*`), data directory, EventBus, ConfigStore, CoreAPI access
3. **Two-tier auth** — `protected` routes (any logged-in user) vs `adminOnly` routes (admin role required). Plugins get both router groups
4. **i18n** — react-i18next with `en.json` / `zh.json`. Plugin names/descriptions have i18n keys in `plugins.names.*` / `plugins.descriptions.*`
5. **Security** — ALTCHA PoW on setup/login, bcrypt passwords, AES-GCM encrypted secrets (TOTP, deploy keys, API keys), constant-time token comparison

## Directory Map

```
main.go                          -> Entry point, route registration, plugin init, SPA serving
VERSION                          -> Version number (single source of truth)
internal/
  config/config.go               -> Env var config loading (WEBCASA_* vars)
  model/model.go                 -> Core GORM models (Host, Upstream, Route, User, etc.)
  database/database.go           -> SQLite init, auto-migrate, WAL mode
  auth/                          -> JWT, bcrypt, TOTP, ALTCHA PoW, rate limiter
  crypto/crypto.go               -> AES-GCM encrypt/decrypt for secrets
  caddy/
    renderer.go                  -> Host[] -> Caddyfile text (all host types + TLS + DNS)
    manager.go                   -> Caddy process: start/stop/reload, atomic file write
    validate.go                  -> Domain, upstream, IP, Caddyfile value, custom directives validation
  service/
    host.go                      -> Host CRUD, ApplyConfig (render + write + reload), Clone, Import/Export
    template.go                  -> Host templates (presets + custom + import/export)
    totp.go                      -> 2FA (TOTP + recovery codes)
  handler/                       -> HTTP handlers for core routes (auth, host, caddy, user, etc.)
  plugin/
    types.go                     -> Plugin interface, CoreAPI interface, Context, FrontendManifest
    manager.go                   -> Plugin lifecycle: init, start, stop, enable/disable, dependency sort
    eventbus.go                  -> In-memory pub/sub with wildcard matching
    configstore.go               -> Per-plugin key-value config (scoped DB prefix)
    coreapi.go                   -> CoreAPI implementation (150+ methods for cross-plugin access)
  notify/                        -> Notification system (Webhook, Email, Discord, Telegram)
plugins/
  deploy/                        -> Source code deployment (Git clone, build, process management)
  docker/                        -> Docker & Compose management (stacks, containers, images, daemon config)
  ai/                            -> AI assistant (chat, tool use 67+ tools, memory, code review, inspection)
  database/                      -> Database instances (MySQL, PostgreSQL, MariaDB, Redis via Docker)
  filemanager/                   -> File browser, editor, archive, web terminal (PTY)
  backup/                        -> Backup/restore via Kopia (local, S3, WebDAV, SFTP)
  monitoring/                    -> System metrics, history charts, threshold alerts
  appstore/                      -> One-click Docker app install, project template marketplace
  mcpserver/                     -> MCP (Model Context Protocol) server for AI IDE integration
  firewall/                      -> firewalld rule management
  cronjob/                       -> Scheduled task management (robfig/cron)
  php/                           -> PHP-FPM and FrankenPHP runtime management
web/
  src/pages/                     -> React page components (30+ pages)
  src/locales/                   -> i18n translation files (en.json, zh.json)
  src/stores/                    -> Zustand stores (auth, pluginNav)
  src/api/index.js               -> Axios API client with all endpoint definitions
```

## Plugin System

### Plugin Interface

```go
type Plugin interface {
    Metadata() Metadata    // ID, name, version, dependencies, priority
    Init(ctx *Context) error  // DB migrations, route registration, setup
    Start() error          // Background tasks
    Stop() error           // Cleanup
}
```

### Plugin Context

Each plugin receives:
- `DB` — shared GORM connection (use `plugin_{id}_*` table prefix)
- `Router` / `AdminRouter` — Gin route groups at `/api/plugins/{id}/`
- `EventBus` — publish/subscribe events
- `ConfigStore` — scoped key-value settings
- `CoreAPI` — 150+ methods to access core functionality and other plugins
- `DataDir` — `data/plugins/{id}/` for files
- `Logger` — structured logger with plugin ID prefix

### CoreAPI Highlights

- Host management: `CreateHost`, `DeleteHost`, `UpdateHost`, `UpdateHostUpstream`
- Settings: `GetSetting`, `SetSetting`
- Deploy: `CreateProject`, `TriggerBuild`, `StartProject`, `StopProject`
- Docker: `DockerPS`, `DockerLogs`, `DockerManageContainer`, `DockerRunContainer`
- Database: `DatabaseCreateInstance`, `DatabaseCreateDatabase`, `DatabaseExecuteQuery`
- File ops: `FileWrite`, `FileDelete`, `FileRename`
- Secrets: `EncryptSecret`, `DecryptSecret`
- And more (monitoring, backup, firewall, cron, notifications)

## Data Flow: Creating a Host

```
POST /api/hosts -> handler.Create -> service.Create
  -> Validate domain, upstreams, IP rules, headers, custom directives, basicauth usernames
  -> GORM insert into hosts + upstreams + headers + rules + basic_auths
  -> Audit log entry
  -> service.ApplyConfig()
    -> List all hosts from DB
    -> caddy.RenderCaddyfile(hosts, config, dnsProviders)
    -> manager.WriteCaddyfile(content)  // atomic: temp -> validate -> backup -> rename
    -> manager.Reload()                 // if fails, rollback to previous Caddyfile
```

## Database Schema

### Core Tables
- `users` — id, username, password (bcrypt), role (admin/viewer), totp_secret, totp_enabled
- `hosts` — domain (unique), host_type (proxy/redirect/static/php), 30+ config fields
- `upstreams`, `routes`, `custom_headers`, `access_rules`, `basic_auths` — host children (CASCADE)
- `dns_providers` — provider type + encrypted config JSON
- `certificates` — custom SSL cert paths
- `tags`, `groups`, `host_tags` — host organization
- `templates` — reusable host configuration snapshots
- `settings` — key-value store
- `audit_logs` — user action history

### Plugin Tables (prefixed `plugin_{id}_*`)
- `plugin_deploy_projects`, `plugin_deploy_deployments` — deploy plugin
- `plugin_docker_stacks` — Docker Compose stacks
- `plugin_database_instances`, `plugin_database_databases`, `plugin_database_users`
- `plugin_ai_conversations`, `plugin_ai_messages`, `plugin_ai_memories`
- `plugin_backup_configs`, `plugin_backup_snapshots`
- `plugin_monitoring_metrics`, `plugin_monitoring_alert_rules`
- `plugin_appstore_sources`, `plugin_appstore_apps`, `plugin_appstore_installed`
- `plugin_mcpserver_tokens` — MCP API tokens
- `plugin_firewall_*`, `plugin_cronjob_tasks`, `plugin_php_*`

## API Authentication

- **Public**: `POST /api/auth/setup`, `POST /api/auth/login`, `GET /api/auth/need-setup`, `GET /api/auth/altcha-challenge`
- **Protected** (JWT required): `GET /api/hosts`, `GET /api/caddy/status`, etc.
- **Admin-only** (JWT + admin role): mutations, `GET /api/caddy/caddyfile`, cert management
- **Plugin routes**: `/api/plugins/{id}/*` — gated by PluginGuardMiddleware (disabled plugins return 404)
- **MCP tokens**: `wc_` prefixed API tokens with scoped permissions, constant-time hash comparison

JWT: HS256, 24h expiry. 2FA: TOTP with encrypted secret + bcrypt recovery codes.

## Common Tasks

### Adding a new plugin feature
1. Add handler method in `plugins/{id}/handler.go`
2. Register route in `plugins/{id}/plugin.go` Init() (use `ctx.Router` or `ctx.AdminRouter`)
3. Add API function in `web/src/api/index.js`
4. Create/update React component in `web/src/pages/`

### Adding a CoreAPI method
1. Add method signature to `CoreAPI` interface in `internal/plugin/types.go`
2. Implement in `internal/plugin/coreapi.go`
3. Add stub in test files: `internal/plugin/manager_test.go` and `plugins/ai/tools_test.go`

### Security validation for Caddyfile fields
- All user-input strings rendered into Caddyfile must pass `caddy.ValidateCaddyValue()`
- Upstreams: `caddy.ValidateUpstream()` — rejects shell metacharacters
- Domains: `caddy.ValidateDomain()` — strict format check
- Custom directives: `caddy.SanitizeCustomDirectives()` — per-character brace depth tracking
- DNS credentials: `safeDnsValue()` — rejects newlines, braces, quotes
- BasicAuth usernames: validated via `ValidateCaddyValue` before rendering

## Testing

```bash
# All tests
go test ./... -timeout 120s

# Skip slow service tests
go test $(go list ./... | grep -v internal/service) -timeout 60s

# Frontend build check
cd web && npm run build
```

## Gotchas

- **CGO required**: `CGO_ENABLED=1` for SQLite driver (mattn/go-sqlite3)
- **Bool pointers**: `Enabled`, `TLSEnabled`, etc. are `*bool` to distinguish nil from false
- **ApplyConfig rollback**: If `caddy reload` fails, the old Caddyfile is automatically restored
- **Plugin data isolation**: Tables use `plugin_{id}_*` prefix, files go in `data/plugins/{id}/`
- **AI memory isolation**: Memories are scoped by `user_id`; per-user prune limits (1000/user)
- **MCP token permissions**: Empty `"[]"` = no permissions; `["*"]` = full access
- **Frontend dist**: Not embedded in binary; must be at `web/dist/` relative to working directory
- **Docker container ports**: Default to `127.0.0.1` (loopback), not `0.0.0.0`
- **Password minimum**: 8 characters enforced across all endpoints (setup, login, user create)
