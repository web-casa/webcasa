# WebCasa 技术架构

## 架构概览

WebCasa 采用前后端分离 + 插件化架构：

```
┌─────────────────────────────────────────────────────┐
│  Browser (React SPA)                                │
│  http://panel:39921                                  │
└────────────────┬────────────────────────────────────┘
                 │ REST API (JSON) + SSE + WebSocket
                 ▼
┌─────────────────────────────────────────────────────┐
│  Go Backend (Gin)                                   │
│                                                     │
│  ┌──────────┐ ┌──────────┐ ┌───────────┐           │
│  │ Handler  │→│ Service  │→│ GORM/DB   │→ SQLite   │
│  └──────────┘ └──────────┘ └───────────┘           │
│                                                     │
│  ┌──────────────────────────────────────┐           │
│  │ Plugin Manager                       │           │
│  │ ┌────────┐ ┌─────────┐ ┌─────────┐  │           │
│  │ │EventBus│ │ConfigSto│ │ CoreAPI │  │           │
│  │ └────────┘ └─────────┘ └─────────┘  │           │
│  │ Plugins: deploy│docker│ai│db│file│  │           │
│  │   backup│monitor│appstore│mcp│...   │           │
│  └──────────────────────┬───────────────┘           │
│                         │                           │
│              ┌──────────▼──────┐                    │
│              │   Caddy Manager │                    │
│              └──────────┬──────┘                    │
│    Render ──→ Caddyfile ──→ caddy reload            │
└──────────────────────────┼──────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────┐
│  Caddy Server                                       │
│  :80 / :443  ──→  upstream backends                 │
│  Admin API :2019                                    │
└─────────────────────────────────────────────────────┘
```

## 技术栈

### 后端
| 组件 | 技术 | 版本 |
|------|------|------|
| 语言 | Go | 1.26+ |
| HTTP 框架 | Gin | v1.10 |
| ORM | GORM | v1.25 |
| 数据库 | SQLite (WAL) | mattn/go-sqlite3 |
| 认证 | JWT (HS256) + TOTP + ALTCHA PoW | - |
| 加密 | AES-GCM (secrets), bcrypt (passwords) | - |
| 定时任务 | robfig/cron/v3 | - |
| Docker | Docker SDK + docker compose CLI | - |
| 备份 | Kopia CLI | - |
| MCP | mcp-go SDK | - |

### 前端
| 组件 | 技术 | 版本 |
|------|------|------|
| 框架 | React | 19 |
| 构建 | Vite | 7 |
| UI 库 | Radix UI Themes | - |
| 样式 | Tailwind CSS | 4 |
| 状态 | Zustand | - |
| 国际化 | react-i18next | - |
| 编辑器 | CodeMirror 6 | - |
| 图标 | Lucide React | - |
| HTTP | Axios | - |

## 插件系统

### 架构

```
Plugin Interface
├── Metadata()   → ID, Name, Version, Dependencies, Priority
├── Init(ctx)    → DB migrate, route registration, service init
├── Start()      → Background tasks (schedulers, watchers)
└── Stop()       → Cleanup

Plugin Context
├── DB           → GORM connection (tables: plugin_{id}_*)
├── Router       → /api/plugins/{id}/ (JWT required)
├── AdminRouter  → /api/plugins/{id}/ (JWT + admin)
├── EventBus     → Pub/sub with wildcard matching
├── ConfigStore  → Scoped key-value settings
├── CoreAPI      → 150+ methods (host/deploy/docker/db/file/...)
├── DataDir      → data/plugins/{id}/
└── Logger       → slog with plugin ID prefix
```

### 插件列表

| ID | 插件 | 优先级 | 依赖 | 数据表 |
|----|------|--------|------|--------|
| deploy | 项目部署 | 5 | - | projects, deployments |
| docker | Docker | 10 | - | stacks |
| database | 数据库 | 15 | docker | instances, databases, users |
| php | PHP | 16 | docker | runtimes, sites |
| ai | AI 助手 | 30 | - | conversations, messages, memories |
| filemanager | 文件管理 | 40 | - | (无) |
| firewall | 防火墙 | 45 | - | (无) |
| cronjob | 定时任务 | 50 | - | tasks, logs |
| monitoring | 系统监控 | 50 | - | metrics, alert_rules, alert_history |
| backup | 备份 | 55 | - | configs, snapshots |
| appstore | 应用商店 | 60 | docker | sources, apps, installed, templates |
| mcpserver | MCP 服务 | 90 | - | tokens |

