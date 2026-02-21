# CaddyPanel 技术架构

## 架构概览

CaddyPanel 采用经典的前后端分离 + 单二进制分发架构：

```
┌─────────────────────────────────────────────────────┐
│  Browser (React SPA)                                │
│  http://panel:39921                                  │
└────────────────┬────────────────────────────────────┘
                 │ REST API (JSON)
                 ▼
┌─────────────────────────────────────────────────────┐
│  Go Backend (Gin)                                   │
│  ┌──────────┐ ┌──────────┐ ┌───────────┐           │
│  │ Handler  │→│ Service  │→│ GORM/DB   │→ SQLite   │
│  └──────────┘ └────┬─────┘ └───────────┘           │
│                    │                                 │
│              ┌─────▼──────┐                         │
│              │ Caddy Mgr  │                         │
│              └─────┬──────┘                         │
│    Render ──→ Caddyfile ──→ caddy reload            │
└────────────────────┼────────────────────────────────┘
                     ▼
┌─────────────────────────────────────────────────────┐
│  Caddy Server                                       │
│  :80 / :443  ──→  upstream backends                 │
│  Admin API :2019                                    │
└─────────────────────────────────────────────────────┘
```

## 技术栈

### 后端

| 组件 | 技术 | 版本 | 用途 |
|------|------|------|------|
| 语言 | Go | 1.26+ | 后端主语言 |
| Web 框架 | Gin | 1.10 | HTTP 路由、中间件 |
| ORM | GORM | 1.25 | 数据库操作、Auto Migrate |
| 数据库 | SQLite | 3.x | 嵌入式持久化（WAL 模式） |
| 认证 | golang-jwt | v5 | JWT Token 签发/验证 |
| 密码 | bcrypt | — | 密码哈希 |
| CORS | gin-contrib/cors | — | 跨域请求支持 |

### 前端

| 组件 | 技术 | 版本 | 用途 |
|------|------|------|------|
| 框架 | React | 19 | UI 框架 |
| 构建 | Vite | 7 | 开发服务器 + 构建 |
| 组件库 | Radix UI Themes | 3.x | 企业级 UI 组件 |
| CSS | Tailwind CSS | v4 | 工具类 CSS |
| 状态管理 | Zustand | 5 | 轻量状态管理 |
| 路由 | React Router | 7 | SPA 路由 |
| HTTP | Axios | 1.13 | API 请求客户端 |
| 图标 | Lucide React | — | SVG 图标库 |

### 部署

| 组件 | 技术 | 用途 |
|------|------|------|
| 进程管理 | systemd | 服务注册、开机启动 |
| 容器 | Docker (multi-stage) | 可选容器化部署 |
| 反代核心 | Caddy 2.x | 反向代理 + 自动 HTTPS |

## 后端分层

```
main.go                        入口：初始化组件、注册路由、启动服务
  │
  ├── internal/config/         配置层：环境变量读取
  ├── internal/database/       数据层：SQLite 初始化 + Auto Migrate
  ├── internal/model/          模型层：GORM 结构体 + DTO
  ├── internal/auth/           认证层：JWT 签发/验证/中间件
  ├── internal/handler/        控制器层：HTTP 请求处理
  │     ├── auth.go              登录/注册/用户信息
  │     ├── host.go              Host CRUD
  │     ├── caddy.go             进程控制
  │     ├── log.go               日志查看
  │     └── export.go            配置导入/导出
  ├── internal/service/        服务层：业务逻辑
  │     └── host.go              Host CRUD + ApplyConfig
  └── internal/caddy/          Caddy 管理层
        ├── renderer.go          数据库 → Caddyfile 渲染
        └── manager.go           进程启停 + 原子写入
```

## 数据流

### 创建 Host

