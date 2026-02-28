# Web.Casa 安装与使用指南

Web.Casa 是一个轻量级服务器管理面板，基于 Go + React 构建，使用 Caddy 作为 Web 服务器，SQLite 作为数据库，开箱即用。

项目地址：https://web.casa

---

## 目录

- [系统要求](#系统要求)
- [安装方式](#安装方式)
  - [方式一：Docker 部署（推荐）](#方式一docker-部署推荐)
  - [方式二：Docker Compose 部署](#方式二docker-compose-部署)
  - [方式三：二进制直接运行](#方式三二进制直接运行)
  - [方式四：从源码编译](#方式四从源码编译)
- [环境变量配置](#环境变量配置)
- [首次使用](#首次使用)
- [功能模块](#功能模块)
  - [反向代理管理](#反向代理管理)
  - [项目部署](#项目部署)
  - [Docker 管理](#docker-管理)
  - [文件管理器](#文件管理器)
  - [Web 终端](#web-终端)
  - [AI 助手](#ai-助手)
- [命令行工具](#命令行工具)
- [HTTPS 与证书](#https-与证书)
- [数据备份与恢复](#数据备份与恢复)
- [安全建议](#安全建议)
- [常见问题](#常见问题)

---

## 系统要求

| 项目 | 要求 |
|------|------|
| 操作系统 | Linux（amd64 / arm64） |
| 内存 | 最低 256MB，建议 512MB+ |
| 磁盘 | 最低 200MB（不含项目数据） |
| 网络 | 需要开放 80、443 端口（用于 HTTPS） |

Docker 部署时无需额外依赖。二进制部署需要自行安装 Caddy。

---

## 安装方式

### 方式一：Docker 部署（推荐）

```bash
docker run -d \
  --name webcasa \
  --restart unless-stopped \
  -p 80:80 \
  -p 443:443 \
  -p 39921:8080 \
  -e WEBCASA_JWT_SECRET="$(openssl rand -hex 32)" \
  -v webcasa-data:/app/data \
  ghcr.io/web-casa/webcasa:latest
```

面板访问地址：`http://你的服务器IP:39921`

> **说明**：端口 80/443 用于 Caddy 处理反向代理和自动 HTTPS；39921 是面板管理端口。

### 方式二：Docker Compose 部署

创建 `docker-compose.yml`：

```yaml
services:
  webcasa:
    image: ghcr.io/web-casa/webcasa:latest
    container_name: webcasa
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "39921:8080"
    environment:
      - WEBCASA_JWT_SECRET=请替换为随机字符串
      - WEBCASA_PORT=8080
      - GIN_MODE=release
    volumes:
      - webcasa-data:/app/data
      # 如需 Docker 管理功能，挂载 Docker socket：
      - /var/run/docker.sock:/var/run/docker.sock

volumes:
  webcasa-data:
```

启动：

```bash
docker compose up -d
```

### 方式三：二进制直接运行

1. 从 [Releases](https://github.com/web-casa/webcasa/releases) 下载对应架构的二进制文件。

2. 安装 Caddy：

```bash
# Debian/Ubuntu
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install caddy

# 停止系统自带的 Caddy 服务（Web.Casa 会自行管理）
sudo systemctl stop caddy
sudo systemctl disable caddy
```

3. 运行：

```bash
export WEBCASA_JWT_SECRET="$(openssl rand -hex 32)"
export WEBCASA_CADDY_BIN="$(which caddy)"
export GIN_MODE=release
./webcasa
```

面板默认监听 `http://localhost:39921`。

### 方式四：从源码编译

```bash
# 依赖：Go 1.26+、Node.js 20+
git clone https://github.com/web-casa/webcasa.git
cd webcasa

# 编译前端
cd web && npm ci && npm run build && cd ..

# 编译后端
CGO_ENABLED=1 go build -ldflags="-s -w -X main.Version=$(cat VERSION)" -o webcasa .

# 运行
export WEBCASA_JWT_SECRET="$(openssl rand -hex 32)"
./webcasa
```

---

## 环境变量配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `WEBCASA_PORT` | `39921` | 面板 HTTP 监听端口 |
| `WEBCASA_JWT_SECRET` | `webcasa-change-me-in-production` | JWT 签名密钥，**生产环境必须修改** |
| `WEBCASA_DATA_DIR` | `./data` | 数据目录（数据库、日志、备份） |
| `WEBCASA_DB_PATH` | `{DATA_DIR}/webcasa.db` | SQLite 数据库路径 |
| `WEBCASA_CADDY_BIN` | `caddy` | Caddy 二进制路径 |
| `WEBCASA_CADDYFILE_PATH` | `{DATA_DIR}/Caddyfile` | 生成的 Caddyfile 路径 |
| `WEBCASA_LOG_DIR` | `{DATA_DIR}/logs` | Caddy 日志目录 |
| `WEBCASA_ADMIN_API` | `http://localhost:2019` | Caddy Admin API 地址 |
| `GIN_MODE` | `debug` | 设为 `release` 关闭调试输出 |

---

## 首次使用

1. 访问面板地址（如 `http://你的IP:39921`）。

2. 首次访问会进入**初始设置**页面，创建管理员账号：
   - 输入用户名和密码
   - 点击确认完成设置

3. 使用刚创建的账号登录。登录时需要完成 ALTCHA 验证（自动完成，无需人工操作）。

4. 登录后进入仪表盘，左侧导航栏包含所有功能模块。

---

## 功能模块

### 反向代理管理

核心功能。在 **Hosts** 页面管理反向代理规则。

**创建代理站点：**

1. 点击「Add Host」
2. 填写域名（如 `app.example.com`）
3. 添加上游地址（如 `localhost:3000`）
4. 开启 TLS（Caddy 会自动申请 Let's Encrypt 证书）
5. 可选开启 HTTP→HTTPS 跳转、WebSocket 支持
6. 保存后 Caddy 自动重载配置

**其他操作：**

- **分组/标签**：对站点进行分类管理
- **模板**：保存常用配置为模板，快速创建新站点
- **克隆**：基于现有站点快速创建新站点
- **启用/禁用**：一键切换站点状态
- **Caddyfile 编辑器**：直接查看和编辑生成的 Caddyfile
- **导入/导出**：JSON 格式配置导入导出

### 项目部署

在 **Projects** 页面管理源码部署。

**部署流程：**

1. 点击「Create Project」
2. 填写 Git 仓库地址、分支名
3. 如果是私有仓库，填写 Deploy Key（SSH 私钥）
4. 选择或自动检测框架（Node.js、Go、Python、PHP 等）
5. 确认构建命令和启动命令
6. 保存并点击「Build」

**支持的框架：**

- Node.js（Next.js、Nuxt、Vite、CRA 等）
- Go
- Python（Django、Flask、FastAPI）
- PHP（Laravel）
- 静态站点

**功能：**

- 自动检测框架和构建配置
- 实时构建日志
- 运行时日志查看
- 一键回滚到历史版本
- 环境变量管理
- Webhook 自动部署（支持 GitHub/GitLab）
- 自动创建反向代理

**Webhook 配置：**

在项目详情页的 Webhook 标签中找到 URL，格式为：
```
https://你的面板地址/api/plugins/deploy/webhook/{token}
```
将此 URL 添加到 GitHub/GitLab 仓库的 Webhook 设置中，开启 `auto_deploy` 开关即可。

### Docker 管理

在 **Docker** 页面管理容器、镜像、网络、卷。

**功能：**

- **概览**：系统资源使用情况
- **容器管理**：启动、停止、重启、删除、实时日志
- **镜像管理**：查看、删除、拉取镜像
- **网络管理**：查看、创建、删除网络
- **卷管理**：查看、创建、删除卷
- **Compose Stacks**：创建和管理 Docker Compose 项目
  - 在线编辑 `docker-compose.yml` 和 `.env`
  - 一键 Up / Down / Restart

> **注意**：Docker 功能需要面板进程能访问 Docker Socket。Docker 部署时需挂载 `/var/run/docker.sock`。

### 文件管理器

在 **Files** 页面浏览和管理服务器文件。

**功能：**

- 浏览目录、面包屑导航
- 创建文件夹
- 上传文件（最大 100MB）
- 下载文件
- 重命名、删除（支持批量）
- 修改权限（chmod）
- 在线编辑代码文件（CodeMirror 编辑器，支持语法高亮）
- 压缩/解压（tar.gz、zip）

### Web 终端

在 **Terminal** 页面访问服务器终端。

**功能：**

- 多标签页，同时打开多个终端会话
- 基于 xterm.js，支持全彩色输出
- 自动适应窗口大小
- 支持链接点击

> **安全提示**：终端以面板运行用户的权限执行命令。Docker 部署时为 root 权限。

### AI 助手

面板右下角的浮动对话按钮。

**配置：**

1. 进入 **AI Config** 页面
2. 填写 API 配置：
   - Base URL（如 `https://api.openai.com/v1`）
   - Model（如 `gpt-4o`）
   - API Key
3. 点击「Test Connection」验证

**功能：**

- 智能对话助手
- Docker Compose 生成（用自然语言描述需求）
- 错误日志诊断分析
- 对话历史管理

支持 OpenAI API 兼容的任何服务（OpenAI、Anthropic via proxy、Ollama 等）。

---

## 命令行工具

```bash
# 查看版本
./webcasa --version

# 重置管理员密码（交互式）
./webcasa reset-password
```

---

## HTTPS 与证书

Web.Casa 使用 Caddy 作为反向代理，Caddy 内置自动 HTTPS：

1. 确保域名 DNS 已解析到服务器 IP
2. 确保服务器 80 和 443 端口可访问（用于 ACME 验证）
3. 在创建 Host 时开启 TLS
4. Caddy 会自动申请并续期 Let's Encrypt 证书

**面板本身的 HTTPS：**

面板默认以 HTTP 运行在内网端口。如需通过 HTTPS 访问面板，建议：
- 在面板中为面板域名创建一个反向代理，指向 `localhost:39921`
- 或使用外部反向代理（如 nginx）代理面板端口

---

## 数据备份与恢复

所有数据存储在 `WEBCASA_DATA_DIR` 目录下：

```
data/
├── webcasa.db          # SQLite 数据库（用户、站点、插件配置）
├── Caddyfile           # 生成的 Caddy 配置
├── logs/               # Caddy 访问日志
├── backups/            # 自动备份
└── plugins/            # 插件数据
    ├── deploy/         # 项目源码和构建日志
    ├── docker/         # Compose Stack 文件
    ├── ai/             # AI 对话记录
    └── filemanager/    # 文件管理器配置
```

**备份：**

```bash
# Docker 部署
docker cp webcasa:/app/data ./webcasa-backup-$(date +%Y%m%d)

# 或直接备份 volume
docker run --rm -v webcasa-data:/data -v $(pwd):/backup alpine \
  tar czf /backup/webcasa-backup-$(date +%Y%m%d).tar.gz -C /data .
```

**恢复：**

```bash
# 停止面板
docker stop webcasa

# 恢复数据
docker run --rm -v webcasa-data:/data -v $(pwd):/backup alpine \
  sh -c "rm -rf /data/* && tar xzf /backup/webcasa-backup-YYYYMMDD.tar.gz -C /data"

# 重新启动
docker start webcasa
```

---

## 安全建议

1. **修改 JWT 密钥**：生产环境必须设置 `WEBCASA_JWT_SECRET` 为随机字符串
   ```bash
   openssl rand -hex 32
   ```

2. **限制面板访问**：面板端口（默认 39921）不建议公网暴露，可通过防火墙限制或使用 HTTPS 反向代理

3. **定期备份**：定期备份 `data/` 目录

4. **Docker Socket 权限**：挂载 Docker Socket 等同于给予面板 root 权限，请评估风险

5. **2FA 认证**：面板支持 TOTP 两步验证，建议开启（登录后在设置中配置）

---

## 常见问题

**Q: 忘记管理员密码怎么办？**

```bash
# 二进制部署
./webcasa reset-password

# Docker 部署
docker exec -it webcasa ./webcasa reset-password
```

**Q: Caddy 无法申请证书？**

- 确认域名 DNS 已解析到服务器
- 确认 80 和 443 端口未被其他程序占用
- 查看 Caddy 日志：面板中 Caddyfile 页面可查看状态

**Q: Docker 管理功能显示连接失败？**

确保 Docker Socket 已挂载：
```bash
docker run -v /var/run/docker.sock:/var/run/docker.sock ...
```

**Q: 如何更新版本？**

```bash
# Docker
docker pull ghcr.io/web-casa/webcasa:latest
docker stop webcasa && docker rm webcasa
# 用相同参数重新 docker run

# Docker Compose
docker compose pull && docker compose up -d
```

**Q: 面板启动后访问报 502？**

检查面板是否正常启动：
```bash
# Docker
docker logs webcasa

# 二进制
# 检查终端输出或日志文件
```

**Q: 支持哪些语言？**

面板内置中文和英文，可在界面右上角切换。
