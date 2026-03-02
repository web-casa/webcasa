# Web.Casa 方案设计稿

> 版本: 1.1 | 日期: 2026-03-01
>
> 变更记录:
> - v1.1 (2026-03-01): Phase 0-4 已完成，功能精简重组阶段。详见 [04-feature-simplification.md](./04-feature-simplification.md)

---

## 1. 总体架构

### 1.1 系统架构图

```
┌──────────────────────────────────────────────────────────────────┐
│                        Browser / IDE (MCP)                       │
└────────────────────────────┬─────────────────────────────────────┘
                             │ HTTPS / WSS
                             ▼
┌──────────────────────────────────────────────────────────────────┐
│                     Web.Casa Panel (:39921)                      │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │                    Gin HTTP Server                          │  │
│  │  ┌──────────┐  ┌──────────────┐  ┌─────────────────────┐  │  │
│  │  │ 核心路由  │  │ 插件路由注册  │  │ WebSocket (终端/日志)│  │  │
│  │  │ /api/*   │  │/api/plugins/*│  │ /ws/*               │  │  │
│  │  └────┬─────┘  └──────┬───────┘  └──────────┬──────────┘  │  │
│  │       │               │                      │             │  │
│  │  ┌────▼───────────────▼──────────────────────▼──────────┐  │  │
│  │  │              统一中间件层                              │  │  │
│  │  │  JWT Auth │ RBAC │ Rate Limit │ Audit │ CORS         │  │  │
│  │  └──────────────────────┬───────────────────────────────┘  │  │
│  └─────────────────────────┼──────────────────────────────────┘  │
│                            │                                     │
│  ┌─────────────────────────▼──────────────────────────────────┐  │
│  │                    Plugin Manager                           │  │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐  │  │
│  │  │ 生命周期  │ │ 依赖解析  │ │ 事件总线  │ │ 前端资源管理 │  │  │
│  │  │ Manager  │ │ Resolver │ │ EventBus │ │ AssetServer  │  │  │
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────────┘  │  │
│  └─────────────────────────┬──────────────────────────────────┘  │
│                            │                                     │
│  ┌─────────────┬───────────▼───────────┬──────────────────────┐  │
│  │   Core      │     Plugins (Go)      │    External Procs    │  │
│  │             │                       │                      │  │
│  │ HostService │ plugin-docker         │ Caddy Server         │  │
│  │ CaddyMgr   │ plugin-project-deploy │ Docker Daemon        │  │
│  │ UserService │ plugin-ai-assistant   │ Kopia Agent          │  │
│  │ AuditSvc   │ plugin-database       │ Node.js Apps         │  │
│  │ ...        │ plugin-filemanager    │ Go Apps              │  │
│  │             │ plugin-backup         │ PHP-FPM              │  │
│  │             │ plugin-monitor        │                      │  │
│  │             │ plugin-app-store      │                      │  │
│  └──────┬──────┴───────────────────────┴──────────────────────┘  │
│         │                                                        │
│  ┌──────▼──────────────────────────────────────────────────────┐  │
│  │                  SQLite (WAL Mode)                           │  │
│  │  core tables │ plugin_{name}_* tables │ settings KV          │  │
│  └─────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

### 1.2 关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 插件机制 | Go Interface + 内置编译 (非 .so) | Go plugin 跨版本兼容差，改用编译时注册模式 |
| 前端插件加载 | 动态 import() + 约定式路由 | 避免 Module Federation 的复杂性 |
| 进程间通信 | 内进程调用 (同一 Go binary) | 2GB 内存约束，不额外启进程 |
| 事件系统 | 内存 EventBus (pub/sub) | 单机场景，无需 Redis/NATS |
| 构建隔离 | Linux namespace / cgroup (可选 Docker) | 安全执行用户代码 |
| AI 接入 | 通用 OpenAI-compatible API | 兼容所有主流 LLM Provider |

---

## 2. 插件系统设计

### 2.1 插件接口定义 (Go)

```go
// internal/plugin/types.go

