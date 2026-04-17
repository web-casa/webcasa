[English](./README.md) | [简体中文](./README_ZH.md)

<div align="center">

# WebCasa

**A lightweight server control panel built for the Vibe Coding era**

Reverse proxy management powered by [Caddy](https://caddyserver.com) with plugin-based extensibility. Lite mode requires at least 256MB RAM and recommends 512MB+; Full mode requires at least 1GB and recommends 2GB+.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev)
[![Caddy](https://img.shields.io/badge/Caddy-2.x-22B638?logo=caddy&logoColor=white)](https://caddyserver.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

</div>

---

## Product Tiers

- **Lite** — Caddy reverse proxy management out of the box
- **Full** — Lite plus 12 plugin extensions (Containers (Podman) / Project Deployment / AI Assistant / Database / File Manager / Backup / Monitoring / App Store / MCP / Firewall / Cron Jobs / PHP)
- **Full** means the complete edition with all plugin features enabled
- Enable plugins as needed and scale from Lite to Full progressively

## Features

### Site Management (Core)
- **Multiple Host Types** — Reverse proxy, 301/302 redirects, static sites, PHP/FastCGI sites
- **Load Balancing** — Multiple upstream servers with round-robin
- **WebSocket** — Native WebSocket proxy support
- **Custom Headers** — Request/response header rewriting
- **IP Access Control** — IP allowlists/blocklists in CIDR format
- **HTTP Basic Auth** — bcrypt-encrypted HTTP authentication
- **Import/Export** — One-click backup and restore for all configuration in JSON
- **Site Templates** — 6 built-in presets plus custom templates with import/export

### Certificate Management
- **Automatic HTTPS** — Automatic Let's Encrypt certificate issuance and renewal
- **DNS Challenge** — Supports Cloudflare, AliDNS, Tencent Cloud, and Route53
- **Wildcard Certificates** — Request `*.domain.com` certificates via DNS providers
- **Custom Certificates** — Upload your own SSL certificates

### Plugin Ecosystem

| Plugin | Description |
|------|------|
| **Containers** | Podman-based container and Compose management for containers, images, networks, and volumes (Docker-compatible CLI via `podman-docker`) |
| **Project Deployment** | Git-based source deployment for Node.js/Go/PHP/Python with framework detection and zero-downtime release |
| **AI Assistant** | AI chat with 67+ tools, natural-language ops, self-healing, and daily inspections |
| **Database** | MySQL / PostgreSQL / MariaDB / Redis instance management with SQL browser |
| **File Manager** | File browser, online editor, and PTY web terminal |
| **Backup** | Backup panel data / container volumes / databases via Kopia (local/S3/WebDAV/SFTP) |
| **Monitoring** | Real-time system metrics, historical charts, and threshold alerts |
| **App Store** | One-click containerised app installs and project template marketplace |
| **MCP Server** | MCP protocol service for AI IDE integrations (Cursor / Windsurf / Claude Code) |
| **Firewall** | firewalld rule management |
| **Cron Jobs** | General-purpose scheduled task management with cron expressions and shell commands |
| **PHP** | PHP-FPM and FrankenPHP runtime management with one-click PHP site creation |

### Performance and Security
- **Response Compression** — Automatic Gzip + Zstd compression
- **CORS** — One-click cross-origin resource sharing configuration
- **Security Headers** — One-click HSTS / X-Frame-Options / CSP
- **2FA** — TOTP two-factor authentication with recovery codes
- **ALTCHA PoW** — Protection against brute-force attacks during login/setup
- **Audit Logs** — Full operation audit trail

### System
- **Process Control** — One-click Caddy start/stop/reload with graceful zero-downtime reload
- **Caddyfile Editor** — Online editor powered by CodeMirror 6
- **Multi-User Management** — `admin` / `viewer` roles
- **Multilingual** — Chinese / English

## Quick Start

### One-Line Installer (Recommended)

Supports RHEL 9/10 family distributions: CentOS Stream 9/10, AlmaLinux 9/10, Rocky Linux 9/10, Fedora, openAnolis 23, Alibaba Cloud Linux 3, openEuler 22.03+, Kylin V10, and more.

```bash
curl -fsSL https://raw.githubusercontent.com/web-casa/webcasa/main/install.sh | sudo bash
```

After installation, open `http://YOUR_IP:39921`. On first access, the panel will guide you through creating an administrator account.

> **Container runtime:** v0.12 installs **Podman 5.6** (AppStream) with the `podman-docker` shim plus `podman-compose` (EPEL). The panel talks to the rootful Podman socket; existing `docker`/`docker-compose` commands and images keep working unchanged.

**Custom options:**

```bash
# Specify panel port
sudo bash install.sh --port 9090

# Skip Caddy installation if it already exists
sudo bash install.sh --no-caddy

# Uninstall while keeping data
sudo bash install.sh --uninstall

# Full uninstall including data
sudo bash install.sh --purge
```

### Docker Installation

```bash
git clone https://github.com/web-casa/webcasa.git
cd webcasa
docker compose up -d
```

Panel URL: `http://localhost:39921`

### Manual Build

**Requirements:** Go 1.26+, Node.js 24+, GCC

```bash
git clone https://github.com/web-casa/webcasa.git
cd webcasa

# Build frontend + backend
make build

# Run
./webcasa
```

## Development

```bash
# Backend (terminal 1)
go run .
# -> http://localhost:39921

# Frontend (terminal 2)
cd web && npm install && npm run dev
# -> http://localhost:5173 (API is proxied to the backend automatically)
```

## Project Structure

```text
webcasa/
├── main.go                  # Entry point
├── VERSION                  # Version number
├── internal/
│   ├── config/              # Environment configuration
│   ├── model/               # GORM models
│   ├── database/            # SQLite initialization
│   ├── auth/                # JWT + TOTP + ALTCHA
│   ├── crypto/              # AES-GCM encryption
│   ├── caddy/               # Caddy management, rendering, validation
│   ├── service/             # Business logic (Host / Template / TOTP)
│   ├── handler/             # HTTP handlers
│   ├── plugin/              # Plugin framework (Manager / EventBus / ConfigStore / CoreAPI)
│   └── notify/              # Notification system
├── plugins/                 # 12 plugins
│   ├── deploy/              # Project deployment
│   ├── docker/              # Docker management
│   ├── ai/                  # AI assistant
│   ├── database/            # Database management
│   ├── filemanager/         # File management
│   ├── backup/              # Backup
│   ├── monitoring/          # System monitoring
│   ├── appstore/            # App store
│   ├── mcpserver/           # MCP server
│   ├── firewall/            # Firewall
│   ├── cronjob/             # Cron jobs
│   └── php/                 # PHP management
├── web/                     # React 19 frontend
│   └── src/
│       ├── pages/           # 30+ page components
│       └── locales/         # Chinese and English translations
├── install.sh               # One-line installer
├── Dockerfile
└── docker-compose.yml
```

## Configuration

| Environment Variable | Default | Description |
|----------|--------|------|
| `WEBCASA_PORT` | `39921` | Panel port |
| `WEBCASA_DATA_DIR` | `./data` | Data directory |
| `WEBCASA_DB_PATH` | `data/webcasa.db` | Database path |
| `WEBCASA_JWT_SECRET` | Auto-generated | JWT signing secret |
| `WEBCASA_CADDY_BIN` | `caddy` | Caddy binary path |
| `WEBCASA_CADDYFILE_PATH` | `data/Caddyfile` | Caddyfile path |
| `WEBCASA_LOG_DIR` | `data/logs` | Log directory |

## Tech Stack

- **Backend**: Go 1.26+ / Gin / GORM / SQLite
- **Frontend**: React 19 / Vite 7 / Radix UI / Tailwind CSS / Zustand
- **Proxy**: Caddy 2.x
- **i18n**: react-i18next (Chinese / English)

## License

MIT License
