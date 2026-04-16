# WebCasa v2.0 功能路线图

> 基于 Coolify / CapRover / Dokku / Dokploy 四个竞品分析，结合 WebCasa 自身定位制定。

## 设计原则

1. **零依赖优先** — SQLite 为默认存储，Valkey 为可选增强（队列/缓存）
2. **EL9/EL10 专属** — 仅支持 RHEL 系 9/10，Valkey 8.0 来自 AppStream
3. **渐进增强** — P0/P1 不依赖 Valkey，P2 开始可选使用
4. **向后兼容** — 现有 API 不做破坏性变更，新功能通过新端点或可选参数添加
5. **插件内聚** — 新功能优先在现有插件内实现，避免新增插件

---

## Phase 1: 安全与性能基础 (P0)

> 预计工作量: 12h | 无外部依赖 | 向后兼容

### 1.1 Webhook HMAC 签名验证
**来源**: Coolify | **位置**: `plugins/deploy/handler.go`

- Webhook 端点支持 `X-Hub-Signature-256` 头验证
- 使用 `crypto/hmac` + `crypto/sha256` 计算签名
- `crypto/subtle.ConstantTimeCompare` 常量时间比较
- 兼容: 签名头不存在时仍接受纯 token 验证（向后兼容旧 webhook）
- 项目设置页面增加 "Webhook Secret" 字段

```go
// 验证逻辑
if sigHeader != "" {
    mac := hmac.New(sha256.New, []byte(project.WebhookSecret))
    mac.Write(body)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    if !hmac.Equal([]byte(sigHeader), []byte(expected)) {
        return 401
    }
} else {
    // 降级: 纯 token 匹配 (向后兼容)
}
```

### 1.2 SSRF 防护
**来源**: Coolify | **位置**: `internal/notify/notifier.go`

新增 `ValidateWebhookURL(url string) error`:
- Layer 1: `url.Parse` 格式检查
- Layer 2: Scheme 白名单 (http/https)
- Layer 3: 危险主机名拒绝 (localhost, 0.0.0.0, ::1, *.internal)
- Layer 4: IP 黑名单 (127.0.0.0/8, 169.254.0.0/16)
- 允许私有网段 (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) — 自托管场景需要
- 在通知渠道创建/更新时调用，以及每次发送前验证

### 1.3 三层配置回退
**来源**: Dokku | **位置**: `internal/service/host.go`, 新增 `internal/service/computed.go`

```go
// ComputedValue 按优先级查找配置值: Host → Global Settings → Default
func ComputedValue(db *gorm.DB, hostID uint, key string, defaultVal string) string {
    // 1. Host 级别: host 表的对应字段
    // 2. Global 级别: settings 表的 key
    // 3. 内置默认值
}
```

首批支持的计算属性:
- `max_body_size` (默认 100m)
- `proxy_read_timeout` (默认 60s)
- `proxy_connect_timeout` (默认 10s)
- `hsts_max_age` (默认 31536000)
- `access_log_format` (默认 combined)

兼容: Host 表新增 `config_overrides` JSON 字段存储覆盖值，不影响现有列。

### 1.4 Caddy Reload 合并
**来源**: CapRover | **位置**: `internal/caddy/manager.go`

```go
type Manager struct {
    // ...
    reloadDebounce *time.Timer
    reloadMu       sync.Mutex
    reloadPending  bool
}

// RequestReload 请求 reload，500ms 内的多次请求合并为一次
func (m *Manager) RequestReload() {
    m.reloadMu.Lock()
    defer m.reloadMu.Unlock()
    m.reloadPending = true
    if m.reloadDebounce != nil {
        m.reloadDebounce.Stop()
    }
    m.reloadDebounce = time.AfterFunc(500*time.Millisecond, func() {
        m.reloadMu.Lock()
        m.reloadPending = false
        m.reloadMu.Unlock()
        m.Reload()
    })
}
```

兼容: `ApplyConfig` 改为调用 `RequestReload()` 而非直接 `Reload()`。批量操作（导入、批量启用）自动受益。紧急场景（CLI reset）仍可直接 `Reload()`。

---

## Phase 2: 部署可靠性 (P1)

