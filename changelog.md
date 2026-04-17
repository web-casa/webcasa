# Changelog

所有版本变更记录。本项目使用 [Semantic Versioning](https://semver.org/)。

> 📌 本项目采用 AI Vibe Coding 方式开发，使用 Gemini 2.5 Pro + Antigravity Agent 辅助编码。

---

## [Unreleased] — v0.11 Phase 5 "Docker Polish"

### F8: 镜像状态分层缓存 (Portainer Pattern 5，本地对比版)
- 新增 `plugins/docker/imagestatus.go` — 短 TTL (5s) 缓存 tag→本地 imageID 映射，一次 ListContainers 内多个容器共用同一 tag 只 inspect 1 次
- `ContainerInfo` 新增字段:
  - `ImageID` — 容器实际运行的 SHA (docker SDK 已提供，之前未透传)
  - `ImageStatus` — `"updated"`/`"outdated"`/`"unknown"`，handler 层填充
- 状态判定纯本地对比 (**零网络依赖**)：容器运行的 `ImageID` 与该 tag 当前本地 SHA 对比：
  - 不同 = `outdated` (意思: 你 `docker pull` 了新版但没重建容器)
  - 相同 = `updated`
  - `nginx@sha256:...` digest 固定 = `unknown` (pinned 不适用)
  - 空 ImageID / inspect 失败 = `unknown`
- `PullImage` / `RemoveImage` 后自动 `invalidateImageStatusCache()`，下次 list 立即反映新状态
- handler 层用 500ms sub-timeout 包裹 annotation，慢 inspect 永不阻塞列表响应
- 前端 `DockerOverview.jsx` 容器卡片 `state` Badge 旁出现 `amber` 色 "outdated" Badge + 悬浮解释
- i18n keys (en + zh): `docker.image_outdated`, `docker.image_outdated_hint`
- 测试: 7 个用例 (compareImageStatus 矩阵 / 缓存 TTL 内去重 / 失败结果缓存 / Invalidate 立即生效 / TTL 过期刷新)

**设计说明**: 原计划通过 registry HEAD 请求对比远程 digest。改为纯本地 `docker pull` 状态对比后：
1. 零网络、零 registry 认证复杂度、毫秒级响应
2. 捕获的是更实际的问题 (pull 了新镜像但忘记重建容器)，而非 "registry 上游有新版本"
3. 若未来需要对比远程 digest，此基础设施已可扩展 (加一个 `remoteDigest` 字段到 cacheEntry，ttl=60s)

---

## [Unreleased] — v0.11 Phase 4 "Automation & Reliability"

### F7: Git polling 自动 redeploy (Portainer Pattern 8)
- 新增 `plugins/deploy/poller.go` — 后台定时调度器，全局 30s tick 扫描所有启用的项目；per-project 间隔最低 60s (配置低于则自动 clamp)，默认 300s
- 通过 `git ls-remote --heads <url> <branch>` 零克隆拉取远端 HEAD SHA，对比 `Project.LastDeployedCommit` 变化触发 `Service.Build()` (复用 Phase 1 SingleFlight 去重，webhook + polling 并发只会 build 一次)
- 支持 3 种 git 认证：SSH key (临时写 0600 key 文件 + `GIT_SSH_COMMAND`)、GitHub App token (HTTPS)、GitHub OAuth token (HTTPS)
- `Project` model 新增字段：`GitPollEnabled bool`、`GitPollIntervalSec int` (默认 300)、`LastDeployedCommit string`、`LastPolledAt *time.Time`
- 构建成功流程写入 `LastDeployedCommit = result.Commit` 供下次 polling 比对
- `UpdateProject` handler 白名单加 `git_poll_enabled`/`git_poll_interval_sec`，interval < 60 自动 clamp，< 0 拒绝
- Plugin `Start()/Stop()` 挂接 poller 生命周期 (goroutine 优雅停机，close(stop) + <-done 等待 tick 退出)
- 测试: 4 个用例 (effectivePollInterval 边界矩阵 / shortSHA 长度截断 / Build 触发 / 间隔 gate 生效)
- **兼容性**: `GitPollEnabled=false` 默认 = 零行为变化；依赖现有 `ErrBuildCoalesced` 和 `SingleFlight` (Phase 1)

### F6: 备份保留安全地板 (基于 Pigsty Pattern 7 启发)
- 发现现有 `plugins/backup/service.go` 已实现 count/age/size 三层保留 (v0.10.0)，本 Phase 聚焦于**加固而非重复**
- `BackupConfig` model 新增 `MinRetainCount int` 字段 (默认 1) — 安全地板，保证无论 count/age/size 规则如何激进，最新 N 个快照永远保留
  - **防止场景**: 用户误操作 `retain_days: 30 → 1` 一次性抹掉全部历史
- `enforceRetention()` 重写: 先按 count/age/size 计算 `toDelete` 集合，再按 MinRetainCount 反向 unpin 最新的 N 个
- 每次删除记录结构化日志 `reason=count|age|size`，汇总输出 `by_count=X by_age=Y by_size=Z` 便于审计
- `UpdateConfigRequest` 新增 `MinRetainCount *int` (负值拒绝)
- 首次启动默认配置 `MinRetainCount: 1`
- 测试: 5 个用例 (count-only 兼容 v0.10 / MinFloor 阻止全删 / MinFloor 钉住最新的 / MinFloor=0 退回旧行为 / count+age+size 三层叠加)
- **兼容性**: 新字段默认 1，既有用户升级后立即获得地板保护 (若之前 retention 策略会删光，升级后最新 1 个仍保留)

---

## [Unreleased] — v0.11 Phase 3 "Database Tuning"

### F1: PostgreSQL 调优预设 (Pigsty Pattern 1)
- 新增 `plugins/database/presets_postgres.go`：4 个工作负载感知预设
  - **OLTP** — 事务密集 web 应用 (shared_buffers≈25% RAM, work_mem=4MB, max_connections=100)
  - **OLAP** — 分析查询 (shared_buffers≈40% RAM, work_mem=64MB, max_connections=50)
  - **Tiny** — 资源受限 (shared_buffers ≤256MB 上限, max_connections=20, wal_level=minimal)
  - **Crit** — 强一致性 (与 OLTP 同内存布局，预留 v0.12 启用 sync_commit/full_page_writes)
- 预设根据 Instance.MemoryLimit (`256m`/`1g`/`0.5g`/`512mb`) 动态推算 `EngineConfig`，写入 `Config` JSON
- `Instance` model 新增 `TuningPreset` 字段 (size:32, default `''`) 记录所选预设供审计 + 未来 memory resize 时重应用
- `CreateInstanceRequest` 新增 `tuning_preset` 字段 (postgres only)，`resolveTuningPreset()` helper 统一两个创建入口
- 新 API `GET /api/plugins/database/presets/postgres-tuning` 返回 4 个预设的元数据 (id/name/description/good_for) 供 UI 渲染
- 前端 `DatabaseInstances.jsx` 创建表单新增「Workload Tuning Preset」下拉 (engine=postgres 时显示)，选中预设时跳过手填 Config (后端推算)，i18n keys: `tuning_preset`, `tuning_preset_custom`
- 完整测试覆盖: parseMemoryLimitMB 边界 / IsValidPostgresPreset / 4 个预设的内存缩放 / 自定义 fallback / 解析失败的安全降级 / List 元数据完整性 (8 个测试用例)
- **兼容性**: 现有实例 `tuning_preset=""` 行为完全等价 (走原有 EngineConfig 路径)，无 schema 数据迁移

---

## [Unreleased] — v0.11 Phase 2 "Deploy UX Quick Win"

### F2: docker run → compose 转换器 (Dockge Pattern 3)
- 新增 `web/src/utils/composerize.js` 包装 `composerize` npm 库 (1.7.5)，导出 `dockerRunToCompose(cmd)` 纯函数
- 在 Docker Overview 页面 `CreateStackDialog` 新增「从 `docker run` 导入」Tab：
  - TextArea 粘贴 `docker run ...` 命令
  - 「转换为 Compose」按钮调用 composerize
  - 转换成功后自动切换到 compose tab 并填入生成的 YAML
  - 错误情况显示 Callout 错误消息
- 自动剥离 composerize 输出的 `name: <your project name>` 占位行 (Stack 名称由独立表单字段管理)
- 新增 i18n keys (en + zh): `docker.import_docker_run`, `import_docker_run_hint`, `import_docker_run_failed`, `convert_to_compose`
- Smoke 测试覆盖 4 类常见命令 (env+port+volume+network / 仅镜像 / memory+cpu+restart / 多 port)
- Bundle 增加 ~25KB gzipped (composerize 内部含 yargs-parser + deepmerge)，无后端改动

---

## [Unreleased] — v0.11 Phase 2 审查修复

基于 Codex Review 发现的 4 个问题修复 (2 MEDIUM + 2 LOW)：

### MEDIUM: composerize 全量打进主 bundle
- **问题**: composerize + composeverter + yargs-parser@13 + core-js@2 (deprecated) + deepmerge 全部被 eagerly imported，拖累主 bundle ~90KB gzip，哪怕用户从不用这个功能
- **修复**: `DockerOverview.jsx` 移除顶层 `import { dockerRunToCompose }`，改为点击时 `await import('../utils/composerize.js')` 动态加载
- **效果**: 主 bundle 从 920KB gzip → 827KB gzip (**-10%**)；composerize 拆为独立 chunk `composerize-*.js` (89KB gzip)，仅首次点击时加载
- 添加 `converting` 状态 + Loader2 spinner，避免异步加载期间双击重复加载

### MEDIUM: 对话框状态未在关闭时重置
- **问题**: `activeTab`、`dockerRunCmd`、`convertError` 只在成功创建时重置。用户 cancel 失败的转换后重新打开对话框，会看到停留在 docker-run tab + 旧命令 + 红色错误
- **修复**: 新增 `useEffect(() => if (!open) ...)` 在 Dialog `open` prop 变 false 时清空所有瞬态状态，覆盖所有关闭路径 (Cancel 按钮 / Escape / 遮罩点击)

### LOW: 向用户直接暴露 composerize 原始错误文本
- **问题**: `setConvertError(e?.message || String(e))` 把 parser 内部错误 (e.g. "Cannot read property 'tokens' of undefined") 直接显示给用户，既不友好也泄露实现细节
- **修复**: `convertError` 改为 boolean 标志，UI 只显示 i18n `docker.import_docker_run_failed` 友好文本；原始错误走 `console.error` 供开发者调试

### LOW: 占位行剥离正则无测试守卫
- **问题**: `/^name: <your project name>\r?\n/m` 依赖 composerize 1.7.x 的精确占位文本。未来版本若变化会静默失败
- **修复**: 新增 `web/src/utils/composerize.test.mjs` 节点可执行回归测试 (无需 test framework)，覆盖 10 个断言：
  - 外部 network 触发 comment 时占位剥离
  - 简单镜像命令占位剥离
  - env + port + volume + restart 复合命令不丢字段
  - 空输入 throw
- CI / 本地用 `node web/src/utils/composerize.test.mjs` 即可运行；失败时立即知晓 composerize 行为变化

---

## [Unreleased] — v0.11 Phase 1 审查修复

基于 Codex Review 发现的 3 个问题 (1 HIGH + 2 MEDIUM) 修复：

### HIGH: buildLoop 退出 race — pending 请求可能被吞掉
- **问题**: `plugins/deploy/service.go` 的 `buildLoop` 用 `defer` 清 `buildInflight`，在 "pending 检查" 和 "inflight 清除" 之间存在窗口。并发 `Build()` 在窗口内看到 `inflight=true` 设 `pending=true` 然后返回 `ErrBuildCoalesced`，但 loop 已决定退出——pending 标志被孤立，最新 commit 永不部署
- **修复**: 移除 defer，将 `delete(buildInflight)` 挪到同一把锁下与 pending 检查原子执行
- **回归测试**: `TestBuild_ExitRace_PendingNotLost` — 50 轮 × 20 并发 Build，`-race` 下验证 `buildPending` 永不泄漏

### MEDIUM: 2FA TempToken 路径仍被 Login 限流器拦截
- **问题**: `Login()` 在路由到 tempToken 分支前调用 `limiters.Login.Check()`，导致 Login 预算耗尽时 2FA 第二步也被 429 拒绝——违反了 Phase 1 设计中的预算分离意图
- **修复**: 先 parse body，再按 `TempToken != ""` 分流；TempToken 路径完全跳过 Login bucket，仅由 `handleTempTokenLogin` 入口的 TOTP bucket 门控
- **回归测试**: `TestLogin_TempTokenPath_NotBlockedByLoginLimiter` 验证 Login 耗尽时 2FA 仍可通过；`TestLogin_PrimaryPath_StillBlockedByLoginLimiter` 验证主凭据路径仍受保护

### MEDIUM: `clientAcceptsGzip` 误读多参数 header
- **问题**: 旧解析只读 `;` 后的第一个参数块，导致 `gzip;foo=bar;q=0` 和 `gzip;q=0;foo=bar` 被错误识别为接受 gzip，违反 RFC 7231 §5.3.1
- **修复**: 扫描所有 `;` 分隔的参数查找 `q=`，malformed q 保守拒绝 (返回 false 而非 true)
- **回归测试**: 5 个新 case 覆盖多参数排列

所有 Phase 1 影响包通过 `go test -race` 验证。

---

## [Unreleased] — v0.11 Phase 1 "Core Infrastructure"

本 Phase 交付 3 个横切基础设施改进，基于 Portainer / Dockge / 1Panel 竞品分析。所有变更 **additive-only**，对 v0.10.0 行为完全兼容。

### F3: SingleFlight 部署去重
- 替换 `plugins/deploy/service.go` 现有 buildSems/timer 合并逻辑为 `golang.org/x/sync/singleflight.Group` + inflight/pending 双标志
- **修复**: 旧代码会在第二个并发 Build() 调用处阻塞最多 5 分钟 (webhook handler 可能 504)。新实现 Build() **永不阻塞**，立即返回 nil 或 ErrBuildCoalesced
- 保留 "队列最新版本" 语义: 并发请求合并为 1 个 pending，current build 完成后再跑 1 次保证最新代码生效
- 新增 `plugins/deploy/build_dedup_test.go` (3 个测试: 非阻塞 / pending 重跑 / 跨项目独立)

### F4: Gzip 响应压缩 helper
- 新增 `internal/handler/helper.go` 的 `SuccessGzipped(c, data)` 函数
- 显式调用非全局中间件，阈值 1KB 以下不压缩 (避免反向开销)
- Accept-Encoding 协商支持 q-value (q=0 禁用)，fallback 到未压缩响应
- `sync.Pool` 复用 gzip.Writer 降低分配
- `plugins/appstore/handler.go` 的 `ListApps` 启用 gzip (典型 50KB+ 多语言 JSON)
- 新增 `internal/handler/helper_test.go` (4 个测试: 协商矩阵 / 小负载跳过 / 大负载压缩 / 无头裸响应)

### F5: 分层限流器服务
- 重构 `internal/auth/ratelimit.go` 引入 `Limiters` 场景化预实例化:
  - **Login**: 5/15min (防暴破，与旧版行为一致)
  - **TOTP**: 10/5min (允许 2FA 打错纠正，阻止穷举)
  - **APIRead**: 300/min (仪表盘轮询留余量)
  - **APIWrite**: 60/min (突变端点上限)
  - **Default**: 600/min (未标记路由 umbrella)
- 新增 `RateLimiter.Middleware()` 返回 gin.HandlerFunc (路由挂载式限流)，不自动 RecordFail (语义由 handler 决定)
- `internal/handler/auth.go` 拆分 Login/Setup → `limiters.Login`；2FA 步骤 → `limiters.TOTP` (打错 2FA 不烧 Login 预算)
- `handleTempTokenLogin` 新增入口 TOTP.Check，防止 2FA 穷举绕过
- 新增 `internal/auth/ratelimit_test.go` (5 个测试: 桶独立 / 配置校验 / 中间件通过 / 429 触发 / 不自动 RecordFail)

### Internals
- 升级 `golang.org/x/sync` v0.10.0 → v0.20.0 (需 singleflight 子包)
- 无 schema 迁移、无 API 破坏性变更、无权限模型变动

---

## [0.10.0] - 2026-04-16

### v2.0 Roadmap — 19 Features + RBAC Overhaul

基于 Coolify / CapRover / Dokku / Dokploy 四竞品分析，实施 4 个 Phase 的功能升级。

#### Phase 1: 安全与性能基础
- **Webhook HMAC 签名验证** — GitHub SHA-256 + GitLab token，加密存储 secret
- **SSRF 防护** — 4 层 URL 验证 + DNS rebinding 防护 (SafeDialContext) + 重定向禁用
- **三层配置回退** — Host → Global Settings → 内置默认值
- **Caddy Reload 合并** — 500ms debounce，fan-out 多调用者

#### Phase 2: 部署可靠性
- **构建队列合并** — 同项目构建自动合并，超时重入队
- **容器状态聚合** — 6 级优先级状态机 (degraded/restarting/running/paused/starting/stopped)
- **健康检查增强** — HTTP method + 预期状态码 + 响应体匹配 + 启动等待期
- **备份保留策略** — 数量 + 天数 + 总大小三维自动清理
- **镜像级回滚** — 部署成功时 tag 镜像，回滚秒级切换

#### Phase 3: 用户体验与权限
- **四级 RBAC** — owner / admin / operator / viewer 角色层级
- **DNS 解析预验证** — 创建 Host 时异步检查域名指向
- **通配符子域名** — wildcard_domain 设置 + DNS label 清洗
- **多构建器** — Dockerfile / Nixpacks / Paketo / Railpack / Static，自动检测

#### Phase 4: 架构扩展
- **Preview 部署模型** — GitHub PR 临时环境（pending/running/expired 生命周期）
- **异步任务队列** — Valkey/内存 fallback，bounded worker pool + graceful shutdown
- **多代理抽象** — ProxyBackend 接口 + CaddyBackend 适配器
- **通知接口重构** — Notification 接口 + BaseNotification 默认实现

#### 权限系统全面重构
- 插件框架新增 OperatorRouter（3 级路由：Router / OperatorRouter / AdminRouter）
- 敏感路由提权：settings / audit / export / certs / users / AI config → adminOnly
- 操作类路由：deploy build/start/stop + docker container/stack ops + caddy control → operatorOnly
- Deploy env_vars 对非 admin 用户脱敏
- Owner 账户保护（密码/角色/删除均需 owner 权限）
- PluginGuardMiddleware 覆盖所有 4 个路由组
- 所有 io.ReadAll 加 LimitReader 上限
- 文件管理器默认根路径改为 /

---

## [0.9.5] - 2026-03-20

### Security — 全量安全审查与加固

基于 6 轮外部安全审查 + 全量代码审查（6 个并行 agent），修复 100+ 安全和可靠性问题。

#### 安全修复
- **Caddyfile 注入防护** — DNS 凭据、BasicAuth 用户名、custom_directives `} {` 绕过全部修复
- **路由权限加固** — Caddyfile 仅 admin 可读、review-code 移至 admin 路由、SQLite Browser 限制在数据目录 + admin-only
- **AI Memory 用户隔离** — 所有查询按 user_id 隔离，per-user prune 限制，delete_memory 仅 admin
- **MCP Token 安全** — constant-time hash 比较、空权限默认拒绝（需 `["*"]` 显式授权）
- **Google API Key** — 从 URL 参数改为 `x-goog-api-key` 请求头
- **密码最低长度** — 统一为 8 位（setup/login/user create）
- **上传文件名** — `filepath.Base()` 防路径穿越
- **凭据加密基础设施** — CoreAPI.EncryptSecret/DecryptSecret (AES-GCM)

#### 可靠性修复
- **ApplyConfig 回滚** — Caddy reload 失败时自动恢复旧 Caddyfile
- **Host 更新路由重映射** — upstream 重建后自动更新 route.UpstreamID 映射
- **创建失败回滚** — Docker stack / 数据库实例 / PHP runtime / FrankenPHP 站点 auto-start 失败时回滚 DB + 清理文件
- **删除保护** — Docker stack / 数据库实例 / PHP runtime/site compose down 失败时保留 UI 记录
- **Docker daemon 配置回滚** — restart 失败时恢复旧 daemon.json
- **PHP 配置/扩展回滚** — restart/rebuild 失败时恢复旧配置
- **SSE 解析** — 同时支持 `data: ` 和 `data:` 两种格式（兼容 GLM 等提供商）
- **端口验证** — 数据库实例自定义端口范围检查 + 冲突检测
- **用户授权回滚** — grant 失败时 drop 已创建用户

#### 功能改进
- **插件 i18n** — 12 个插件的名称/描述/分类全部中英文翻译
- **MCP 独立页面** — Token 管理从系统设置独立，仅 MCP 插件启用时显示
- **模板域名检查** — 部署前检查域名是否已占用
- **App Store 域名验证** — InstallApp 时校验域名格式
- **Dockerfile 修复** — 运行时镜像包含前端静态文件

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
