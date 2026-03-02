# Web.Casa 功能精简方案

> 版本: 1.0 | 日期: 2026-03-01

---

## 1. 背景

Web.Casa 从 CaddyPanel（Caddy 反向代理管理面板）演进而来，逐步加入了 Docker 管理、项目部署、文件管理、Web 终端、AI 助手等功能。当前导航栏有 **16 个入口**，功能覆盖面广但定位模糊：

- 不是纯粹的 Caddy 管理面板（功能远超反代管理）
- 也不是完整的服务器面板（缺少系统监控、进程管理、定时任务等）
- 功能堆砌导致用户认知负担重，各模块之间融合度不高

需要对现有功能进行精简和重组，使产品定位更清晰、体验更聚焦。

---

## 2. 产品重新定位

**Web.Casa — AI-First 的轻量级服务器管理面板**

核心理念：
- **AI 作为核心交互方式**，传统 UI 作为可视化和确认层
- 用户可以通过 AI 对话完成大部分操作（创建站点、部署项目、管理 Docker）
- 传统 UI 保留给需要可视化浏览、批量操作、精确配置的场景
- 面向 Vibe Coding 时代的开发者和小团队

与传统面板的区别：
| | 宝塔/1Panel | NPM | Coolify | **Web.Casa** |
|---|---|---|---|---|
| 交互方式 | 传统表单 | 传统表单 | 传统表单 | **AI 对话 + 轻量 UI** |
| Web 服务器 | Nginx/Apache | Nginx | Traefik | **Caddy** |
| 部署方式 | 手动/FTP | 无 | Docker/Nixpacks | **Git 源码 + Docker Compose** |
| AI 能力 | 无 | 无 | 无 | **内置 AI 助手 + MCP** |
| 资源占用 | 重 | 轻 | 重 | **轻** |

---

## 3. 导航精简方案

### 3.1 精简前（16 项）

```
Dashboard / Hosts / Caddyfile / DNS Providers / Certificates / Templates
Docker (5个子页面) / Deploy / AI Config / Files / Terminal
Logs / Users / Audit Logs / Plugins / Settings
```

### 3.2 精简后（8 项）

```
Dashboard / Hosts / Caddyfile / Docker / Deploy / Files / Terminal / Settings
```

### 3.3 变化明细

| 导航项 | 状态 | 说明 |
|--------|------|------|
| **Dashboard** | 保留 | 总览页，不变 |
| **Hosts** | 保留 | 站点管理，内嵌 TLS/证书/DNS Provider 配置 |
| **Caddyfile** | 保留 | Caddyfile 编辑器，不变 |
| **Docker** | 精简 | 仅保留 Compose Stacks，砍掉容器/镜像/网络/卷 |
| **Deploy** | 保留 | 项目部署，不变 |
| **Files** | 保留 | 文件管理 + 代码编辑，不变 |
| **Terminal** | 保留 | Web 终端，不变 |
| **Settings** | 扩展 | 合并多个管理页面（见下文） |
| ~~DNS Providers~~ | 合并 | → Host 编辑页的 TLS 配置区域 |
| ~~Certificates~~ | 合并 | → Host 编辑页的 TLS 配置区域 |
| ~~Templates~~ | 合并 | → 创建 Host 时的预设选择弹窗 |
| ~~AI Config~~ | 合并 | → Settings 的 AI Tab |
| ~~Plugins~~ | 合并 | → Settings 的 Plugins Tab |
| ~~Audit Logs~~ | 合并 | → Settings 的 Logs Tab（与 Caddy 日志用 Tab 切换） |
| ~~Logs~~ | 合并 | → Settings 的 Logs Tab |
| ~~Users~~ | 合并 | → Settings 的 Users Tab |

---

## 4. 详细设计

### 4.1 Docker 精简 — 只保留 Compose Stacks

**删除的页面（4 个）：**
- `DockerContainers.jsx` — 容器管理
- `DockerImages.jsx` — 镜像管理
- `DockerNetworks.jsx` — 网络管理
- `DockerVolumes.jsx` — 卷管理

**保留：**
- `DockerOverview.jsx` → 重构为纯 Compose Stacks 管理页面