> 预计工作量: 22h | 无外部依赖 | 向后兼容

### 2.1 构建队列合并
**来源**: CapRover+Dokploy | **位置**: `plugins/deploy/service.go`

```go
type BuildQueue struct {
    mu       sync.Mutex
    active   map[uint]*BuildJob    // projectID → running job
    pending  map[uint]*BuildJob    // projectID → queued job (latest wins)
    sem      chan struct{}          // 并发限制
}

// Enqueue 入队构建任务，同一项目的旧排队任务被替换
func (q *BuildQueue) Enqueue(job *BuildJob) {
    q.mu.Lock()
    if _, running := q.active[job.ProjectID]; running {
        q.pending[job.ProjectID] = job  // 替换，不重复排队
        q.mu.Unlock()
        return
    }
    q.mu.Unlock()
    q.execute(job)
}
```

兼容: 替换现有信号量机制，API 不变。

### 2.2 容器状态聚合
**来源**: Coolify | **位置**: `plugins/docker/service.go`, 新增 `plugins/docker/status.go`

10 级优先级状态机:
```
degraded > restarting > crash_loop > mixed > running > dead > paused > starting > exited
```

输出格式: `"running:healthy"`, `"degraded:unhealthy"`, `"mixed:unknown"`

- Stack 列表 API 返回聚合状态而非第一个容器的状态
- 前端 Docker 页面用颜色标签显示聚合状态

兼容: Stack API 新增 `aggregated_status` 字段，原 `status` 保留。

### 2.3 零停机部署增强
**来源**: Dokku+Dokploy | **位置**: `plugins/deploy/service.go`

部署流程改为:
```
1. 构建新镜像
2. 启动新容器（旧容器继续服务）
3. 等待新容器健康检查通过
   - HTTP 探测: path + method + 预期状态码 + 响应体匹配
   - 端口监听检查
   - 启动等待期 (start_period)
   - 重试次数 + 间隔 + 超时
4. 健康 → 切换 Caddy upstream → 退役旧容器
5. 不健康 → 删除新容器 → 旧容器继续服务 → 报错
```

Project 模型新增字段:
```go
HealthCheckMethod     string  // GET, HEAD, POST
HealthCheckExpectCode int     // 200, 201, etc.
HealthCheckExpectBody string  // 响应体包含的文本
HealthCheckStartPeriod int    // 启动等待秒数
```

兼容: 新字段默认值为空/0，不影响现有项目的健康检查行为。

### 2.4 备份保留策略
**来源**: Coolify | **位置**: `plugins/backup/service.go`

BackupConfig 模型新增:
```go
RetentionCount     int  // 保留最近 N 份 (0=不限)
RetentionDays      int  // 超过 N 天删除 (0=不限)
RetentionMaxSizeMB int  // 总大小超过 N MB 删除 (0=不限)
```

定时清理: 每次备份完成后评估保留策略，删除超出的旧快照。

兼容: 新字段默认 0（不限），现有备份行为不变。

### 2.5 镜像级回滚
**来源**: Dokploy | **位置**: `plugins/deploy/service.go`

```go
// 部署成功后 tag 镜像
docker tag app:latest app:v{deploymentNum}

// 回滚时
docker tag app:v{targetNum} app:latest
// 重启容器指向新 tag → 秒级完成
```

Deployment 模型新增: `image_tag string`

兼容: 旧部署记录 `image_tag` 为空，回滚时 fallback 到重新构建。

---

## Phase 3: 用户体验与权限 (P2)

> 预计工作量: 41h | Valkey 8.0 可选 | 部分需要前端改动

### 3.1 四级 RBAC 角色
**来源**: Dokploy | **位置**: `internal/model/model.go`, `internal/auth/`, `internal/handler/`

| 角色 | 权限 |
|------|------|
| **owner** | 全部权限 + 删除面板 + 管理 owner |
| **admin** | 全部权限（不能改 owner） |
| **operator** | 操作权限（启停/部署/重启），不能删除和改配置 |
| **viewer** | 只读（看 dashboard/日志/状态） |

