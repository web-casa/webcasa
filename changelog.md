# Changelog

所有版本变更记录。本项目使用 [Semantic Versioning](https://semver.org/)。

> 📌 本项目采用 AI Vibe Coding 方式开发，使用 Gemini 2.5 Pro + Antigravity Agent 辅助编码。

---

## [1.0.0] - 2026-03-01

### 🎉 Web.Casa Pro — AI-First 服务器管理面板

从 v0.5.1 (Lite) 到 v1.0.0 (Pro)，10 个插件全部就绪。

### Added — Phase 0: 插件框架
- 🆕 **Plugin System** — Go Interface + 编译时注册，生命周期管理，依赖拓扑排序 (Kahn)
- 🆕 **EventBus** — 内存 pub/sub 事件总线 (含 wildcard)
- 🆕 **ConfigStore** — 插件配置 KV 存储 (scoped prefix)
- 🆕 **CoreAPI** — 核心 API 供插件调用 (CreateHost/ReloadCaddy/Settings)
- 🆕 **Frontend Manifests** — 动态插件路由注入

### Added — Phase 1: Docker 管理
- 🆕 **Docker Compose Stacks** — Stack CRUD + 生命周期 (up/down/restart/pull)
- 🆕 **Container Management** — 容器列表、启停、日志、资源监控
- 🆕 **Image Management** — 镜像列表、拉取、清理、Docker Hub 搜索
- 🆕 **Network & Volume** — 网络/卷 CRUD
- 🆕 **WebSocket 实时日志** — 容器/Stack 日志流式推送

### Added — Phase 2: 项目部署
- 🆕 **Git 集成** — HTTPS/SSH Clone + Deploy Key 支持
- 🆕 **框架自动检测** — 9 种框架预设 (Next.js/Nuxt/Vite/Remix/Express/Go/Laravel/Flask/Django)
- 🆕 **构建流水线** — Clone → Install → Build + 实时日志
- 🆕 **进程管理** — Systemd service 自动生成与管理
- 🆕 **Webhook 自动部署** — Git push 触发自动构建
- 🆕 **版本回滚** — 一键回滚到历史构建

### Added — Phase 3: AI 助手
- 🆕 **智能对话** — OpenAI-compatible LLM SSE 流式对话
- 🆕 **错误诊断** — 日志分析 + AI 根因定位
- 🆕 **Text-to-Compose** — 自然语言生成 Docker Compose
- 🆕 **API Key 加密** — AES-256-GCM 安全存储

### Added — Phase 4: 文件管理 + Web 终端
- 🆕 **Web 文件管理器** — 树形导航 / 上传下载 / 压缩解压
- 🆕 **在线代码编辑器** — CodeMirror 语法高亮
- 🆕 **Web 终端** — xterm.js + PTY 多标签终端

### Added — Phase 5: 数据库管理
- 🆕 **数据库实例管理** — MySQL / PostgreSQL / MariaDB / Redis 一键创建
- 🆕 **Database/User CRUD** — 数据库和用户增删改查
- 🆕 **SQL 查询控制台** — Web 端 SQL 执行 + 结果表格
- 🆕 **SQLite 浏览器** — 本地 .db 文件浏览 + 只读查询

### Added — Phase 6: 备份 + 系统监控
- 🆕 **Kopia 备份** — 支持 Local / S3 / WebDAV / SFTP 多目标
- 🆕 **定时备份** — Cron 调度 + 保留策略
- 🆕 **系统监控** — CPU/内存/磁盘/网络实时图表
- 🆕 **容器监控** — Docker 容器资源使用
- 🆕 **WebSocket 实时指标** — 60 秒采集 + 实时推送
- 🆕 **告警规则** — 阈值告警 + Webhook/Email 通知

### Added — Phase 7: 应用商店
- 🆕 **应用市场** — Runtipi 兼容应用源，一键安装 Docker 应用
- 🆕 **应用管理** — 启停、更新、卸载已安装应用
- 🆕 **多源支持** — 官方源 + 自定义源
- 🆕 **项目模板** — 从模板快速创建部署项目

### Added — Phase 8: MCP Server + API Token
- 🆕 **MCP Server** — Model Context Protocol Streamable HTTP 端点，18 个工具
- 🆕 **API Token** — `wc_` 前缀长期令牌认证 (SHA-256 哈希存储)
- 🆕 **IDE 集成** — Cursor / Windsurf / Claude Code 直接操控面板

### Added — Phase 9: 体验打磨
- 🆕 **导航精简** — 侧边栏从 13 项减至 8 项
- 🆕 **Settings 整合** — 监控 + 备份合并到设置页 Tab
- 🆕 **Dashboard 增强** — Docker/部署项目统计卡片
- 🔧 **i18n 修复** — BackupManager 22+ 个键名修正，硬编码字符串消除

### Security — 安全加固
- 🔒 **插件路由 AdminRouter** — 所有写操作路由需 JWT + admin 角色验证
- 🔒 **API Token RBAC** — RequireAdmin 对 API Token 用户同样校验数据库角色
- 🔒 **MCP Token 权限执行** — 18 个 MCP Tool 全部检查 `scope:action` 权限 (hosts:read, docker:write, etc.)
- 🔒 **静态文件路径遍历防护** — filepath.Abs + strings.HasPrefix 容器化检查
- 🔒 **CORS 同源判断** — 比对完整 scheme + host:port，非仅主机名
- 🔒 **AI 会话隔离** — 对话按 user_id 隔离，防止跨用户访问
- 🔒 **WebSocket Origin 校验** — url.Parse 严格比对 u.Host == r.Host
- 🔒 **App Store Logo 防符号链接** — os.Lstat + EvalSymlinks 防文件泄露

---

## [0.5.1] - 2026-02-23

### Fixed
- 🐛 修复 Altcha Widget 在 HTTP（非 HTTPS）环境下因 Web Crypto API 不可用导致验证永远无法完成的问题
- 🐛 修复安全验证组件文字在浅色主题下看不清的问题

### Changed
- 🔄 使用纯 JavaScript SHA-256 实现替代 Altcha Widget Web Component，移除 `altcha` npm 依赖
- 🔄 自定义 `PowCaptcha` 组件：复选框式 UI、进度百分比显示、主题自适应
- 📦 JS bundle 从 960KB 降至 895KB

---

## [0.5.0] - 2026-02-23

### Added
- ✨ 安全增强：使用 Altcha Proof-of-Work (PoW) 机制替换原有的滑块验证码，大幅提升验证成功率
- ✨ 后端新增 `internal/auth/altcha.go` 封装 PoW 挑战生成与验证（基于 `altcha-lib-go`）

### Removed
- 🗑️ 移除滑块验证码组件 `SliderCaptcha` 及全部相关 CSS（~180 行）
- 🗑️ 移除后端 `ChallengeStore` 滑块验证逻辑（~86 行）

## [0.4.4] - 2026-02-23

### Fixed
- 🐛 重构并修复了 `install.sh` 中的 GLIBC 版本解析逻辑，避免由于异常命令堆叠或环境差异造成多行输出被错误拼接，进而导致把较新系统误判为不受支持。

---

## [0.4.3] - 2026-02-23

### Fixed
- 🐛 修复登录页面滑块验证码控件中指引位置、进度条及目标点由于参考坐标系不统一导致判定永远偏离（无法通过）的问题
- 🐛 移除了验证码滑块上 onMouseLeave 时异常触发状态重置的设计，以免误触导致重来

### Added
- ✨ 安装脚本 `install.sh` 在下载预编译版本之前，增加针对宿主机 GLIBC 版本的要求检查（必须 >= 2.32），对于不符合要求的版本（如 AlmaLinux 8/RHEL 8 等低版本系统）提示使用源码编译而非产生运行期崩溃

---

## [0.4.2] - 2026-02-23

### Fixed
- 🐛 修复 Settings 页面缺少 `TextField` 组件隐式导入导致页面崩溃白屏的问题
- 🐛 修复 `index.css` 由于 `@font-face` 语法错误使用 Google Fonts CSS 地址造成的 OTS 解析报错，改用标准 `@import` 加载字体

---

## [0.4.1] - 2026-02-23

### Fixed
- 🐛 修复 `Tooltip.Provider` 在 Radix UI Themes v3 中为 undefined 导致前端白屏 (React error #130)
- 🐛 修复 `install.sh` 通过 `curl | bash` 管道执行时 `BASH_SOURCE[0]` unbound variable 错误
- 🐛 修复 CI 集成测试中登录步骤未传 slider challenge 参数导致测试失败

### Changed
- 🔄 IP 检测方案优化 — 按优先级 icanhazip.com → api.ip.sb → ifconfig.me → Cloudflare trace 多级 fallback
- 🔄 IP 检测超时从 5s 缩短为 3s（connect-timeout），总超时 5s（max-time）

---

## [0.4.0] - 2026-02-23

### Added — Phase 5: 安全增强与主题系统

- 🆕 **登录限速** — 后端驱动的登录失败速率限制 + 指数退避
- 🆕 **滑块验证码** — 登录页集成滑块 CAPTCHA 防暴力破解
- 🆕 **证书管理增强** — 新增独立证书管理功能
- 🆕 **服务器 IP 设置** — Dashboard 支持查看/配置服务器公网 IP

### Changed

- 🎨 **主题系统重构** — 前端样式全面迁移至 CSS 变量，支持深色/浅色主题动态切换
- 🎨 **侧边栏样式统一** — 引入新 CSS 类统一导航、按钮和文本区域样式
- 🔧 硬编码颜色值替换为 CSS 变量，提升主题一致性

---

## [0.3.1] - 2026-02-23

### Changed
- 统一版本管理机制：创建 `VERSION` 文件作为唯一真相源
- Go 后端通过 `go build -ldflags -X main.Version=` 注入版本号
- `install.sh` 自动从 VERSION 文件或 GitHub API 获取版本号
- 前端右下角版本号改为从 Dashboard API 动态读取
- CI/CD 构建注入版本号

### Fixed
- 修复 `install.sh` 版本号仍为 0.2.1 的遗留问题

---

## [0.3.0] - 2026-02-22

### Added — Phase 4: Caddy 高级特性

#### 批次 1: DNS Provider + TLS 管理
- 🆕 DNS Provider 管理（Cloudflare / 阿里云 DNS / 腾讯云 DNS / Route53）
- 🆕 TLS 模式选择（自动 / DNS Challenge / 通配符 / 自定义证书 / 关闭）
- 🆕 `DnsProvider` 模型 + CRUD API（5 个端点）
- 🆕 `renderDnsTLS()` Caddyfile 渲染函数
- 🆕 前端 DNS Providers 管理页面
- 🆕 HostList TLS 模式选择器 + DNS Provider 下拉选择

#### 批次 2: Host 选项增强
- 🆕 响应压缩（Gzip + Zstd）— `encode gzip zstd`
- 🆕 CORS 跨域配置 — Preflight + 自定义 Origin/Methods/Headers
- 🆕 安全响应头一键开启 — HSTS / X-Frame-Options / CSP / X-XSS-Protection
- 🆕 自定义错误页面 — handle_errors 404/502/503
- 🆕 响应缓存开关 + TTL 配置
- 🆕 Host model 新增 9 个字段
- 🆕 4 个新的 renderer 函数

#### 批次 3: 静态文件和 PHP 托管
- 🆕 `static` 类型 — 静态文件托管（root + file_server + 目录浏览）
- 🆕 `php` 类型 — PHP/FastCGI 站点（php_fastcgi + file_server）
- 🆕 Host 类型从 2 种扩展到 4 种（proxy / redirect / static / php）
- 🆕 前端类型选择器和动态表单

#### 批次 4: Caddyfile 编辑器
- 🆕 CodeMirror 6 在线编辑器（oneDark 主题）
- 🆕 `caddy fmt` 一键格式化
- 🆕 `caddy validate` 语法验证
- 🆕 保存 / 保存并重载 / 重置
- 🆕 3 个新的 API 端点（`/caddy/caddyfile` POST / `/caddy/fmt` / `/caddy/validate`）

---

## [0.2.1] - 2026-02 (Before Phase 4)

### Added — Phase 3: 体验提升
- 🆕 Dashboard 增强 — Host 分类计数 / TLS 状态统计 / 系统信息
- 🆕 多用户管理 — CRUD + admin/viewer 角色
- 🆕 审计日志 — 所有操作记录 + IP 追踪 + 分页查询
- 🆕 自定义 Caddy 指令片段（custom_directives 文本框）

### Added — Phase 2: 核心缺失填补
- 🆕 域名跳转（Redirect Host 类型）— 301/302 跳转
- 🆕 自定义 SSL 证书上传 — cert/key 文件管理
- 🆕 HTTP Basic Auth — bcrypt 密码保护站点

### Added — Phase 1: 核心完善
- 🆕 预编译发布模式（GitHub Actions CI/CD）
- 🆕 Caddy 进程生命周期管理
- 🆕 一键安装脚本（支持 10+ Linux 发行版）
- 🆕 公网反代支持
- 🔧 修复 TLS 开关 `*bool` 空指针 bug