**理由：**
- 独立容器/镜像/网络/卷的管理场景很少，开发者习惯用命令行或 Portainer
- Compose Stacks 是面板用户的主要 Docker 使用方式（AI 生成 Compose → 一键部署）
- 减少 4 个页面，Docker 导航从 5 个子项变为 1 个

**后端 API 处理：**
- 容器/镜像/网络/卷的 API endpoint 保留不删（MCP 和 AI 可能用到）
- 只删除前端页面

### 4.2 Host 编辑 — 内嵌证书 + DNS Provider

当前证书和 DNS Provider 是独立页面，但它们的使用场景 100% 关联到 Host 配置。

**合并方案：**
- Host 编辑页的 TLS 区域增加：
  - 「上传证书」按钮 — 直接在 Host 编辑弹窗内上传
  - 「DNS 验证」下拉选择 — 选择已配置的 DNS Provider
  - 「管理 DNS Providers」链接 — 打开 Settings 或弹窗管理

- DNS Providers 管理移到 Settings 的专属 Tab
- Certificates 管理也移到 Settings 的专属 Tab

### 4.3 Templates — 融入创建 Host 流程

当前模板页面使用率低，且模板的核心用途就是加速创建 Host。

**合并方案：**
- 创建 Host 弹窗顶部增加「从模板创建」选项
- 展示预设模板（WordPress 反代、静态站点、Node.js 等）和自定义模板
- Host 详情页保留「保存为模板」按钮
- 删除独立的 Templates 页面和导航项

### 4.4 Settings 页 — 合并后的 Tab 结构

```
Settings
├── General     — Caddy 控制（启停/重载）、密码修改、2FA、外观设置
├── Users       — 用户管理（CRUD、角色）
├── Logs        — Tab 切换：Caddy 访问日志 / 审计日志
├── AI          — API Base URL、Model、API Key、连接测试
├── DNS         — DNS Provider 管理（Cloudflare、AliDNS 等）
├── Certificates — 证书上传和管理
└── Plugins     — 插件启用/禁用开关列表
```

### 4.5 AI 升级路径

当前 AI 是独立的聊天窗口 + 配置页面，与面板其他功能割裂。

**短期（本次精简）：**
- AI Config 合并到 Settings
- 浮动聊天窗保留

**中期（后续迭代）：**
- AI 能直接执行操作（创建站点、部署项目、管理 Docker）
- 各页面增加上下文感知的「Ask AI」按钮
- AI 生成 Docker Compose 后可一键部署到 Stacks

**长期：**
- MCP Server 对接 Vibe Coding IDE
- AI 作为面板的主要操控入口

---

## 5. 删除/精简清单

### 5.1 删除的前端页面（10 个）

| 文件 | 原导航位置 | 去向 |
|------|-----------|------|
| `DnsProviders.jsx` | DNS Providers | Settings > DNS Tab |
| `Certificates.jsx` | Certificates | Settings > Certificates Tab |
| `Templates.jsx` | Templates | Host 创建弹窗的模板选择 |
| `DockerContainers.jsx` | Docker > Containers | 删除 |
| `DockerImages.jsx` | Docker > Images | 删除 |
| `DockerNetworks.jsx` | Docker > Networks | 删除 |
| `DockerVolumes.jsx` | Docker > Volumes | 删除 |
| `AIConfig.jsx` | AI Config | Settings > AI Tab |
| `Plugins.jsx` | Plugins | Settings > Plugins Tab |
| `AuditLogs.jsx` | Audit Logs | Settings > Logs Tab |

### 5.2 修改的前端页面

| 文件 | 修改内容 |
|------|---------|
| `Layout.jsx` | 导航从 16 项减到 8 项 |
| `App.jsx` | 删除多余路由，Settings 增加子路由 |
| `Settings.jsx` | 重构为 Tab 布局，整合 Users/Logs/AI/DNS/Certs/Plugins |
| `DockerOverview.jsx` | 改为纯 Compose Stacks 页面，移除容器/镜像/网络/卷入口 |
| `HostList.jsx` | 创建 Host 弹窗增加「从模板创建」入口 |
| `Logs.jsx` | 合并到 Settings，增加审计日志 Tab |
| `Users.jsx` | 合并到 Settings |

### 5.3 保留不动的后端 API