```
用户提交表单
  → POST /api/hosts (handler)
    → HostService.Create (service)
      → GORM 写入 SQLite
      → ApplyConfig()
        → 从 DB 读取全部 Hosts
        → RenderCaddyfile() 生成配置文本
        → Manager.WriteCaddyfile()
          → 写临时文件
          → caddy validate (若可用)
          → 备份旧文件
          → 原子 rename
        → Manager.Reload() (若 Caddy 运行中)
    → 返回 JSON 响应
```

### Caddyfile 生成策略

采用 `strings.Builder` 程序化构建（非模板），好处是：
- 类型安全，无模板注入风险
- 条件逻辑灵活（路径路由、多上游、Header 等）
- Go 原生，无额外依赖

生成的 Caddyfile 结构：

```caddyfile
{
    admin localhost:2019
    log { output file ... }
}

example.com {
    reverse_proxy localhost:3000 localhost:3001 {
        lb_policy round_robin
        header_up X-Real-IP {remote_host}
        ...
    }
    log { output file .../access-example.com.log }
}
```

### 原子写入流程

```
1. 写入 Caddyfile.tmp
2. caddy validate --config Caddyfile.tmp  (可用时)
3. 备份 Caddyfile → backups/Caddyfile.YYYYMMDD-HHMMSS.bak
4. rename(Caddyfile.tmp → Caddyfile)      (原子操作)
5. caddy reload                            (Caddy 运行时)
```

验证失败 → 删除 tmp，原配置不受影响
rename 是文件系统原子操作，中途崩溃不会出现半写状态

## 数据库模型

```
users           用户表（管理员）
  └─ id, username, password(bcrypt), timestamps

hosts           反向代理主表
  ├─ id, domain, enabled, tls_enabled, http_redirect, websocket
  ├─ timestamps
  ├── upstreams[]       上游服务器（一对多）
  │     └─ address, weight, sort_order
  ├── routes[]          路径路由（一对多）
  │     └─ path, upstream_id, sort_order
  ├── custom_headers[]  自定义 Header（一对多）
  │     └─ direction, operation, name, value
  └── access_rules[]    IP 访问控制（一对多）
        └─ rule_type(allow/deny), ip_range(CIDR)
```

## API 端点

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| POST | `/api/auth/setup` | ✗ | 首次创建管理员 |
| POST | `/api/auth/login` | ✗ | 登录 |
| GET | `/api/auth/need-setup` | ✗ | 是否需要初始化 |
| GET | `/api/auth/me` | ✓ | 当前用户 |
| GET | `/api/hosts` | ✓ | 列出全部 Host |
| POST | `/api/hosts` | ✓ | 创建 Host |
| GET | `/api/hosts/:id` | ✓ | 获取 Host |
| PUT | `/api/hosts/:id` | ✓ | 更新 Host |
| DELETE | `/api/hosts/:id` | ✓ | 删除 Host |
| PATCH | `/api/hosts/:id/toggle` | ✓ | 启用/禁用 |
| GET | `/api/caddy/status` | ✓ | Caddy 状态 |
| POST | `/api/caddy/start` | ✓ | 启动 Caddy |
| POST | `/api/caddy/stop` | ✓ | 停止 Caddy |
| POST | `/api/caddy/reload` | ✓ | 重载配置 |
| GET | `/api/caddy/caddyfile` | ✓ | 查看 Caddyfile |
| GET | `/api/logs` | ✓ | 查看日志 |
| GET | `/api/logs/files` | ✓ | 列出日志文件 |
| GET | `/api/logs/download` | ✓ | 下载日志 |
| GET | `/api/config/export` | ✓ | 导出配置 |
| POST | `/api/config/import` | ✓ | 导入配置 |

## 安全设计

- **密码**：bcrypt 哈希存储，永不明文传输/存储
- **JWT**：HS256 签名，24h 过期，Secret 由安装脚本随机生成
- **数据库**：config 文件 600 权限，仅 root 可读
- **systemd**：独立用户运行、ProtectSystem=strict、NoNewPrivileges
- **CORS**：AllowAllOrigins（面板自身服务前端，安全由 JWT 保障）
- **Caddyfile**：写入前 validate，失败不影响运行中配置