### 插件间通信

- **EventBus** — 内存 pub/sub，支持 wildcard（如 `deploy.*`）
- **CoreAPI** — 150+ 方法，插件通过 CoreAPI 访问其他插件的数据和功能
- **数据隔离** — 每个插件的表以 `plugin_{id}_` 为前缀，文件存储在 `data/plugins/{id}/`

## 安全架构

### 认证层
- JWT HS256（24h 过期）
- TOTP 二步验证（AES-GCM 加密存储密钥）
- bcrypt 恢复码
- ALTCHA PoW 防暴力破解

### 授权层
- `protected` 路由 — 任何登录用户
- `adminOnly` 路由 — 仅管理员
- 插件路由 — PluginGuardMiddleware（禁用插件返回 404）
- MCP Token — `["*"]` 全权限 / 精细范围控制

### 输入验证
- Caddyfile 注入防护 — `ValidateCaddyValue()`, `ValidateDomain()`, `ValidateUpstream()`
- Custom directives — 逐字符花括号深度跟踪
- DNS 凭据 — `safeDnsValue()` 拒绝换行/花括号
- SQL 注入 — 参数化查询 + `isReadOnlyQuery()` + `containsUnquotedSemicolon()`
- 路径穿越 — `filepath.EvalSymlinks()` + 前缀检查
- 命令注入 — upstream/域名字段字符白名单

### 凭据保护
- API Key / Deploy Key — AES-GCM 加密存储
- 数据库 Root 密码 — CoreAPI.EncryptSecret/DecryptSecret
- DNS Provider Config — JSON 存储，API 响应中 mask 为 `***`
- MCP Token — SHA-256 哈希存储，constant-time 比较

## 数据库 Schema

### 核心表
```
users           — 用户（bcrypt 密码 + TOTP + 角色）
hosts           — 站点（域名唯一，30+ 配置字段）
upstreams       — 上游服务器（CASCADE 删除）
routes          — 路径路由（upstream_id 映射）
custom_headers  — 自定义响应头
access_rules    — IP 访问规则
basic_auths     — HTTP 基础认证
dns_providers   — DNS 提供商配置
certificates    — 自定义 SSL 证书
tags / groups   — 站点标签和分组
host_tags       — 多对多关联
templates       — 站点模板
settings        — 系统设置 KV
audit_logs      — 审计日志
plugin_states   — 插件启用/禁用状态
```

### 插件表（自动迁移）
每个插件在 `Init()` 中通过 `db.AutoMigrate()` 创建自己的表，表名以 `plugin_{id}_` 为前缀。

## 前端路由

| 路径 | 页面 | 说明 |
|------|------|------|
| `/` | Dashboard | 仪表盘 |
| `/hosts` | HostList | 站点管理 |
| `/editor` | CaddyfileEditor | Caddyfile 编辑器 |
| `/settings` | Settings | 系统设置（6 个 Tab） |
| `/docker` | DockerOverview | Docker 管理 |
| `/deploy` | ProjectList | 项目部署 |
| `/database` | DatabaseInstances | 数据库管理 |
| `/files` | FileManager | 文件管理 |
| `/terminal` | WebTerminal | Web 终端 |
| `/monitoring` | MonitoringDashboard | 系统监控 |
| `/backup` | BackupManager | 备份管理 |
| `/store` | AppStore | 应用商店 |
| `/firewall` | FirewallManager | 防火墙 |
| `/php` | PHPManager | PHP 管理 |
| `/cronjob` | CronJobManager | 定时任务 |
| `/mcp` | MCPManager | MCP Token 管理 |
| `/plugins` | PluginsPage | 插件管理 |
