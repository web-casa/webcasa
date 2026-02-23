# Changelog

所有版本变更记录。本项目使用 [Semantic Versioning](https://semver.org/)。

> 📌 本项目采用 AI Vibe Coding 方式开发，使用 Gemini 2.5 Pro + Antigravity Agent 辅助编码。

---

## [0.5.0] - 2026-02-23

### Added
- ✨ 安全增强：使用 Altcha Proof-of-Work (PoW) 机制替换原有的滑块验证码，大幅提升成功率并降低用户操作负担，全过程零额外 Go 模块依赖。

---

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
