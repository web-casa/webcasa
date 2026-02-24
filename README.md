<div align="center">

# ⚡ WebCasa

**开源的 Caddy 反向代理 Web 管理面板**

类似 Nginx Proxy Manager，但使用 [Caddy](https://caddyserver.com) 作为反代内核。

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev)
[![Caddy](https://img.shields.io/badge/Caddy-2.x-22B638?logo=caddy&logoColor=white)](https://caddyserver.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

</div>

---

## ✨ 功能特性

### 站点管理
- 🌐 **多类型 Host** — 反向代理、301/302 跳转、静态网站、PHP/FastCGI 站点
- ⚖️ **负载均衡** — 支持多上游服务器 + Round Robin
- 🔌 **WebSocket** — 原生 WebSocket 代理支持
- 📝 **自定义 Header** — 请求/响应 Header 重写
- 🛡️ **IP 访问控制** — IP 白名单/黑名单（CIDR 格式）
- 🔐 **HTTP Basic Auth** — bcrypt 加密的 HTTP 认证保护
- 📦 **导入/导出** — 一键备份和恢复所有配置（JSON 格式）

### 证书管理
- 🔒 **自动 HTTPS** — Let's Encrypt 证书自动申请/续签
- 🌍 **DNS Challenge** — 支持 Cloudflare、阿里云、腾讯云、Route53 DNS 验证
- 🃏 **通配符证书** — 通过 DNS Provider 申请 `*.domain.com` 证书
- 📜 **自定义证书** — 上传自有 SSL 证书

### 性能和安全
- 🗜️ **响应压缩** — Gzip + Zstd 自动压缩
- 🌐 **CORS 跨域** — 一键配置跨域资源共享
- 🔰 **安全响应头** — HSTS / X-Frame-Options / CSP 一键开启
- 🚨 **自定义错误页** — 404/502/503 错误页面定制

### 编辑器和管理
- ✏️ **Caddyfile 编辑器** — CodeMirror 6 在线编辑器，支持格式化/语法验证/保存
- 👥 **多用户管理** — 用户 CRUD + admin/viewer 角色
- 📋 **审计日志** — 所有操作记录，追踪 IP 和操作详情
- 📊 **Dashboard** — Host 分类统计、TLS 状态、系统信息

### 系统
- 🔄 **进程控制** — 一键启停/重载 Caddy，零停机 Graceful Reload
- 📋 **日志查看** — 实时查看/搜索/下载 Caddy 访问日志和错误日志
- 💾 **SQLite 持久化** — 零依赖嵌入式数据库，重启数据不丢失

## 📸 截图

> 面板截图待补充

## 🚀 快速安装

### 一键安装（推荐）

支持 Ubuntu 20+、Debian 11+、CentOS Stream 8+、AlmaLinux、Rocky Linux、Fedora、openAnolis、Alibaba Cloud Linux、openEuler、openCloudOS、银河麒麟 等主流 Linux 发行版。

```bash
# 下载并运行安装脚本（自动安装 Caddy + WebCasa）
curl -fsSL https://raw.githubusercontent.com/web-casa/webcasa/main/install.sh | sudo bash
```

> 脚本会自动安装 Caddy Server、Go、Node.js 等所有依赖，编译面板，配置 systemd 服务并启动。无需手动安装任何组件。

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

## 🛠️ 开发指南

```bash
# 后端（终端 1）
go run .
# → http://localhost:39921

# 前端（终端 2）
cd web && npm install && npm run dev
# → http://localhost:5173（自动代理 API 到后端）
```

## 📂 目录结构

```
webcasa/
├── main.go                  # 入口
├── VERSION                  # 版本号（唯一真相源）
├── internal/
│   ├── config/              # 环境变量配置
│   ├── model/               # 数据模型（GORM）
│   ├── database/            # SQLite 初始化
│   ├── auth/                # JWT 认证
│   ├── caddy/               # Caddy 进程管理 + Caddyfile 渲染
│   ├── service/             # 业务逻辑
│   └── handler/             # HTTP 路由处理
├── web/                     # React 19 前端
│   └── src/pages/           # 页面组件
├── install.sh               # 一键安装脚本
├── Dockerfile               # Docker 构建
└── docker-compose.yml
```

## ⚙️ 配置说明

所有配置通过环境变量设置，安装脚本会自动生成 `/etc/web-casa/webcasa.env`：

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `WEBCASA_PORT` | `39921` | 面板端口 |
| `WEBCASA_DATA_DIR` | `./data` | 数据目录 |
| `WEBCASA_DB_PATH` | `data/webcasa.db` | 数据库路径 |
| `WEBCASA_JWT_SECRET` | — | JWT 签名密钥（必须修改） |
| `WEBCASA_CADDY_BIN` | `caddy` | Caddy 二进制路径 |
| `WEBCASA_CADDYFILE_PATH` | `data/Caddyfile` | 生成的 Caddyfile 路径 |
| `WEBCASA_LOG_DIR` | `data/logs` | 日志目录 |

## 🗺️ 路线图

- [x] Host CRUD + Caddyfile 自动生成
- [x] 自动 HTTPS（Let's Encrypt）
- [x] 日志查看/下载
- [x] 面板认证（JWT）
- [x] 配置导入/导出
- [x] 一键安装脚本
- [x] 多用户与权限管理
- [x] 审计日志
- [x] Dashboard 增强
- [x] DNS Challenge 支持 (Cloudflare / 阿里云 / 腾讯云 / Route53)
- [x] 通配符证书
- [x] 自定义 SSL 证书上传
- [x] 静态网站 / PHP 站点托管
- [x] Caddyfile 在线编辑器
- [x] 响应压缩 / CORS / 安全头 / 错误页
- [ ] 仪表盘流量统计
- [ ] 插件系统
- [ ] 速率限制

## 📄 License

MIT License
