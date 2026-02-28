# Web.Casa 需求分析文档

> 版本: 1.0 | 日期: 2026-02-28 | 作者: AI Vibe Coding

---

## 1. 项目背景与定位

### 1.1 市场痛点

| 现有产品 | 痛点 |
|---------|------|
| cPanel / Plesk | 架构老旧、资源占用高(4G+)、授权昂贵、面向传统 LAMP 时代 |
| 宝塔面板 | 面向云计算时代设计、闭源生态、插件收费模式重、与现代开发工作流脱节 |
| Nginx Proxy Manager | 仅反向代理管理、无项目部署能力、无 AI 辅助 |
| Coolify / CapRover | 面向开发者但门槛高、资源占用大(4G+)、不够轻量 |

### 1.2 产品定位

**Web.Casa** — 为 Vibe Coding 时代而生的轻量级服务器控制面板。

核心差异化:
- **AI 原生**: 面板内置 AI 辅助，对接 Vibe Coding IDE (MCP/Skills)，AI 可直接操控面板部署
- **极致轻量**: 最低 2GB 内存即可运行全功能面板
- **渐进式架构**: Lite → Pro 通过插件市场平滑升级，按需扩展

### 1.3 产品形态

```
┌─────────────────────────────────────────────────┐
│              Web.Casa Pro (全功能)                │
│  ┌───────────────────────────────────────────┐  │
│  │  项目部署 │ Docker │ 数据库 │ 备份 │ 监控  │  │
│  │  应用市场 │ AI助手 │ 文件管理 │ 终端      │  │
│  └───────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────┐  │
│  │         Web.Casa Lite (核心面板)           │  │
│  │  Caddy 反代 │ SSL │ 域名 │ 用户 │ 日志    │  │
│  └───────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

- **Web.Casa Lite** = 现有项目 (Caddy 反向代理管理面板)
- **Web.Casa Pro** = Lite + 插件市场安装所有扩展模块
- 用户可以逐步安装插件从 Lite 升级到 Pro，也可以一键安装 Pro 版

---

## 2. 用户画像

### 2.1 主要用户

| 用户类型 | 特征 | 核心需求 |
|---------|------|---------|
| Vibe Coder | 使用 AI IDE (Cursor/Windsurf/Claude Code) 编程的开发者 | 通过 AI 指令一键部署项目到服务器 |
| 独立开发者 | 全栈开发，自己维护服务器 | 快速部署 Next.js/Nuxt/Laravel 等项目，低运维成本 |
| 技术新手 | 刚学编程，想上线自己的项目 | 向导式操作，遇到问题有 AI 帮忙 |
| 小团队 | 2-5 人创业团队 | 低成本替代 Vercel/Railway 的自托管方案 |

### 2.2 典型使用场景

1. **场景 A: Vibe Coding 部署**
   - 用户在 Cursor 中用 AI 写完了一个 Next.js 项目
   - 通过 MCP 调用 Web.Casa API: "部署到我的服务器，域名 app.example.com"
   - 面板自动: 拉代码 → 构建 → 启动 → 配域名 → 开 HTTPS
   - 部署失败时，AI 自动读取日志并修复

2. **场景 B: Docker 应用一键部署**
   - 用户想跑一个 N8N + Redis + Postgres
   - 对面板 AI 说: "帮我部署一个带 Redis 和 Postgres 的 N8N"
   - AI 生成 Docker Compose → 面板一键拉起
   - 自动配好反向代理和 HTTPS

3. **场景 C: 新手部署博客**
   - 用户跟着向导: 选框架(Hugo) → 选部署方式(Docker) → 填域名 → 开 HTTPS
   - 每一步旁边有「问 AI」按钮
   - 3 分钟完成上线

---

## 3. 功能需求清单

### 3.1 核心层 — Web.Casa Lite (现有 + 增强)

现有功能已经完善，作为 Pro 的基座:

| 模块 | 现状 | 需增强 |
|------|------|--------|
| Caddy 反向代理管理 | ✅ 已完成 | — |
| SSL/TLS 自动化 | ✅ 已完成 | — |
| DNS Provider | ✅ 已完成 | — |
| 多用户 + 审计 | ✅ 已完成 | — |
| 配置模板 | ✅ 已完成 | — |
| 主题/国际化 | ✅ 已完成 | — |
| **插件系统** | ❌ 未实现 | **必须新建，作为 Pro 的基础** |
| **插件市场 UI** | ❌ 未实现 | **必须新建** |
| **系统事件总线** | ❌ 未实现 | **插件间通信基础** |

### 3.2 扩展层 — Pro 功能 (均以插件形式实现)

#### P0 — 必须有 (MVP)

| # | 插件名 | 功能描述 | 依赖 |
|---|--------|---------|------|
| 1 | **plugin-project-deploy** | 项目源码部署 (Node.js/PHP/Go)，Git 拉取、构建、进程管理 | 核心 |
| 2 | **plugin-docker** | Docker/Compose 管理，简单模式+高级模式 | 核心 |
| 3 | **plugin-ai-assistant** | AI 助手，用户配置 LLM，日志分析，Text-to-Template | 核心 |
| 4 | **plugin-database** | 数据库管理 (MySQL/MariaDB/PostgreSQL/Redis/SQLite) | plugin-docker |
| 5 | **plugin-filemanager** | Web 文件管理器 + Web 终端 (xterm.js) | 核心 |

#### P1 — 应该有

| # | 插件名 | 功能描述 | 依赖 |
|---|--------|---------|------|
| 6 | **plugin-backup** | Kopia 备份，S3/WebDAV，增量备份，定时策略 | 核心 |
| 7 | **plugin-monitor** | 系统监控 + 告警 (CPU/MEM/Disk/Network) | 核心 |
| 8 | **plugin-app-store** | Docker 应用商店，兼容 Runtipi 模板 | plugin-docker |
| 9 | **plugin-template-market** | 项目模板市场 (Next.js/Nuxt/Laravel/Gin 等) | plugin-project-deploy |

#### P2 — 可以有

| # | 插件名 | 功能描述 | 依赖 |
|---|--------|---------|------|
| 10 | **plugin-mcp-server** | 对接 Vibe Coding IDE 的 MCP Server | plugin-project-deploy |
| 11 | **plugin-cron** | 定时任务管理 | 核心 |
| 12 | **plugin-firewall** | 防火墙管理 (iptables/nftables) | 核心 |

---

## 4. 非功能需求

### 4.1 性能要求

| 指标 | 要求 |
|------|------|
| 最低内存 | 2GB (Lite 运行 < 100MB，Pro 全插件 < 512MB) |
| 启动时间 | 面板本身 < 3 秒 |
| API 响应 | P99 < 200ms (常规 CRUD) |
| 并发用户 | 支持 20+ 同时在线 |
| 数据库 | SQLite 单机，未来可选 PostgreSQL |

### 4.2 安全要求

| 项目 | 要求 |
|------|------|
| 身份认证 | JWT + TOTP 2FA (已实现) |
| 插件隔离 | 插件运行在受限环境，不能直接访问核心数据库 |
| API 安全 | 所有插件 API 走统一鉴权中间件 |
| 敏感数据 | LLM API Key 加密存储 (AES-GCM) |
| 项目部署 | 构建在隔离环境执行，限制网络和文件系统访问 |
| Docker | 面板不以 root 运行 Docker daemon |

### 4.3 兼容性要求

| 维度 | 要求 |
|------|------|
| OS | Ubuntu 22.04+, Debian 12+, CentOS 9+, AlmaLinux 9+, Fedora 42+ |
| 架构 | amd64, arm64 |
| 浏览器 | Chrome 90+, Firefox 90+, Safari 15+, Edge 90+ |
| Docker | Docker Engine 20.10+, Docker Compose v2 |
| Node.js | 18/20/22 LTS (项目部署) |
| Go | 1.21+ (项目部署) |
| PHP | 8.1+ (项目部署) |

### 4.4 部署方式

| 方式 | 说明 |
|------|------|
| 一键安装脚本 | `curl -fsSL https://web.casa/install.sh | bash` (已有，需扩展) |
| Docker Compose | 官方镜像 (已有，需扩展) |
| 单二进制 | 下载即用 (已有) |
| 插件安装 | 面板内一键安装/卸载，热加载无需重启 |