package plugin

import (
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

// Plugin 是所有插件必须实现的接口
type Plugin interface {
    // 元数据
    Metadata() Metadata

    // 生命周期
    Init(ctx *PluginContext) error    // 初始化(注册路由、迁移数据库)
    Start() error                     // 启动后台任务
    Stop() error                      // 停止
    Uninstall() error                 // 卸载清理
}

// Metadata 插件元数据
type Metadata struct {
    ID           string            // 唯一标识: "docker", "ai-assistant"
    Name         string            // 显示名称: "Docker 管理"
    Version      string            // 语义版本: "1.0.0"
    Description  string            // 描述
    Author       string            // 作者
    Dependencies []string          // 依赖的其他插件 ID
    Priority     int               // 加载优先级 (越小越先)
    Icon         string            // 图标名称 (Lucide icon name)
    Category     string            // 分类: "deploy", "database", "tool"
}

// PluginContext 插件运行上下文
type PluginContext struct {
    DB          *gorm.DB           // 数据库连接 (插件用独立表前缀)
    Router      *gin.RouterGroup   // API 路由组 /api/plugins/{id}/
    EventBus    *EventBus          // 事件总线
    Logger      *slog.Logger       // 日志器
    DataDir     string             // 插件数据目录
    ConfigStore ConfigStore        // 插件配置读写
    CoreAPI     CoreAPI            // 核心功能 API (如添加反代)
}

// CoreAPI 插件可调用的核心功能
type CoreAPI interface {
    // Host 管理
    CreateHost(req CreateHostRequest) (*Host, error)
    UpdateHost(id uint, req UpdateHostRequest) error
    DeleteHost(id uint) error
    ReloadCaddy() error

    // 用户信息
    GetCurrentUser(c *gin.Context) (*User, error)

    // 设置
    GetSetting(key string) (string, error)
    SetSetting(key, value string) error
}
```

### 2.2 插件管理器

```go
// internal/plugin/manager.go

type Manager struct {
    plugins    map[string]Plugin        // 已注册插件
    order      []string                 // 按依赖拓扑排序的加载顺序
    eventBus   *EventBus
    db         *gorm.DB
    router     *gin.Engine
    dataDir    string
}

// 插件注册 (编译时)
func (m *Manager) Register(p Plugin) error

// 初始化所有插件 (依赖排序)
func (m *Manager) InitAll() error

// 启动/停止
func (m *Manager) StartAll() error
func (m *Manager) StopAll() error

// 按 ID 获取插件
func (m *Manager) Get(id string) (Plugin, bool)

// 列出已安装插件
func (m *Manager) List() []PluginInfo

// 启用/禁用单个插件
func (m *Manager) Enable(id string) error
func (m *Manager) Disable(id string) error
```

### 2.3 事件总线

```go
// internal/plugin/eventbus.go

type Event struct {
    Type    string                 // "host.created", "deploy.failed", "docker.started"
    Payload map[string]interface{}
    Source  string                 // 来源插件 ID
    Time    time.Time
}

type EventHandler func(event Event) error

type EventBus struct {
    handlers map[string][]EventHandler
    mu       sync.RWMutex
}

func (eb *EventBus) Subscribe(eventType string, handler EventHandler)
func (eb *EventBus) Publish(event Event) error
```

### 2.4 前端插件机制

插件的前端采用**约定式目录结构**，构建后生成标准 JS bundle:

```
plugin-docker/
  web/
    src/
      pages/
        DockerOverview.jsx     → 侧边栏主页面
        DockerContainers.jsx   → 子页面
        DockerImages.jsx       → 子页面
      manifest.json            → 前端路由+菜单声明
```

**manifest.json:**
```json
{
  "id": "docker",
  "routes": [
    { "path": "/docker", "component": "DockerOverview", "menu": true, "icon": "Container", "label": "Docker" },
    { "path": "/docker/containers", "component": "DockerContainers" },
    { "path": "/docker/images", "component": "DockerImages" }
  ],
  "menuGroup": "deploy",
  "menuOrder": 20
}
```

**核心前端加载机制:**
```jsx
// web/src/plugins/PluginLoader.jsx

// 核心应用扫描 /api/plugins/frontend-manifests 获取所有启用插件的路由
// 动态注册到 React Router

function loadPluginRoutes() {
  const manifests = await fetch('/api/plugins/frontend-manifests');
  return manifests.flatMap(m =>
    m.routes.map(r => ({
      path: r.path,
      element: React.lazy(() => import(`/api/plugins/${m.id}/assets/${r.component}.js`))
    }))
  );
}
```

### 2.5 插件数据隔离

```
数据库表命名: plugin_{id}_{table}
  例: plugin_docker_containers
      plugin_docker_images
      plugin_backup_schedules
      plugin_ai_conversations

数据目录: {DATA_DIR}/plugins/{id}/
  例: data/plugins/docker/
      data/plugins/backup/
      data/plugins/ai-assistant/

配置存储: settings 表, key = "plugin.{id}.{key}"
  例: plugin.ai-assistant.llm_base_url
      plugin.docker.socket_path
```

---

## 3. 核心插件设计

### 3.1 plugin-project-deploy (项目部署)

```
核心功能:
  ├── Git 克隆 (支持 GitHub/GitLab/Gitea + Deploy Key)
  ├── 框架自动检测 (package.json/go.mod/composer.json)
  ├── 构建流程编排
  │     ├── Node.js: npm install → npm run build → pm2/node start
  │     ├── Go: go build → supervisord/systemd
  │     ├── PHP: composer install → php-fpm
  │     └── 自定义: Dockerfile / 脚本
  ├── 进程管理 (启动/停止/重启/日志)
  ├── 环境变量管理 (.env)
  ├── 自动反代配置 (调用 CoreAPI)
  ├── Webhook 触发自动重新部署
  └── 回滚 (保留最近 N 个版本)

数据模型:
  plugin_deploy_projects
    ├── id, name, domain
    ├── git_url, git_branch, deploy_key
    ├── framework (nextjs/nuxt/laravel/gin/custom)
    ├── build_command, start_command
    ├── env_vars (JSON, 加密)
    ├── port, status (building/running/stopped/error)
    ├── current_version, versions (JSON)
    └── auto_deploy (webhook)

API:
  POST   /api/plugins/deploy/projects          — 创建项目
  GET    /api/plugins/deploy/projects           — 列出项目
  GET    /api/plugins/deploy/projects/:id       — 项目详情
  PUT    /api/plugins/deploy/projects/:id       — 更新项目
  DELETE /api/plugins/deploy/projects/:id       — 删除项目
  POST   /api/plugins/deploy/projects/:id/build — 触发构建
  POST   /api/plugins/deploy/projects/:id/start — 启动
  POST   /api/plugins/deploy/projects/:id/stop  — 停止
  POST   /api/plugins/deploy/projects/:id/rollback — 回滚
  GET    /api/plugins/deploy/projects/:id/logs  — 构建/运行日志
  POST   /api/plugins/deploy/webhook/:token     — Git Webhook
  GET    /api/plugins/deploy/detect             — 检测框架类型
```

### 3.2 plugin-docker (Docker 管理)

```
┌─────────────────────────────────────────────┐
│              Docker Plugin                   │
│                                             │
│  ┌─────────────────────────────────────┐    │
│  │         简单模式 (默认)              │    │
│  │  应用/Stack 列表                     │    │
│  │  一键: 部署 / 重启 / 更新 / 回滚    │    │
│  │        备份 / 日志                   │    │
│  └─────────────────────────────────────┘    │
│                                             │
│  ┌─────────────────────────────────────┐    │
│  │         高级模式                     │    │
│  │  容器 │ 镜像 │ 网络 │ 卷 │ Compose  │    │
│  │  资源限制 │ 健康检查 │ 日志          │    │
│  └─────────────────────────────────────┘    │
│                                             │
│  ┌─────────────────────────────────────┐    │
│  │     Docker Socket 通信层             │    │
│  │  /var/run/docker.sock               │    │
│  │  Docker API v1.43+                  │    │
│  └─────────────────────────────────────┘    │
└─────────────────────────────────────────────┘

API:
  # Stack (简单模式)
  GET    /api/plugins/docker/stacks             — 列出 Stack
  POST   /api/plugins/docker/stacks             — 创建 Stack (从 Compose)
  POST   /api/plugins/docker/stacks/:id/up      — 启动
  POST   /api/plugins/docker/stacks/:id/down    — 停止
  POST   /api/plugins/docker/stacks/:id/restart — 重启
  POST   /api/plugins/docker/stacks/:id/pull    — 拉取更新
  GET    /api/plugins/docker/stacks/:id/logs    — 日志

  # 容器 (高级模式)
  GET    /api/plugins/docker/containers         — 列出容器
  POST   /api/plugins/docker/containers/:id/start
  POST   /api/plugins/docker/containers/:id/stop
  POST   /api/plugins/docker/containers/:id/restart
  DELETE /api/plugins/docker/containers/:id
  GET    /api/plugins/docker/containers/:id/logs
  GET    /api/plugins/docker/containers/:id/stats — 实时资源

  # 镜像/网络/卷
  GET    /api/plugins/docker/images
  DELETE /api/plugins/docker/images/:id
  GET    /api/plugins/docker/networks
  GET    /api/plugins/docker/volumes
```

### 3.3 plugin-ai-assistant (AI 助手)

```
架构:
  ┌──────────────────────────────┐
  │       AI Assistant Plugin     │
  │                              │
  │  ┌────────────────────────┐  │
  │  │    Chat Interface      │  │  ← 面板右下角浮动对话框
  │  │  (Streaming SSE)       │  │
  │  └───────────┬────────────┘  │
  │              │               │
  │  ┌───────────▼────────────┐  │
  │  │   Context Collector    │  │  ← 自动收集相关上下文
  │  │  - 当前页面上下文       │  │
  │  │  - 最近错误日志         │  │
  │  │  - 项目配置信息         │  │
  │  │  - 系统状态             │  │
  │  └───────────┬────────────┘  │
  │              │               │
  │  ┌───────────▼────────────┐  │
  │  │   LLM Client          │  │  ← OpenAI-compatible API
  │  │  (Streaming)           │  │
  │  └────────────────────────┘  │
  └──────────────────────────────┘

核心功能:
  1. 智能对话 — 流式输出，Markdown 渲染
  2. 错误诊断 — 自动注入日志上下文，分析部署错误
  3. Text-to-Template — 自然语言 → Docker Compose YAML
  4. 配置建议 — 分析项目类型，推荐最优配置
  5. 每步向导旁「问 AI」— 解释字段含义和最佳实践

API:
  POST   /api/plugins/ai/chat           — 发送消息 (SSE 流式响应)
  GET    /api/plugins/ai/conversations   — 对话历史
  DELETE /api/plugins/ai/conversations/:id
  POST   /api/plugins/ai/generate-compose — Text-to-Template
  POST   /api/plugins/ai/diagnose        — 自动诊断 (传入日志)
  GET    /api/plugins/ai/config          — AI 配置
  PUT    /api/plugins/ai/config          — 更新 AI 配置
```

### 3.4 plugin-database (数据库管理)

```
支持的数据库 (均通过 Docker 运行):
  ├── MySQL 8.x
  ├── MariaDB 11.x
  ├── PostgreSQL 16.x
  ├── Redis 7.x
  ├── SQLite (本地文件)
  └── Turso (LibSQL, 通过 Docker 或远程连接)

功能:
  ├── 一键创建数据库实例 (Docker 容器)
  ├── 创建数据库/用户/权限
  ├── 备份/恢复 (集成 plugin-backup)
  ├── 连接信息展示 (连接字符串、端口)
  ├── 基础查询界面 (可选，轻量级)
  └── 监控 (连接数、内存、慢查询)

依赖: plugin-docker (通过 Docker 运行数据库实例)
```

### 3.5 plugin-filemanager (文件管理器 + 终端)

```
文件管理器:
  ├── 目录浏览 (树形 + 列表)
  ├── 文件上传/下载 (拖拽上传，分片上传)
  ├── 在线编辑 (CodeMirror, 语法高亮)
  ├── 文件搜索
  ├── 权限修改 (chmod/chown)
  └── 压缩/解压

Web 终端:
  ├── xterm.js + WebSocket
  ├── 多标签页
  ├── 终端大小自适应
  └── 安全: 仅限管理员访问

API:
  GET    /api/plugins/files/list?path=       — 列出目录
  GET    /api/plugins/files/read?path=       — 读取文件
  POST   /api/plugins/files/write            — 写入文件
  POST   /api/plugins/files/upload           — 上传文件
  GET    /api/plugins/files/download?path=   — 下载文件
  POST   /api/plugins/files/mkdir            — 创建目录
  DELETE /api/plugins/files/delete           — 删除
  POST   /api/plugins/files/rename           — 重命名/移动
  POST   /api/plugins/files/compress         — 压缩
  POST   /api/plugins/files/extract          — 解压
  WS     /ws/plugins/files/terminal          — WebSocket 终端
```

### 3.6 plugin-backup (备份)

```
基于 Kopia:
  ├── 目标: S3 / WebDAV / 本地目录 / SFTP
  ├── 增量备份 (Kopia 原生支持)
  ├── 定时策略 (每日/每周/每月)
  ├── 保留策略 (保留 N 份/保留 N 天)
  ├── 备份范围:
  │     ├── 面板数据 (SQLite DB + 配置)
  │     ├── 项目源码 + 数据
  │     ├── Docker 卷
  │     └── 数据库 dump
  └── 一键恢复
```

### 3.7 plugin-monitor (系统监控)

```
采集指标:
  ├── CPU 使用率 (总体 + 每核)
  ├── 内存使用 (total/used/available/swap)
  ├── 磁盘 I/O + 使用率
  ├── 网络流量 (in/out per interface)
  ├── 系统负载 (1/5/15min)
  └── Docker 容器资源 (CPU/MEM per container)

存储: SQLite 表, 按分钟聚合, 自动清理 30 天以上数据
展示: 实时仪表盘 + 历史图表 (Recharts)
告警: 阈值触发 → Webhook / Email 通知
```

### 3.8 plugin-app-store (应用商店)

```
兼容 Runtipi appstore 格式:
  app-id/
    ├── config.json        — 应用元数据
    ├── docker-compose.yml — Compose 定义
    ├── description.md     — 应用描述
    └── logo.png          — 应用图标

功能:
  ├── 浏览应用列表 (分类、搜索)
  ├── 一键安装 (填参数 → 生成 compose → 启动)
  ├── 应用更新
  ├── 自动反代配置
  └── AI 生成自定义模板 (Text-to-Template)

数据源:
  ├── 内置精选应用
  ├── 兼容 runtipi-appstore 仓库
  └── 用户自定义应用源
```

---

## 4. 新手友好体验设计

### 4.1 创建项目向导

```
步骤 1/4: 选择语言/框架          [问 AI 💬]
  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐
  │Node.js│ │ PHP  │ │  Go  │ │Docker│
  └──────┘ └──────┘ └──────┘ └──────┘
  推荐框架: Next.js | Nuxt | Express | ...

步骤 2/4: 选择部署方式            [问 AI 💬]
  ○ 从 Git 仓库部署 (推荐)
  ○ 上传源码
  ○ Docker 镜像
  ○ 从模板创建

步骤 3/4: 配置域名和端口          [问 AI 💬]
  域名: [app.example.com    ]
  端口: [3000               ] (自动检测)

步骤 4/4: 可选增强                [问 AI 💬]
  ☑ 启用 HTTPS (Let's Encrypt)
  ☐ 启用自动备份
  ☐ 启用系统监控
  ☐ 配置数据库
```

### 4.2 新手模式 vs 高级模式

```
全局切换: 设置 → 界面模式 → 新手/高级

新手模式:
  - 只显示必要字段
  - 大量默认值 (自动检测最优配置)
  - 每个关键操作旁有说明文字
  - 操作确认弹窗 (防止误操作)
  - 部署失败自动弹出 AI 诊断

高级模式:
  - 显示全部配置项
  - 可编辑原始 YAML/Caddyfile
  - 显示 Docker 底层信息
  - 显示完整日志和调试信息
```

---

## 5. 目录结构规划 (扩展后)

```
caddypanel/
├── main.go                          # 入口 + 插件注册
├── internal/
│   ├── config/                      # 配置
│   ├── database/                    # 数据库
│   ├── model/                       # 核心模型
│   ├── auth/                        # 认证
│   ├── handler/                     # 核心 API handler
│   ├── service/                     # 核心业务逻辑
│   ├── caddy/                       # Caddy 管理
│   └── plugin/                      # ← 新增: 插件框架
│       ├── types.go                 #   插件接口定义
│       ├── manager.go               #   插件管理器
│       ├── eventbus.go              #   事件总线
│       ├── registry.go              #   插件注册表
│       └── context.go               #   插件上下文
├── plugins/                         # ← 新增: 插件实现
│   ├── docker/
│   │   ├── plugin.go                #   Plugin 接口实现
│   │   ├── handler.go               #   API handlers
│   │   ├── service.go               #   业务逻辑
│   │   ├── model.go                 #   数据模型
│   │   └── web/                     #   前端资源
│   ├── deploy/
│   ├── ai-assistant/
│   ├── database/
│   ├── filemanager/
│   ├── backup/
│   ├── monitor/
│   └── app-store/
├── web/                             # 核心前端
│   └── src/
│       ├── plugins/                 # ← 新增: 插件加载器
│       │   ├── PluginLoader.jsx
│       │   ├── PluginRouter.jsx
│       │   └── PluginSidebar.jsx
│       ├── components/
│       │   └── AIChatWidget.jsx     # ← 新增: AI 浮动对话框
│       └── ...
└── docs/
    ├── 01-requirements-analysis.md
    ├── 02-architecture-design.md
    └── 03-development-tasks.md
```

---

## 6. 数据流设计

### 6.1 项目部署流程

```
用户/AI ──→ POST /api/plugins/deploy/projects
               │
               ▼
         创建 Project 记录 (status=pending)
               │
               ▼
         触发 Event: "deploy.started"
               │
               ▼
    ┌──────────────────────┐
    │    Build Pipeline     │
    │  1. git clone         │
    │  2. 检测框架           │  ← 自动识别 package.json / go.mod
    │  3. npm install        │
    │  4. npm run build      │  ← 实时输出日志到 WebSocket
    │  5. 启动进程           │
    └──────────┬───────────┘
               │
          成功 │ 失败
          ┌────┴────┐
          ▼         ▼
     status=      status=error
     running      触发 Event: "deploy.failed"
          │              │
          ▼              ▼
     CoreAPI:       AI 自动读取日志
     CreateHost()   生成修复建议
     (自动反代)     推送给用户
```

### 6.2 AI Text-to-Template 流程

```
用户: "我要一个带 Redis 缓存和 Postgres 的 N8N 环境"
               │
               ▼
    POST /api/plugins/ai/generate-compose
               │
               ▼
    ┌────────────────────────┐
    │  构建 Prompt:           │
    │  System: 你是 Docker    │
    │  Compose 专家...        │
    │  User: 用户需求         │
    │  Context: 服务器信息     │
    └───────────┬────────────┘
               │
               ▼
         LLM API (Streaming)
               │
               ▼
    ┌────────────────────────┐
    │  解析响应:              │
    │  - 提取 YAML 代码块     │
    │  - 验证 Compose 语法    │
    │  - 安全检查 (无特权等)   │
    └───────────┬────────────┘
               │
               ▼
    返回 Compose YAML + 说明
    用户确认 → 一键部署
```

---

## 7. 安全设计

### 7.1 插件安全

```
原则: 插件是信任代码 (编译进二进制)，但限制运行时能力

限制:
  ├── 插件只能操作自己的数据表 (plugin_{id}_*)
  ├── 插件只能读写自己的数据目录
  ├── 插件 API 路由统一经过 JWT 鉴权
  ├── 插件不能直接访问其他插件的数据
  └── 插件通过 CoreAPI 操作核心功能 (有权限检查)
```

### 7.2 项目部署安全

```
构建环境:
  ├── 使用独立 Linux 用户执行构建
  ├── 限制网络访问 (只允许 npm registry / github 等)
  ├── 限制文件系统 (只能访问项目目录)
  ├── 资源限制 (CPU/MEM/时间超时)
  └── 构建产物扫描 (可选)

运行环境:
  ├── 每个项目一个独立系统用户
  ├── 进程使用 systemd/supervisord 管理
  ├── 端口自动分配 (避免冲突)
  └── 环境变量加密存储
```

### 7.3 AI 安全

```
  ├── API Key 使用 AES-GCM 加密存储
  ├── AI 请求不包含敏感环境变量
  ├── AI 生成的 Compose 文件经过安全检查
  │     ├── 禁止 privileged: true
  │     ├── 禁止 network_mode: host (默认)
  │     ├── 禁止挂载 /etc, /var/run/docker.sock 等
  │     └── 警告过大的资源请求
  └── AI 不能直接执行命令，只能建议
```

---

## 8. 技术选型细节

### 8.1 新增依赖

| 组件 | 技术 | 用途 |
|------|------|------|
| Docker 客户端 | github.com/docker/docker | Docker API 通信 |
| WebSocket | gorilla/websocket | 终端 + 实时日志 |
| 前端终端 | xterm.js | Web 终端模拟器 |
| 备份引擎 | Kopia CLI | 增量备份 (外部进程调用) |
| 系统监控 | shirou/gopsutil | CPU/MEM/Disk/Net 采集 |
| 进程管理 | supervisord 或 systemd API | 管理部署的应用进程 |
| 图表 | Recharts | 监控仪表盘 |
| Markdown | react-markdown | AI 对话渲染 |
| SSE | 原生 Gin | AI 流式响应 |

### 8.2 前端新增页面 (Pro)

| 页面 | 对应插件 | 描述 |
|------|---------|------|
| /plugins | 核心 | 插件市场 + 已安装管理 |
| /deploy | plugin-deploy | 项目列表 + 部署向导 |
| /deploy/:id | plugin-deploy | 项目详情 (日志/环境变量/版本) |
| /docker | plugin-docker | Docker 总览 (简单模式) |
| /docker/advanced | plugin-docker | 高级模式 (容器/镜像/网络/卷) |
| /databases | plugin-database | 数据库实例管理 |
| /files | plugin-filemanager | 文件管理器 |
| /terminal | plugin-filemanager | Web 终端 |
| /backups | plugin-backup | 备份任务 + 恢复 |
| /monitoring | plugin-monitor | 系统监控仪表盘 |
| /app-store | plugin-app-store | 应用商店 |
| /templates | plugin-template-market | 项目模板市场 |
