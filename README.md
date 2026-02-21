<div align="center">

# ⚡ CaddyPanel

**开源的 Caddy 反向代理 Web 管理面板**

类似 Nginx Proxy Manager，但使用 [Caddy](https://caddyserver.com) 作为反代内核。

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev)
[![Caddy](https://img.shields.io/badge/Caddy-2.x-22B638?logo=caddy&logoColor=white)](https://caddyserver.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

</div>

---

## ✨ 功能特性

- 🌐 **Host 管理** — 通过 UI 创建/编辑/删除 域名 → 上游 反向代理映射
- 🔒 **自动 HTTPS** — Let's Encrypt 证书自动申请/续签，一键开关 HTTP→HTTPS 重定向
- 📋 **日志查看** — 实时查看/搜索/下载 Caddy 访问日志和错误日志
- 🔄 **进程控制** — 一键启停/重载 Caddy，零停机 Graceful Reload
- 🔑 **面板认证** — JWT 登录保护，首次启动引导创建管理员
- 📦 **导入/导出** — 一键备份和恢复所有配置（JSON 格式）
- ⚖️ **负载均衡** — 支持多上游服务器 + Round Robin
- 🔌 **WebSocket** — 原生 WebSocket 代理支持
- 📝 **自定义 Header** — 请求/响应 Header 重写
- 🛡️ **IP 访问控制** — IP 白名单/黑名单（CIDR 格式）
- 💾 **SQLite 持久化** — 零依赖嵌入式数据库，重启数据不丢失

## 📸 截图

> 面板截图待补充

## 🚀 快速安装

### 一键安装（推荐）

支持 Ubuntu 20+、Debian 11+、CentOS Stream 8+、AlmaLinux、Rocky Linux、Fedora、openAnolis、Alibaba Cloud Linux、openEuler、openCloudOS、银河麒麟 等主流 Linux 发行版。

```bash
# 下载并运行安装脚本
curl -fsSL https://raw.githubusercontent.com/caddypanel/caddypanel/main/install.sh | sudo bash
```

安装完成后访问 `http://YOUR_IP:8080`，首次访问会引导创建管理员账户。

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
git clone https://github.com/caddypanel/caddypanel.git
cd caddypanel
docker compose up -d
```

面板地址：`http://localhost:8080`

### 手动编译

**前置要求：** Go 1.22+、Node.js 20+、GCC

```bash
git clone https://github.com/caddypanel/caddypanel.git
cd caddypanel

# 编译前端 + 后端
make build

# 运行
./caddypanel
```

## 🛠️ 开发指南

```bash
# 后端（终端 1）
go run .
# → http://localhost:8080

# 前端（终端 2）
cd web && npm install && npm run dev
# → http://localhost:5173（自动代理 API 到后端）
```

## 📡 API 速览

所有接口需要 JWT Token（`Authorization: Bearer <token>`），登录和初始设置接口除外。

```bash
# 创建管理员（首次）
curl -X POST http://localhost:8080/api/auth/setup \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"yourpassword"}'

# 登录
curl -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"yourpassword"}'

# 创建反向代理 Host
curl -X POST http://localhost:8080/api/hosts \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "domain": "app.example.com",
    "tls_enabled": true,
    "upstreams": [{"address": "localhost:3000"}]
  }'
```

完整 API 列表请参考 [stack.md](stack.md)。

## 📂 目录结构

```
caddypanel/
├── main.go                  # 入口
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

所有配置通过环境变量设置，安装脚本会自动生成 `/etc/caddypanel/caddypanel.env`：

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `CADDYPANEL_PORT` | `8080` | 面板端口 |
| `CADDYPANEL_DATA_DIR` | `./data` | 数据目录 |
| `CADDYPANEL_DB_PATH` | `data/caddypanel.db` | 数据库路径 |
| `CADDYPANEL_JWT_SECRET` | — | JWT 签名密钥（必须修改） |
| `CADDYPANEL_CADDY_BIN` | `caddy` | Caddy 二进制路径 |
| `CADDYPANEL_CADDYFILE_PATH` | `data/Caddyfile` | 生成的 Caddyfile 路径 |
| `CADDYPANEL_LOG_DIR` | `data/logs` | 日志目录 |

## 🗺️ 路线图

- [x] Host CRUD + Caddyfile 自动生成
- [x] 自动 HTTPS（Let's Encrypt）
- [x] 日志查看/下载
- [x] 面板认证（JWT）
- [x] 配置导入/导出
- [x] 一键安装脚本
- [ ] DNS Challenge 支持
- [ ] 多用户与权限管理
- [ ] 仪表盘流量统计
- [ ] 插件系统

## 📄 License

MIT License