---

## 5. AI 集成需求详细分析

### 5.1 面板内 AI 助手 (plugin-ai-assistant)

```
用户配置:
  ├── LLM Base URL (如 https://api.openai.com/v1)
  ├── API Key (AES-GCM 加密存储)
  ├── Model Name (如 gpt-4o, claude-sonnet-4-20250514)
  └── 可选: 温度、最大 Token 等参数

功能:
  ├── 部署错误诊断 — 读取构建日志/运行日志，给出修复建议
  ├── Text-to-Template — "我要一个带 Redis 的 N8N" → 生成 Docker Compose
  ├── 配置建议 — 根据项目类型推荐最优配置
  ├── 每步向导旁的「问 AI」按钮
  └── 聊天界面 — 面板右下角可展开的 AI 对话框
```

### 5.2 日志系统设计 (为 AI 可读优化)

```
日志规范:
  ├── 结构化 JSON 日志 (非纯文本)
  ├── 统一字段: timestamp, level, module, action, message, context
  ├── 构建日志: 标记阶段 (clone/install/build/start)
  ├── 运行日志: 标记来源 (stdout/stderr)
  ├── 最近 N 行快速获取 API
  └── 日志搜索 + 过滤 API

AI 消费方式:
  ├── GET /api/logs/recent?lines=100&project=xxx — AI 快速获取最近日志
  ├── GET /api/logs/errors?since=1h — 获取最近 1 小时错误
  └── 日志内容作为 AI 对话的 context 自动注入
```