实现:
- User 模型 `role` 字段从 `admin|viewer` 扩展为 `owner|admin|operator|viewer`
- 新增 `RequireOperator` 中间件（允许 operator+admin+owner）
- 现有 `RequireAdmin` 允许 admin+owner
- 路由分组: 查看类 → protected, 操作类 → operator, 配置类 → admin, 危险操作 → owner

数据库迁移:
```sql
-- 向后兼容: 现有 admin 用户的第一个自动升级为 owner
UPDATE users SET role = 'owner' WHERE id = (
  SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1
);
```

前端: 用户管理页面角色选择器从 2 个变为 4 个。

### 3.2 DNS 解析预验证
**来源**: CapRover+Dokku | **位置**: `internal/service/host.go`

```go
// ValidateDNSResolution 检查域名是否指向本机
func ValidateDNSResolution(domain string) (bool, string) {
    ips, err := net.LookupHost(domain)
    serverIPs := getServerIPs()  // 获取本机所有 IP
    for _, ip := range ips {
        if contains(serverIPs, ip) {
            return true, ip
        }
    }
    return false, ""
}
```

- Host 创建时可选验证（settings 中开关，默认关闭）
- 验证失败返回 warning（不阻塞创建），前端显示提示
- DNS 状态在 Host 列表中显示（已有 DNS check 机制，增强之）

兼容: 默认关闭，不影响现有流程。

### 3.3 通配符子域名
**来源**: CapRover+Dokku | **位置**: `internal/service/host.go`, Settings 页面

流程:
1. 用户在 Settings 中配置 `wildcard_domain`（如 `app.example.com`）
2. 用户在 DNS 中添加 `*.app.example.com → 面板 IP`
3. 创建 Host 时可选"自动分配子域名"→ 生成 `{appname}.app.example.com`
4. Caddy 使用 on-demand TLS 自动签发子域名证书

Settings 新增:
```
wildcard_domain = ""          // 通配符根域名
wildcard_tls_mode = "auto"    // auto | dns | off
```

兼容: 不配置 wildcard_domain 时功能不激活。

### 3.4 健康检查增强
**来源**: Coolify+Dokku | **位置**: `plugins/deploy/service.go`

扩展 `runHealthCheck`:
```go
type HealthCheck struct {
    Path       string        // /health
    Method     string        // GET (default), HEAD, POST
    ExpectCode int           // 200 (default)
    ExpectBody string        // "" (any body)
    Timeout    time.Duration // 30s
    Interval   time.Duration // 5s
    Retries    int           // 5
    StartPeriod time.Duration // 0s
}
```

兼容: 现有项目只有 `HealthCheckPath`，其他字段为默认值，行为不变。

### 3.5 多构建器支持
**来源**: Dokku+Dokploy | **位置**: `plugins/deploy/`, 新增 `plugins/deploy/builders/`

```go
type Builder interface {
    Name() string
    Detect(projectDir string) bool  // 自动检测是否适用
    Build(ctx context.Context, project *Project, logWriter io.Writer) (string, error)
}

// 注册的构建器 (按优先级)
var builders = []Builder{
    &DockerfileBuilder{},  // Dockerfile 存在时优先
    &NixpacksBuilder{},    // 自动检测语言环境
    &PaketoBuilder{},      // Cloud Native Buildpacks
    &RailpackBuilder{},    // Railpack (Rust-based)
    &StaticBuilder{},      // 纯静态文件 (nginx 容器)
}
```

Project 模型新增: `build_type string` (dockerfile|nixpacks|paketo|railpack|static|auto)

自动检测逻辑 (`auto` 模式):
1. 有 Dockerfile → DockerfileBuilder
2. 有 package.json / go.mod / requirements.txt → NixpacksBuilder
3. 有 Gemfile → RailpackBuilder
4. 只有 HTML/CSS/JS → StaticBuilder
5. 默认 → NixpacksBuilder

外部依赖:
- Nixpacks: `curl -sSL https://nixpacks.com/install.sh | bash` (一次性安装)
- Paketo: 需要 `pack` CLI
- Railpack: 需要 `railpack` CLI
- 面板提供安装按钮（检测 CLI 是否存在，不存在则提示安装命令）

兼容: 现有项目 `build_type` 默认空，走现有 Dockerfile/裸机逻辑。

---