所有后端 API endpoint 保留不变。理由：
- API 对前端不可见不影响用户体验
- MCP Server 和 AI 助手后续需要调用这些 API
- 避免删除 API 导致的回归风险

### 5.4 保留不动的前端页面

| 文件 | 说明 |
|------|------|
| `Login.jsx` | 登录页 |
| `Dashboard.jsx` | 仪表盘 |
| `HostList.jsx` | 站点管理 |
| `CaddyfileEditor.jsx` | Caddyfile 编辑器 |
| `FileManager.jsx` | 文件管理器 |
| `FileEditor.jsx` | 代码编辑器 |
| `WebTerminal.jsx` | Web 终端 |
| `ProjectList.jsx` | 项目列表 |
| `ProjectCreate.jsx` | 创建项目 |
| `ProjectDetail.jsx` | 项目详情 |
| `AIChatWidget.jsx` | AI 浮动聊天窗 |

---

## 6. 精简前后对比

### 导航结构

```
精简前 (16项):                    精简后 (8项):
├── Dashboard                     ├── Dashboard
├── Hosts                         ├── Hosts（内嵌证书+DNS）
├── Caddyfile                     ├── Caddyfile
├── DNS Providers        ✕        ├── Docker（仅 Compose Stacks）
├── Certificates         ✕        ├── Deploy
├── Templates            ✕        ├── Files
├── Docker ──┬── Overview         ├── Terminal
│            ├── Containers  ✕    └── Settings
│            ├── Images      ✕        ├── General
│            ├── Networks    ✕        ├── Users
│            └── Volumes     ✕        ├── Logs（Caddy + 审计）
├── Deploy                            ├── AI
├── AI Config            ✕            ├── DNS Providers
├── Files                             ├── Certificates
├── Terminal                          └── Plugins
├── Logs                 → Settings
├── Users                → Settings
├── Audit Logs           → Settings
├── Plugins              → Settings
└── Settings
```

### 数据

| 指标 | 精简前 | 精简后 |
|------|--------|--------|
| 导航项 | 16 | 8 |
| 独立页面文件 | 24 | 14 |
| Docker 子页面 | 5 | 1 |
| Settings Tab | 1 | 7 |

---

## 7. 不在本次范围内（后续迭代）

以下功能在原始开发计划（03-development-tasks.md）中但尚未实现，本次精简不影响其规划：

| 原计划 Phase | 功能 | 状态 | 说明 |
|-------------|------|------|------|
| Phase 5 | 数据库管理插件 | 未开始 | 保持原规划 |
| Phase 6 | 备份 + 系统监控 | 未开始 | 保持原规划 |
| Phase 7 | 应用商店 + 模板市场 | 未开始 | 保持原规划 |
| Phase 8 | MCP Server | 未开始 | 保持原规划，是 AI-First 的关键 |

---

## 8. 实施步骤

| 步骤 | 内容 | 涉及文件 |
|------|------|---------|
| 1 | Settings 页重构为 Tab 布局 | `Settings.jsx` |
| 2 | 迁移 Users 到 Settings Tab | `Users.jsx` → `Settings.jsx` |
| 3 | 迁移 Logs + AuditLogs 到 Settings Tab | `Logs.jsx`, `AuditLogs.jsx` → `Settings.jsx` |
| 4 | 迁移 AI Config 到 Settings Tab | `AIConfig.jsx` → `Settings.jsx` |
| 5 | 迁移 DNS Providers 到 Settings Tab | `DnsProviders.jsx` → `Settings.jsx` |
| 6 | 迁移 Certificates 到 Settings Tab | `Certificates.jsx` → `Settings.jsx` |
| 7 | 迁移 Plugins 到 Settings Tab | `Plugins.jsx` → `Settings.jsx` |
| 8 | Docker 页面精简为 Compose Stacks | `DockerOverview.jsx` |
| 9 | Host 创建弹窗增加模板选择 | `HostList.jsx` |
| 10 | 删除废弃的独立页面文件 | 清理 10 个 .jsx 文件 |
| 11 | 更新 App.jsx 路由 | `App.jsx` |
| 12 | 更新 Layout.jsx 导航 | `Layout.jsx` |
| 13 | 更新 i18n 翻译文件 | `en.json`, `zh.json` |
| 14 | 本地构建验证 + Docker 集成测试 | `npm run build`, `test-local.sh` |