### 5.3 MCP Server 对接 (plugin-mcp-server)

```
暴露给 IDE 的工具:
  ├── deploy_project(repo, domain, framework) — 部署项目
  ├── list_projects() — 列出所有项目
  ├── get_project_status(id) — 获取项目状态和日志
  ├── restart_project(id) — 重启项目
  ├── get_deploy_logs(id, lines) — 获取部署日志
  ├── create_database(type, name) — 创建数据库
  ├── add_domain(domain, upstream) — 添加域名反代
  └── ask_ai(question, context) — 向面板 AI 提问
```

---

## 6. 插件系统需求

### 6.1 插件生命周期

```
发现 → 安装 → 启用 → 运行 → 禁用 → 卸载
  │       │       │       │       │       │
  │       │       │       │       │       └── 清理数据(可选保留)
  │       │       │       │       └── 停止服务、移除路由
  │       │       │       └── 注册路由、启动后台任务
  │       │       └── 数据库迁移、初始化配置
  │       └── 下载、校验签名、解压
  └── 从市场获取列表、检查兼容性
```

### 6.2 插件能力

每个插件可以:
- 注册 API 路由 (挂载到 `/api/plugins/{plugin-name}/`)
- 注册前端页面 (侧边栏菜单项 + 页面组件)
- 注册系统事件监听器 (如 host.created, deploy.failed)
- 声明依赖的其他插件
- 拥有独立的数据表 (自动迁移)
- 拥有独立的配置项

### 6.3 插件分发格式

```
plugin-docker/
  ├── manifest.json        # 元数据 (名称、版本、依赖、权限)
  ├── backend.so           # Go 插件 (编译后的 shared library)
  ├── frontend/            # 前端资源 (构建后的 JS/CSS)
  │   ├── index.js
  │   └── index.css
  ├── migrations/          # 数据库迁移文件
  └── README.md
```

---

## 7. Lite vs Pro 功能对比矩阵

| 功能 | Lite | Pro |
|------|------|-----|
| Caddy 反向代理管理 | ✅ | ✅ |
| SSL/TLS 自动化 | ✅ | ✅ |
| 多用户 + 审计 | ✅ | ✅ |
| 模板 + 分组 + 标签 | ✅ | ✅ |
| DNS Provider | ✅ | ✅ |
| Caddyfile 编辑器 | ✅ | ✅ |
| 国际化 (中/英) | ✅ | ✅ |
| 插件市场 | ✅ (安装入口) | ✅ (全部插件) |
| 项目源码部署 | — | ✅ |
| Docker 管理 | — | ✅ |
| AI 助手 | — | ✅ |
| 数据库管理 | — | ✅ |
| 文件管理器/终端 | — | ✅ |
| 备份 (Kopia) | — | ✅ |
| 系统监控 & 告警 | — | ✅ |
| 应用商店 | — | ✅ |
| 项目模板市场 | — | ✅ |
| MCP Server | — | ✅ |

---

## 8. 约束与风险

### 8.1 技术约束

| 约束 | 影响 | 应对 |
|------|------|------|
| SQLite 单机 | 不支持集群部署 | 面板定位为单机管理，足够 |
| Go Plugin 机制 | Go 插件编译需同版本 Go | 考虑 RPC/进程隔离方案替代 |
| 2GB 内存限制 | 不能运行重量级中间件 | 插件按需加载，Docker 应用需用户自行评估资源 |
| Caddy 生态 | Caddy 市场份额小于 Nginx | Caddy 配置更现代、自动 HTTPS 是核心优势 |

### 8.2 风险

| 风险 | 级别 | 缓解 |
|------|------|------|
| 插件系统复杂度高 | 🔴 高 | 先实现最小可用插件机制，迭代优化 |
| Go plugin 跨版本兼容性差 | 🔴 高 | 评估 HashiCorp go-plugin (gRPC) 方案 |
| AI 功能依赖外部 LLM | 🟡 中 | AI 是增强而非必须，无 AI 仍可正常使用 |
| Docker 安全性 | 🟡 中 | 不给面板 root 权限，用 Docker socket proxy |
| 前端插件动态加载 | 🟡 中 | 使用 Module Federation 或 iframe 隔离 |