## Phase 4: 架构扩展 (P2-P3)

> 预计工作量: 24h | Valkey 8.0 推荐 | 前端改动较大

### 4.1 Preview 部署 (仅 GitHub)
**来源**: Dokploy | **位置**: `plugins/deploy/`, 新增 `plugins/deploy/preview.go`

流程:
```
GitHub PR opened/synchronized
  → Webhook 触发
  → 检查项目是否启用 preview
  → 生成临时域名: pr-{number}.{wildcard_domain}
  → 克隆 PR 分支代码
  → 构建 + 部署到独立容器
  → 创建 Caddy Host
  → GitHub API 评论 PR（包含预览 URL）

GitHub PR closed/merged
  → Webhook 触发
  → 删除容器 + Host + 代码目录
  → GitHub API 更新评论（标记已清理）
```

前置条件:
- 项目必须配置 `wildcard_domain`
- 项目必须启用 `preview_enabled`
- 项目必须有 GitHub webhook secret

Project 模型新增:
```go
PreviewEnabled    bool   // 启用 Preview 部署
PreviewExpiry     int    // Preview 最大存活天数 (默认 7)
GitHubToken       string // 用于评论 PR (加密存储)
```

DB 新增表:
```go
type PreviewDeployment struct {
    ID            uint
    ProjectID     uint
    PRNumber      int
    Branch        string
    Domain        string
    ContainerName string
    HostID        uint
    Status        string  // running | stopped | error
    CreatedAt     time.Time
    ExpiresAt     time.Time
}
```

定时清理: CronJob 每小时检查过期的 Preview 部署并清理。

### 4.2 异步部署队列 (Valkey)
**来源**: Dokploy | **位置**: `plugins/deploy/service.go`, 新增 `internal/queue/`

```go
// internal/queue/queue.go
type Queue struct {
    client *asynq.Client
    server *asynq.Server
}

func NewQueue(redisAddr string) *Queue { ... }

// 任务类型
const (
    TypeBuild    = "deploy:build"
    TypePreview  = "deploy:preview"
    TypeCleanup  = "deploy:cleanup"
    TypeBackup   = "backup:run"
)
```

Valkey 安装 (install.sh 中自动):
```bash
# EL9/EL10 AppStream
dnf install -y valkey
systemctl enable --now valkey
```

配置:
```
WEBCASA_VALKEY_ADDR=localhost:6379  # 默认
```

兼容策略:
- Valkey 不可用时 fallback 到内存队列（现有 channel 方案）
- 安装脚本提供 `--with-valkey` 选项，默认不安装
- Settings 页面显示 Valkey 连接状态

### 4.3 多代理抽象接口
**来源**: Dokku | **位置**: `internal/caddy/`, 新增 `internal/proxy/`

```go
// internal/proxy/proxy.go
type ProxyBackend interface {
    Name() string
    GenerateConfig(hosts []model.Host, cfg *config.Config) (string, error)
    WriteConfig(content string) error
    Reload() error
    Validate(content string) error
    IsRunning() bool
    Start() error
    Stop() error
}

// 当前唯一实现
type CaddyBackend struct { ... }
```

- 现有 `caddy.Manager` 重构为实现 `ProxyBackend` 接口
- `HostService` 通过接口调用，不直接依赖 Caddy
- 预留 Nginx/HAProxy 实现的扩展点

兼容: 纯内部重构，API 无变化。

### 4.4 通知渠道重构
**来源**: Coolify | **位置**: `internal/notify/`

```go
type Notification interface {
    Type() string        // deploy.success, backup.complete, etc.
    Title() string
    ToEmail() EmailPayload
    ToDiscord() DiscordPayload
    ToTelegram() TelegramPayload
    ToWebhook() WebhookPayload
}

// 每种事件实现此接口
type DeploySuccessNotification struct { ... }
type BackupCompleteNotification struct { ... }
type AlertFiredNotification struct { ... }
```

兼容: 内部重构，通知 API 和用户配置不变。

---

## 兼容性矩阵

