[English](./README.md) | [简体中文](./README_ZH.md)

<div align="center">

# WebCasa

**为 Vibe Coding 时代设计的轻量服务器控制面板**

基于 [Caddy](https://caddyserver.com) 的反向代理管理 + 插件扩展。Lite 模式最低 256MB、推荐 512MB+；Full 模式最低 1GB、推荐 2GB+。

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev)
[![Caddy](https://img.shields.io/badge/Caddy-2.x-22B638?logo=caddy&logoColor=white)](https://caddyserver.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

</div>

---

## 产品形态

- **Lite** — Caddy 反向代理管理面板（开箱即用）
- **Full** — Lite + 12 个插件扩展（Docker / 项目部署 / AI 助手 / 数据库 / 文件管理 / 备份 / 监控 / 应用商店 / MCP / 防火墙 / 定时任务 / PHP）
- **Full** 指开启全部插件功能的完整形态
- 用户可按需启用插件，从 Lite 渐进升级到 Full

## 功能特性

### 站点管理（核心）
- **多类型 Host** — 反向代理、301/302 跳转、静态网站、PHP/FastCGI 站点
- **负载均衡** — 多上游服务器 + Round Robin
- **WebSocket** — 原生 WebSocket 代理支持
- **自定义 Header** — 请求/响应 Header 重写
- **IP 访问控制** — IP 白名单/黑名单（CIDR 格式）
- **HTTP Basic Auth** — bcrypt 加密的 HTTP 认证保护
- **导入/导出** — 一键备份和恢复所有配置（JSON 格式）
- **站点模板** — 6 个预设模板 + 自定义模板 + 导入/导出

### 证书管理
- **自动 HTTPS** — Let's Encrypt 证书自动申请/续签
- **DNS Challenge** — 支持 Cloudflare、阿里云、腾讯云、Route53 DNS 验证
- **通配符证书** — 通过 DNS Provider 申请 `*.domain.com` 证书
- **自定义证书** — 上传自有 SSL 证书

### 插件生态

| 插件 | 功能 |
|------|------|
| **Docker** | Docker & Compose 管理，容器/镜像/网络/卷，Daemon 配置 |
| **项目部署** | Git 源码部署（Node.js/Go/PHP/Python），自动检测框架，零停机部署 |
| **AI 助手** | 67+ 工具的 AI 聊天，NLOps 自然语言运维，故障自愈，每日巡检 |
| **数据库** | MySQL / PostgreSQL / MariaDB / Redis 实例管理，SQL 浏览器 |
| **文件管理** | 文件浏览器、在线编辑器、Web 终端（PTY） |
| **备份** | 通过 Kopia 备份面板数据 / Docker 卷 / 数据库（本地/S3/WebDAV/SFTP） |
| **系统监控** | 实时系统指标、历史图表、阈值告警 |
| **应用商店** | 一键 Docker 应用安装、项目模板市场 |
| **MCP 服务** | MCP 协议服务，用于 AI IDE 集成（Cursor / Windsurf / Claude Code） |
| **防火墙** | firewalld 规则管理 |
| **定时任务** | 通用定时任务管理（Cron 表达式 + Shell 命令） |
| **PHP** | PHP-FPM 和 FrankenPHP 运行时管理，一键创建 PHP 站点 |

### 性能和安全
- **响应压缩** — Gzip + Zstd 自动压缩
- **CORS 跨域** — 一键配置跨域资源共享
- **安全响应头** — HSTS / X-Frame-Options / CSP 一键开启
- **2FA** — TOTP 二步验证 + 恢复码
- **ALTCHA PoW** — 登录/安装防暴力破解
- **审计日志** — 所有操作记录

### 系统
- **进程控制** — 一键启停/重载 Caddy，零停机 Graceful Reload
- **Caddyfile 编辑器** — CodeMirror 6 在线编辑器
- **多用户管理** — admin / viewer 角色
- **多语言** — 中文 / English

## 快速安装

### 一键安装（推荐）

支持 RHEL 9/10 系列发行版：CentOS Stream 9/10、AlmaLinux 9/10、Rocky Linux 9/10、Fedora、openAnolis 23、Alibaba Cloud Linux 3、openEuler 22.03+、银河麒麟 V10 等。

```bash
curl -fsSL https://raw.githubusercontent.com/web-casa/webcasa/main/install.sh | sudo bash
```

安装完成后访问 `http://YOUR_IP:39921`，首次访问会引导创建管理员账户。

**自定义选项：**

```bash
# 指定面板端口
sudo bash install.sh --port 9090

# 已有 Caddy，跳过安装
sudo bash install.sh --no-caddy

# 卸载（保留数据）
sudo bash install.sh --uninstall

# 完全卸载（含数据）
sudo bash install.sh --purge
```

### Docker 安装

```bash
git clone https://github.com/web-casa/webcasa.git
cd webcasa
docker compose up -d
```

面板地址：`http://localhost:39921`

### 手动编译

**前置要求：** Go 1.26+、Node.js 24+、GCC

```bash
git clone https://github.com/web-casa/webcasa.git
cd webcasa

# 编译前端 + 后端
make build

# 运行
./webcasa
```

## 开发指南

```bash
# 后端（终端 1）
go run .
# -> http://localhost:39921

# 前端（终端 2）
cd web && npm install && npm run dev
# -> http://localhost:5173（自动代理 API 到后端）
```

## 目录结构

```text
webcasa/
├── main.go                  # 入口
├── VERSION                  # 版本号
├── internal/
│   ├── config/              # 环境变量配置
│   ├── model/               # 数据模型（GORM）
│   ├── database/            # SQLite 初始化
│   ├── auth/                # JWT + TOTP + ALTCHA
│   ├── crypto/              # AES-GCM 加密
│   ├── caddy/               # Caddy 管理 + Caddyfile 渲染 + 验证
│   ├── service/             # 业务逻辑（Host / Template / TOTP）
│   ├── handler/             # HTTP 路由处理
│   ├── plugin/              # 插件框架（Manager / EventBus / ConfigStore / CoreAPI）
│   └── notify/              # 通知系统
├── plugins/                 # 12 个插件
│   ├── deploy/              # 项目部署
│   ├── docker/              # Docker 管理
│   ├── ai/                  # AI 助手
│   ├── database/            # 数据库管理
│   ├── filemanager/         # 文件管理
│   ├── backup/              # 备份
│   ├── monitoring/          # 系统监控
│   ├── appstore/            # 应用商店
│   ├── mcpserver/           # MCP 服务
│   ├── firewall/            # 防火墙
│   ├── cronjob/             # 定时任务
│   └── php/                 # PHP 管理
├── web/                     # React 19 前端
│   └── src/
│       ├── pages/           # 30+ 页面组件
│       └── locales/         # 中英文翻译
├── install.sh               # 一键安装脚本
├── Dockerfile
└── docker-compose.yml
```

## 配置说明

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `WEBCASA_PORT` | `39921` | 面板端口 |
| `WEBCASA_DATA_DIR` | `./data` | 数据目录 |
| `WEBCASA_DB_PATH` | `data/webcasa.db` | 数据库路径 |
| `WEBCASA_JWT_SECRET` | 自动生成 | JWT 签名密钥 |
| `WEBCASA_CADDY_BIN` | `caddy` | Caddy 二进制路径 |
| `WEBCASA_CADDYFILE_PATH` | `data/Caddyfile` | Caddyfile 路径 |
| `WEBCASA_LOG_DIR` | `data/logs` | 日志目录 |

## 技术栈

- **后端**: Go 1.26+ / Gin / GORM / SQLite
- **前端**: React 19 / Vite 7 / Radix UI / Tailwind CSS / Zustand
- **代理**: Caddy 2.x
- **国际化**: react-i18next（中/英）

## License

MIT License
