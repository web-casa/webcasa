# Memory — WebCasa 开发记忆

> **目的**: 本文件为跨 AI IDE 的开发记忆文件。无论你是 Claude Code、Gemini Code、Cursor、Windsurf 还是其他 AI 编码工具，请在开始工作前阅读本文件以获取历史上下文。
>
> **Token 优化**: 本文件设计为紧凑格式，避免冗余。详细信息请按需查阅引用的文档。

---

## 项目当前状态

- **版本**: 见 `/VERSION` 文件（唯一真相源）
- **阶段**: Pro 版本全部完成（12 个插件 + 安全审查加固）
- **构建状态**: `go build` + `npm run build` 通过，`go test ./...` 全部通过
- **开发方式**: AI Vibe Coding
- **语言**: 项目文档和注释以中文为主，代码以英文为主

## 文档索引（按需读取）

| 文档 | 内容 | 何时读取 |
|------|------|----------|
| `agents.md` | 架构、插件系统、目录结构、CoreAPI、安全验证 | **修改代码前必读** |
| `stack.md` | 技术栈、插件列表、数据库 Schema、前端路由 | 需要理解底层机制时 |
| `README.md` | 产品功能、安装说明、配置说明 | 需要了解产品功能时 |
| `changelog.md` | 版本变更记录 | 需要了解版本历史时 |
| `notice.md` | Vibe Coding 调试故事和经验教训 | 遇到类似问题时 |
| `docs/install.md` | 详细安装与使用指南 | 部署和运维相关 |

## 架构一句话

Go(Gin) + React19(Vite7) + 12 插件（编译时注册），管理 Caddy 反代。Host DB → `RenderCaddyfile()` → 原子写入 → `caddy reload`（失败自动回滚）。插件通过 CoreAPI(150+ 方法) 访问核心功能。详见 `agents.md`。

## 开发时间线

```
2025-12    Phase 0    项目初始化，基础 Host CRUD + Caddyfile 生成
2026-02    Phase 1-4  核心完善：TLS、DNS Provider、多 Host 类型、Caddyfile 编辑器
2026-02-23 v0.5.1     安全增强、Altcha PoW、主题系统
2026-03-01 v1.0.0     Pro 版本：插件框架 + 12 个插件全部完成
2026-03-20 v0.9.5     全量安全审查加固（100+ 安全/可靠性修复）
```

## 重要约定

### 版本管理
- 版本号**只改** `VERSION` 文件 + `web/package.json`
- Go 通过 `-ldflags "-X main.Version=..."` 注入

### 数据模型
- 布尔字段一律用 `*bool` 指针（GORM 零值陷阱，详见 `notice.md`）
- Host 有 4 种 `host_type`: proxy / redirect / static / php
- Host 有 5 种 `tls_mode`: auto / dns / wildcard / custom / off
- 插件数据表以 `plugin_{id}_*` 为前缀

### Caddyfile 渲染
- `RenderCaddyfile(hosts, cfg, dnsProviders)` 签名接收 3 个参数
- 所有用户输入必须通过 `ValidateCaddyValue()` / `ValidateUpstream()` / `ValidateDomain()` 验证
- DNS 凭据通过 `safeDnsValue()` 验证
- BasicAuth 用户名通过 `ValidateCaddyValue()` 验证
- Custom directives 通过逐字符花括号深度跟踪防注入

### 插件开发
- 插件接口: `Metadata()` / `Init(ctx)` / `Start()` / `Stop()`
- 路由注册在 `Init()` 中：`ctx.Router`（登录用户）/ `ctx.AdminRouter`（仅管理员）
- 新增 CoreAPI 方法需同步更新: `types.go` 接口 + `coreapi.go` 实现 + 两个 test 文件的 stub
- 前端插件路由通过 `FrontendManifest` 声明，侧边栏自动注入

### 前端
- 组件库: Radix UI Themes（不是 shadcn）
- 状态管理: Zustand
- 国际化: react-i18next（`en.json` / `zh.json`）
- 插件名称/描述翻译: `plugins.names.*` / `plugins.descriptions.*`

### 安全
- 密码最低 8 位（所有入口统一）
- MCP Token: 空权限 = 无权限，`["*"]` = 全权限
- AI Memory 按 user_id 隔离
- ApplyConfig 失败时自动回滚旧 Caddyfile
- Docker 容器端口默认绑定 127.0.0.1

## 常用开发命令

```bash
# 验证构建
go build ./...

# 运行测试（跳过慢的 service 测试）
go test $(go list ./... | grep -v internal/service) -timeout 60s

# 完整测试
go test ./... -timeout 120s

# 前端构建
cd web && npm run build
```

## 用户偏好

- 使用**中文**交流
- 新开发计划开始前先**确认问题**再动手
- 偏好一次性完成批量修改