| 功能 | API 变更 | DB 迁移 | 前端改动 | 外部依赖 | 向后兼容 |
|------|---------|---------|---------|---------|---------|
| HMAC 验证 | 无 | 新增列 | 设置页 | 无 | ✅ |
| SSRF 防护 | 无 | 无 | 无 | 无 | ✅ |
| 3 层配置 | 新增端点 | 新增列 | 设置页 | 无 | ✅ |
| Reload 合并 | 无 | 无 | 无 | 无 | ✅ |
| 构建队列合并 | 无 | 无 | 无 | 无 | ✅ |
| 容器状态聚合 | 新增字段 | 无 | Docker 页 | 无 | ✅ |
| 零停机部署 | 无 | 新增列 | 项目设置 | 无 | ✅ |
| 备份保留策略 | 无 | 新增列 | 备份设置 | 无 | ✅ |
| 镜像级回滚 | 无 | 新增列 | 项目详情 | 无 | ✅ |
| RBAC 四级 | 无 | 迁移 | 用户管理 | 无 | ✅ 自动升级 |
| DNS 验证 | 新增端点 | 无 | Host 创建 | 无 | ✅ 默认关闭 |
| 通配符域名 | 无 | 无 | 设置页 | 无 | ✅ |
| 健康检查增强 | 无 | 新增列 | 项目设置 | 无 | ✅ |
| 多构建器 | 无 | 新增列 | 项目设置 | Nixpacks等 | ✅ |
| Preview 部署 | 新增端点 | 新增表 | 项目设置 | 通配符域名 | ✅ |
| Valkey 队列 | 无 | 无 | 设置页 | Valkey 8.0 | ✅ fallback |
| 多代理抽象 | 无 | 无 | 无 | 无 | ✅ |
| 通知重构 | 无 | 无 | 无 | 无 | ✅ |

---

## 数据库迁移计划

### GORM AutoMigrate 新增列 (无损)

```go
// Phase 1
type Host struct {
    ConfigOverrides string `gorm:"type:text"` // JSON: per-host config overrides
}
type Project struct {
    WebhookSecret string `gorm:"size:128"` // HMAC secret
}

// Phase 2
type Project struct {
    HealthCheckMethod     string `gorm:"size:8"`
    HealthCheckExpectCode int    `gorm:"default:0"`
    HealthCheckExpectBody string `gorm:"size:512"`
    HealthCheckStartPeriod int   `gorm:"default:0"`
    BuildType             string `gorm:"size:32;default:''"`
}
type Deployment struct {
    ImageTag string `gorm:"size:128"`
}
type BackupConfig struct {
    RetentionCount     int `gorm:"default:0"`
    RetentionDays      int `gorm:"default:0"`
    RetentionMaxSizeMB int `gorm:"default:0"`
}

// Phase 3
type Project struct {
    PreviewEnabled bool   `gorm:"default:false"`
    PreviewExpiry  int    `gorm:"default:7"`
    GitHubToken    string `gorm:"size:512"`
}
// 新表
type PreviewDeployment struct { ... }
```

### 破坏性迁移 (需特殊处理)

```go
// RBAC: role 字段值域扩展
// 自动迁移: 第一个 admin 升级为 owner
func migrateRBAC(db *gorm.DB) {
    db.Exec(`UPDATE users SET role = 'owner' WHERE id = (
        SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1
    )`)
}
```

---

## 安装脚本变更

```bash
# install.sh 新增选项
--with-valkey    # 安装 Valkey 8.0 (EL9/10 AppStream)

# Valkey 安装逻辑
install_valkey() {
    dnf install -y valkey
    systemctl enable --now valkey
    # 写入配置
    echo "WEBCASA_VALKEY_ADDR=localhost:6379" >> /etc/webcasa/webcasa.env
}
```

---

## 实施时间线

| Phase | 内容 | 预计工作量 | 前置条件 |
|-------|------|----------|---------|
| **Phase 1** | 安全+性能基础 | 12h | 无 |
| **Phase 2** | 部署可靠性 | 22h | Phase 1 |
| **Phase 3** | 体验+权限 | 41h | Phase 2 |
| **Phase 4** | 架构扩展 | 24h | Phase 2, Valkey |
| **总计** | | **99h** | |

建议 Phase 1+2 合并为 v1.1 版本，Phase 3+4 为 v1.2 版本。
