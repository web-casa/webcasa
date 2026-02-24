# Memory — WebCasa 开发记忆

> **目的**: 本文件为跨 AI IDE 的开发记忆文件。无论你是 Claude Code、Gemini Code、Cursor、Windsurf 还是其他 AI 编码工具，请在开始工作前阅读本文件以获取历史上下文。
>
> **Token 优化**: 本文件设计为紧凑格式，避免冗余。详细信息请按需查阅引用的文档。

---

## 📍 项目当前状态

- **版本**: `0.7.0` (定义在 `/VERSION`，唯一真相源)
- **阶段**: Phase 5 完成（安全增强与主题系统）
- **构建状态**: ✅ `go build` + `npm run build` 通过
- **开发方式**: AI Vibe Coding (Gemini 2.5 Pro + Antigravity Agent)
- **语言**: 项目文档和注释以中文为主，代码以英文为主

## 📖 文档索引（按需读取，避免浪费 token）

| 文档 | 内容 | 何时读取 |
|------|------|----------|
| `agents.md` | 架构概览、目录结构、模型定义、API 表、编码约定 | **修改代码前必读** |
| `stack.md` | 技术栈详情、分层架构、数据流、原子写入流程 | 需要理解底层机制时 |
| `README.md` | 功能列表、安装说明、用户文档 | 需要了解产品功能时 |
| `changelog.md` | 版本变更记录（按语义化版本） | 需要了解版本历史时 |
| `notice.md` | Vibe Coding 调试故事和经验教训 | 好奇或遇到类似问题时 |

## 🏗️ 架构一句话

Go(Gin) + React19(Vite7) 单二进制面板，管理 Caddy 反代。DB → `RenderCaddyfile()` → 原子写入 → `caddy reload`。详见 `agents.md`。

## 📅 开发时间线

```
2025-12    Phase 0    项目初始化，基础 Host CRUD + Caddyfile 生成
2026-02    Phase 1    核心完善：CI/CD、Caddy 进程管理、安装脚本、TLS bug 修复
2026-02    Phase 2    核心缺失：Redirect Host、自定义证书、HTTP Basic Auth
2026-02    Phase 3    体验提升：Dashboard、多用户、审计日志、custom_directives
2026-02-22 Phase 4.2  Host 选项：压缩/CORS/安全头/错误页/缓存
2026-02-22 Phase 4.3  新 Host 类型：static（静态网站）、php（PHP/FastCGI）
2026-02-22 Phase 4.4  Caddyfile 编辑器：CodeMirror 6 + format/validate/save
2026-02-22 Phase 4.1  DNS Provider：Cloudflare/阿里云/腾讯云/Route53 + TLS 5 模式
2026-02-23 v0.3.1     统一版本管理机制、文档全面更新
2026-02-23 v0.4.0     安全增强（登录限速/滑块验证）、主题系统重构（CSS变量）、证书管理
2026-02-23 v0.4.1     修复前端白屏、安装脚本兼容性、IP 检测多方案 fallback
2026-02-23 v0.4.2     修复 Settings 页面组件引入缺失及字体加载语法错误
2026-02-23 v0.4.3     修复登录页面滑块验证码坐标系计算不同步导致的验证失败；新增脚本 GLIBC 版本检查，提前拦截暂不支持的低版本 Linux 预编译下载
2026-02-23 v0.4.4     进一步加固和修复 install.sh 中关于 GLIBC 版本检测由于环境差异导致输出拼接所产生的解析 bug
2026-02-23 v0.5.0     滑块验证码 → Altcha PoW 替换；新增 altcha.go；移除 ChallengeStore
2026-02-23 v0.5.1     修复 Altcha Widget 在 HTTP 下 Web Crypto 不可用；用纯 JS SHA-256 替代 Widget
```

## ⚠️ 重要约定（必须遵守）

### 版本管理
- 版本号**只改** `VERSION` 文件 + `web/package.json`
- Go 通过 `-ldflags "-X main.Version=..."` 注入，**不要**在 Go 代码中硬编码版本
- 前端通过 Dashboard API 动态获取版本，**不要**在 JSX 中硬编码
- install.sh 运行时自动从 VERSION 文件或 GitHub API 读取

### 数据模型
- 布尔字段一律用 `*bool` 指针（GORM 零值陷阱，详见 `notice.md`）
- Host 有 4 种 `host_type`: proxy / redirect / static / php
- Host 有 5 种 `tls_mode`: auto / dns / wildcard / custom / off
- DNS Provider config 为 JSON 字符串存储

### Caddyfile 渲染
- 使用 `strings.Builder` 程序化构建，不用模板
- `RenderCaddyfile(hosts, cfg, dnsProviders)` 签名接收 3 个参数
- 每种 Host 类型有独立渲染函数：`renderProxyBlock` / `renderRedirect` / `renderStaticHost` / `renderPHPHost`
- TLS 渲染：`renderDnsTLS(b, provider)` 处理 4 种 DNS Provider

### 前端
- 组件库: Radix UI Themes (不是 shadcn)
- 状态管理: Zustand (不是 Redux)
- 主题: 深色模式，主色调 emerald (#10b981)
- 新页面需要: 1) 页面组件 → 2) App.jsx 路由 → 3) Layout.jsx 侧边栏

### 审计日志
- 所有 Host/User/Caddy/DnsProvider 的增删改操作都记录审计日志
- 调用 `h.audit(c, action, detail)` 方法

## 🔧 常用开发命令

```bash
# 验证构建
cd /home/ivmm/webcasa && go build -o /dev/null .
cd /home/ivmm/webcasa/web && npm run build

# 带版本号构建
VERSION=$(cat VERSION | tr -d '[:space:]') && go build -ldflags="-s -w -X main.Version=${VERSION}" -o webcasa .

# 全局搜索（修改散布值前必做）
grep -rn "搜索词" --include='*.go' --include='*.js' --include='*.jsx' --include='*.sh' .
```

## 🐛 已知问题 / 待办

- [ ] Dashboard 缺少流量统计（需 Caddy log 解析）
- [ ] 缺少插件系统
- [ ] 前端 JS bundle ~895KB，可做 code-split
- [x] ~~滑块验证码经常失败~~ → 已替换为 Altcha PoW (v0.5.0)
- [x] ~~Altcha Widget 在 HTTP 下不工作~~ → 已用纯 JS SHA-256 修复 (v0.5.1)

## 💬 用户偏好

- 使用**中文**交流（包括 Implementation Plan、结果、过程说明等）
- 新开发计划开始前先**确认问题**再动手
- 写代码前先**确认是否开始**
- 偏好一次性完成批量修改，而非反复小改
