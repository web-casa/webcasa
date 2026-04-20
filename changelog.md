# Changelog

所有版本变更记录。本项目使用 [Semantic Versioning](https://semver.org/)。

> 📌 本项目采用 AI Vibe Coding 方式开发，使用 Gemini 2.5 Pro + Antigravity Agent 辅助编码。

---

## [0.14.0] - 2026-04-19

### Preview Deploy Phase A: ephemeral per-PR preview environments

GitHub PR webhook → ephemeral subdomain (`pr-N-slug-id.<wildcard>`) →
isolated container build + Caddy reverse-proxy host. PR `closed` →
full teardown. Backend-complete; frontend "Previews" tab and
build-log streaming arrive in Phase B.

#### What ships
- `pull_request` webhook handler (HMAC-verified, shares the existing
  webhook token + secret) routes `opened` / `reopened` /
  `synchronize` to a build-and-expose pipeline; `closed` to full
  teardown.
- `PreviewDeployment` table tracks per-PR state with composite unique
  index `(project_id, pr_number)`, allocated `BasePort` in
  `[20000, 25000)`, two-slot alternation (slot 0 = BasePort, slot 1 =
  BasePort+5000), and a monotonic `Generation` token for fence-style
  concurrency control.
- Admin endpoints: `GET /projects/:id/previews`,
  `DELETE /previews/:previewId`. PR detail page surfaces the live
  domain + status.
- Daily GC sweeps preview rows past `expires_at` regardless of
  status (default 7 days, configurable per project).
- Plugin lifecycle: `PreviewService.Stop()` cancels in-flight
  goroutines via root context + WaitGroup with 30s drain ceiling so
  plugin teardown doesn't leave zombie git/podman children.

#### Concurrency model

Twelve rounds of Codex review hardened the design against every
race we could enumerate:

- **Two-slot alternation**: each rebuild flips to the unused slot;
  Caddy upstream tracks the live port; old container stops only
  after traffic moves.
- **Generation token** on every DB write
  (`setStatus` / `markFailed` / `host_id` / final slot update):
  `CreatePreview` and `DeletePreview` both bump generation; stale
  goroutines whose ctx-cancel arrived too late find their writes
  rejected with `RowsAffected==0` and tear down their own staging
  container instead of corrupting state.
- **createMu serialization**: per-`PreviewService` mutex covers
  `upsert + jobs-map atomic swap` (CreatePreview) and
  `bump + capture + job-snapshot + cleanup` (DeletePreview). Drain
  windows release the lock so unrelated webhooks aren't blocked.
- **TCP readiness probe** (`waitForPortOpen`, 500ms tick / 30s cap)
  before swapping Caddy upstream — a crashing container fails fast
  instead of receiving traffic.
- **Token-via-env** for HTTPS clones (`GIT_CONFIG_COUNT` ladder,
  scoped to `http.https://<host>/.extraHeader`) so PR auth tokens
  never appear in `ps` output or `git remote -v`.

#### Bug-bash audit trail

12 review rounds, **43 findings landed** (3 Critical / 22 High /
14 Medium / 4 Low), 3 Mediums consciously deferred to v0.15
(per-preview lock for `R11-M1`, main-Build token-via-env for
`R8-M4`). The squashed branch is preserved on `main` as a single
merge for blame-readability; full per-round commit history is in
`fix/v0.14-preview-codex-review` (9 commits) for anyone tracing the
fix evolution.

#### Migration

Pre-v0.14 `PreviewDeployment` rows (none in production — Phase A
was unreleased) are dropped on first start by a guard in
`Init()` that detects the missing `base_port` column. Other plugin
data unaffected.

#### Known limitations (v0.15)

- `R8-M4`: main `Build()` still uses `ConvertToHTTPS` which embeds
  the GitHub App token in the URL (visible to `git remote -v`).
  Preview path uses the env-var approach; main path migration is
  multi-file refactor scoped to v0.15.
- `R11-M1`: `createMu` is per-PreviewService, so DeletePreview's
  destructive cleanup phase briefly blocks unrelated CreatePreview
  webhooks. Acceptable at single-project / low-PR-rate volumes;
  planned `sync.Map`-based per-`(project_id, pr_number)` lock is
  v0.15.

---

## [0.13.0] - 2026-04-19

### Podman refinements: Nixpacks UI + SELinux preview + shim future plan

Three small, independent epics delivered alongside the v0.12.1 patch
cycle. No breaking changes — default install behaviour is identical
to v0.12.1 unless you opt in.

#### Epic 1 — webcasa_t SELinux policy (preview, opt-in)

New `policy/webcasa.te` + `webcasa.fc` + pre-built `webcasa.pp`
(committed) narrow the service domain from `unconfined_service_t` to
a dedicated `webcasa_t` with explicit allow rules for reading
`/etc/webcasa`, writing `/var/lib/webcasa` + `/var/log/webcasa`,
connecting `/run/podman/podman.sock`, binding `http_port_t`, and
executing allow-listed CLIs (`podman` / `podman-compose` / `caddy` /
`kopia` / `docker` shim).

**Status: opt-in preview**. VPS validation revealed that exec'd
children (caddy, curl, podman CLI) inherit `webcasa_t` and hit
additional rules not covered in this baseline. Rather than ship a
broken default, `install.sh` only installs the policy when
`ENABLE_SELINUX_POLICY=1` is set. Default v0.13.0 installs stay on
`unconfined_service_t` (identical to v0.12). Opt-in users help size
the ruleset for a v0.14 default.

See `policy/README.md` and `docs/selinux.md` for activation + AVC
reporting instructions.

#### Epic 2 — `podman-docker` shim future assessment

New `docs/08-podman-docker-shim-future.md` surveys all 39 `docker` CLI
call sites across 10 files and concludes: **do not migrate off the
shim in v0.13/v0.14**. Shim is internal implementation detail, users
see zero difference, Podman community maintains `podman-docker`
long-term. Explicit trigger list documents when to revisit.

Two legacy-only `case RuntimeDocker` paths in `daemon.go` and
`runtime.go` now carry comments pointing at the decision doc.

#### Epic 3 — Nixpacks (and Paketo / Railpack / Static) in the UI

The backend multi-builder support has been in the codebase since
before v0.12 (`plugins/deploy/builders/`), but the Project Create
form silently defaulted to `""` (legacy dockerfile). The Docker
deploy-mode section now shows a **Build Type** dropdown:

- **Auto-detect** (default) — Dockerfile if present, else Nixpacks
- **Dockerfile** — the project's Dockerfile
- **Nixpacks** — language auto-detection + build (requires `nixpacks` CLI)
- **Paketo Buildpacks** — cloud-native Buildpacks via `pack`
- **Railpack** — Railway's builder
- **Static Files (Nginx)** — for HTML-only projects

Each option carries an i18n hint explaining the host-side CLI
requirement and installer command. No runtime changes — the backend
already understood all of these.

#### Version bumps
- `VERSION`                       `0.12.1` → `0.13.0`
- `web/package.json`              `0.12.1` → `0.13.0`
- Binary reports `WebCasa v0.13.0`

---

## [0.12.1] - 2026-04-19

### Patch release — Tier 1 cleanup

Collected during the v0.12.0 VPS validation week but below the bar to
block that release. No behavioural changes for working apps; this is
catalog maintenance + tooling polish.

#### Catalog cleanup
- **Removed** `sshwifty` and `scrypted` from the seed catalogue. Both
  upstream images (`niruix/sshwifty:0.4.3`, `koush/scrypted:20`) were
  deleted from docker.io before v0.12.0 and surface as `manifest unknown`
  on any install attempt. Seed corpus now 267 apps (was 269). Pinned
  tags for these had no working replacement at release time.

#### Tooling
- `scripts/compose-audit.py --check-images` (new): opt-in `skopeo inspect`
  preflight that flags any image ref whose upstream has disappeared. Adds
  an `image-unreachable` critical finding per affected app. Run it
  pre-release to catch future `sshwifty`-class regressions before they
  reach a VPS validation round. Slow (~269 network round-trips) so
  gated behind an explicit flag.

#### Internal
- Python `cleanEmptySection` in `scripts/appstore-batch-test/batch_test.py`
  now matches the Go implementation byte-for-byte. The earlier version
  advanced the cursor by one line after skipping an empty section,
  carrying blank/comment children into the output — a 1-2 line drift
  per affected compose vs what the Go renderer produces in production.
  New `plugins/appstore/cmd/sanitize-all` helper + a corpus parity
  check (6860 lines each, zero diff over all 267 apps).
- `scripts/appstore-batch-test/batch_test.py DEFAULT_APPS`: dropped
  sshwifty + scrypted; 6/8 privileged tier (was 8/8).

---

## [0.12.0] - 2026-04-19

### "Podman Only Edition" — Clean Break from Docker to Podman 5.6

v0.12 是一次向前兼容的**运行时切换**：从 Docker Engine 换成 Podman 5.6 (RHEL
AppStream)。用户安装过 v0.11 的机器需要重跑 `install.sh`；v0.11 维护与 v0.12
发布同时 EOL。v0.12 目标是在 EL9/EL10 主机上**默认零 SELinux 摩擦**，app-store
的 269 个应用零修改可用（经 4 轮 VPS 实测验证）。

> ⚠️ **Breaking change warning**: 运行时从 Docker → Podman。现有 `docker` / `docker-compose`
> CLI 通过 `podman-docker` shim 和 `podman-compose` 继续可用，**但后台引擎不同**。
> 已持有的 Docker 容器/镜像/网络/卷需按 `docs/07-podman-v0.12.md` 迁移说明重建。

#### 为什么切换

- **RHEL 10 Docker 启动失败**: iptables 内核模块 deprecated，Docker 29 报
  `Extension addrtype revision 0 not supported`，官方修复需手动 `dnf install
  kernel-modules-extra` + `modprobe`，破坏一键安装体验
- **Podman 在 RHEL 原生**: AppStream 内置、无 daemon、SELinux / systemd / cgroupv2
  深度集成、长期支持承诺
- **SDK 兼容窗口充足**: Docker Go SDK v27 + `client.WithAPIVersionNegotiation()`
  自动协商到 Podman v1.41/v1.43，WebCasa 代码零 API 层改动

#### 5 Phase 交付 + 4 轮 VPS 实测 + 5 轮 Codex Review

| Phase | 内容 |
|---|---|
| Phase 1 | install.sh Podman 改造 (AppStream + EPEL 自动启用 podman-compose) + 专用 `webcasa` 用户 + systemd CI 兼容 |
| Phase 2 | Runtime 检测层 (`plugins/docker/runtime.go`) + daemon.json Podman 感知（短路错误而非假装生效） + plugin.go 重写 |
| Phase 3 | UI + i18n + README 品牌切换 ("Docker" → runtime-aware 标签)；daemon-config 表单在 Podman 下 fieldset disabled |
| Phase 4 | App Store 静态审计 (`scripts/compose-audit.py`) — 269 apps 对 podman-compose 1.5 兼容性分析；27-app 高风险验证矩阵 |
| Phase 5 | SELinux 运维文档 + NVIDIA GPU/CDI 指引 + 4 个 Podman integration 测试 + AlmaLinux 9/10 CI 矩阵 |

#### 跨插件审计 (举一反三)

Docker plugin 全量 Codex review 后，把发现的两类 HIGH 模式扩展到全仓库：

- **WebSocket 认证**: `?token=<jwt>` URL 参数 → `Sec-WebSocket-Protocol` subprotocol
  header 全插件统一 (`auth.WSUpgradeResponseHeader` helper)；filemanager /
  database / monitoring / docker 四个 WS handler 全迁移
- **Shell pipeline 子进程 kill**: 新 `internal/execx` 包提供 `CommandContext` /
  `BashContext` 带 `Setpgid + Cancel hook + WaitDelay`，docker installer /
  backup kopia installer / firewalld installer / deploy builder / deploy cron /
  cronjob task runner / AI agent shell exec 全迁移，SSE 断开时整个 pipeline 树
  被 SIGKILL，不再留孤儿进程

#### Docker 插件安全与正确性加固

Docker 插件全量 review 发现 20 findings（2 High + 13 Medium + 5 Low），分 4 组修复：

- **Group A (安全+并发)**: WebSocket token 移出 URL；`/logs` 路由从 viewer 提到
  operator；PullImage 用 `distribution/reference.ParseNormalizedNamed` 验证；
  Volume bind 拒绝 `:` 字符；Plugin struct `sync.RWMutex` 覆盖所有可变状态；
  Install 子进程走 `exec.CommandContext` + 进程组 kill；DetectRuntime 缓存
  可失效（`ResetRuntimeCache`）
- **Group B (install SSE 生命周期)**: `AbortController` + reader cancel + mounted
  flag + timer tracking — 离开页面中途不再泄露 fetch / setState / timer
- **Group C (健壮性)**: Stack create/delete 事务化 (DB 失败时清理 on-disk
  artifacts)；`viewLogs` reqId 竞态守卫；WebSocket unmount cleanup；
  status-fetch 失败与 "runtime missing" 区分（给 retry callout 而非假装未装）
- **Group D (UX/i18n/a11y)**: `alert()` → 内联 Callout；`DockerSettings` Podman
  锁用 `<fieldset disabled>`（键盘焦点跳过，不再只挡鼠标）；Badge + 搜索行
  role + tabIndex + Enter/Space 键盘激活；3 个英文硬编码 → `t()`

#### App Store 兼容性 (269 apps)

**静态审计** (`docs/podman-compose-audit.json`):
- ✅ Clean: 234 (87%)
- 🟡 Warning: 10 (`network_mode: host` 9 个 + `gpu-reservation-ignored` 1 个 ollama-nvidia)
- 🔵 Info-only: 25 (sock mount / rootful / device passthrough)
- 🔴 Critical: 0

**VPS 实测** (Round 1 + 2 + 3，AlmaLinux 9 + Podman 5.6，25/27 apps):
- ✅ PASS: 20
- 🔴 Catalog bug: 2 (sshwifty / scrypted — 上游 image tag 已从 docker.io 删除)
- 🟡 User-config required: 3 (crowdsec SQLite race / mdns-repeater 参数 / zigbee2mqtt USB)
- ⏳ 硬件依赖未测: 2 (windows 需 KVM / ollama-nvidia 需 GPU；文档齐全)

**实测产出的生产 bug 修复** (7 个，通过 4 轮 VPS 验证发现):
- `install.sh`: `/etc/containers/registries.conf.d/999-webcasa-shortnames.conf`
  设 `short-name-mode = permissive`，否则 269 app 全部因 "short-name resolution
  enforced but cannot prompt without a TTY" 拒绝拉取
- `plugins/appstore/renderer.go SanitizeCompose`:
  - Host bind mount 自动追加 `:Z`（带 `volumes:` YAML 块上下文守卫，不误伤
    端口映射；跳过命名卷、pseudofs `/dev /sys /proc /run/udev` 和 socket）
  - 引号形式 `- "host:container"` 把 `:Z` 插到**引号内**（7 个 catalog app
    用这种写法）
  - 支持任意缩进深度的 service header（非只认 2-space）
  - 挂 docker.sock/podman.sock 的 service 自动注入 `security_opt: [label=disable]`
    （SELinux silent-deny 的唯一可行修法；已有 `security_opt` 智能 merge，
    不产生重复 key）
- `docs/selinux.md`: 修正 socket SELinux type 为 `var_run_t`（不是
  `container_var_run_t`）；场景 4 改为指向 renderer 自动注入，不再推荐被
  实测推翻的 `setsebool container_manage_cgroup`

#### Phase 5 集成测试

新 `plugins/docker/podman_integration_test.go` + CI `podman-compat` job（AlmaLinux 9/10 矩阵）:
- `TestPodmanSocketCompat` — Go SDK ↔ Podman 兼容 API 协商
- `TestDockerShimTransparency` — `docker version` / `docker compose version` 走 shim
- `TestSocketSymlinkIntegrity` — `/var/run/docker.sock` 符号链接完整性
- `TestAppStoreBatchUpDown` — 单独 `scripts/appstore-batch-test/batch_test.py`
  harness（需 `WEBCASA_RUN_PODMAN_TESTS=1` 环境变量 + 真实 Podman 主机）

#### 文档

- 新增 `docs/07-podman-v0.12.md` — 完整设计文档（决策快照、架构变更、实施路线图、风险登记册、rootless 风险分析附录、27-app 验证矩阵）
- 新增 `docs/selinux.md` — EL9/EL10 + Podman SELinux 运维指南（4 个 denial 场景 + 诊断命令 + "禁用 SELinux 是 ❌ 不要做" 段落）
- 新增 `docs/nvidia-gpu.md` — NVIDIA CDI 配置指引（ollama-nvidia 等 GPU 应用）
- 新增 `docs/podman-compose-audit.json` — 机器可读 269-app 兼容性报告
- 新增 `docs/podman-app-test-report.json` — 4 轮 VPS 实测结果报告

#### Known issues (Phase 6 候选)

- Catalog seed 两个镜像上游已删: `sshwifty` (niruix/sshwifty:0.4.3), `scrypted` (koush/scrypted:20)
- Rootless Podman 评估（v0.13+）— v0.12 采用 rootful 规避 14 个兼容性风险（详见 07-podman-v0.12.md 附录 A）
- `install.sh --gpu=nvidia` 标志自动化 NVIDIA CDI 配置 — 目前需手动 3 步
- 自定义 SELinux policy module (`webcasa_t` 域) — 当前以 `unconfined_service_t` 运行

---

## [0.11.0] - 2026-04-17

### "Safe Wins Edition" — 8 个低风险 additive-only 特性 + 6 轮 Codex Review

基于 8 个竞品分析 (Coolify/CapRover/Dokku/Dokploy/Portainer/1Panel/Dockge/Pigsty) 筛选低风险高价值模式，分 5 个 phase 交付，全部**向后兼容**、无 schema 破坏性迁移、无权限模型变动。

#### 8 个特性

| # | 特性 | 来源 | 影响 |
|---|------|------|------|
| F1 | PostgreSQL 调优预设 (OLTP/OLAP/Tiny/Crit) | Pigsty | database 插件 DBA 级调优 |
| F2 | docker run → compose.yaml 转换器 | Dockge | deploy UX 快速胜利 |
| F3 | SingleFlight 部署去重 | Portainer | 修复 5 分钟阻塞 bug，Build() 永不阻塞 |
| F4 | Gzip 响应压缩 helper | 1Panel | appstore.ListApps 启用 gzip |
| F5 | 分层限流器 (Login/TOTP/APIRead/APIWrite) | Dockge | Login 预算与 2FA 预算隔离 |
| F6 | 备份保留安全地板 (MinRetainCount) | Pigsty | 防止误配置抹掉全部历史 |
| F7 | Git polling 自动 redeploy | Portainer | 无 webhook 也能按间隔拉取检测 |
| F8 | Docker 镜像状态 badge (本地对比) | Portainer | 容器列表标记 outdated 镜像 |

#### 质量与安全加固

经过 6 轮 Codex Review (每 phase 一轮 + HIGH 模式举一反三审计 + 最终全量 sweep)，共发现并修复 **28 个 findings** (1 CRITICAL + 7 HIGH + 13 MEDIUM + 7 LOW)。每个 finding 配对应回归测试。

**关键安全修复**:
- **SSRF 防护统一**: 4 个 outbound HTTP 站点 (notify/poller/monitoring/AI) 统一使用 `ValidateWebhookURL` + `SafeDialContext` + `CheckRedirect`
- **Git 远程 SSRF 防护**: `ValidateGitRemoteTarget` 覆盖 poller / detector / appstore 三处 git clone 入口，block loopback/link-local/metadata endpoint，保留 RFC1918 私网支持
- **Poller SSH 主机密钥校验**: 从 `StrictHostKeyChecking=no` 升级到 `accept-new` + 托管 known_hosts，防 MITM
- **Build 去重**: buildLoop 原子化 pending 检查 + inflight 清除，消除 Phase 1 修复前的 "stranded pending" race

**关键正确性修复**:
- **PG Crit 预设**: 新增 `SynchronousCommit`/`FullPageWrites`/`Fsync` 三个 EngineConfig 字段并在 compose command 输出，兑现描述中的 durability 承诺
- **PG 预设最小内存**: 每预设声明 `MinMemoryMB`，防止 shared_buffers 超过容器物理内存
- **镜像状态缓存**: 世代计数器防 in-flight resolve 写回陈旧 SHA + context 取消不污染 negative cache
- **PullImage 失效时机**: `invalidatingReader` 包装进度流，Close 时触发失效，消除拉取期间缓存旧值的窗口

#### 性能与资源

- 前端主 bundle gzip 净减少 ~90KB (composerize lazy import)
- `AnnotateImageStatuses` 500ms sub-timeout，慢 Docker socket 不阻塞列表响应
- Poller worker pool (4 并发) 替代串行循环，慢 remote 不拖垮整轮
- SQL 响应 gzip 压缩 (appstore 大 JSON 负载 50KB+ → 5KB)

#### 测试

- `go test ./... -timeout 120s` 全绿
- `go test -race` clean on plugins/deploy + plugins/docker + plugins/monitoring + internal/auth + internal/handler
- `npm run build` 通过
- `npm run test:composerize` 10/10 pass

#### 依赖变更

- `golang.org/x/sync` v0.10.0 → v0.20.0 (添加 singleflight 子包)
- `composerize@1.7.5` 新增前端依赖 (lazy-loaded chunk)

---

## [v0.11 内部变更流水] Phase 5 "Docker Polish"

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

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 4 "Automation & Reliability"

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

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 3 "Database Tuning"

### F1: PostgreSQL 调优预设 (Pigsty Pattern 1)
- 新增 `plugins/database/presets_postgres.go`：4 个工作负载感知预设
  - **OLTP** — 事务密集 web 应用 (shared_buffers≈25% RAM, work_mem=4MB, max_connections=100)
  - **OLAP** — 分析查询 (shared_buffers≈40% RAM, work_mem=64MB, max_connections=50)
  - **Tiny** — 资源受限 (shared_buffers ≤256MB 上限, max_connections=20, wal_level=minimal)
  - **Crit** — 强一致性 (OLTP 内存布局 + durability 字段，初始发布时仅内存布局；durability 字段在 Phase 3 审查修复中已实装)
- 预设根据 Instance.MemoryLimit (`256m`/`1g`/`0.5g`/`512mb`) 动态推算 `EngineConfig`，写入 `Config` JSON
- `Instance` model 新增 `TuningPreset` 字段 (size:32, default `''`) 记录所选预设供审计 + 未来 memory resize 时重应用
- `CreateInstanceRequest` 新增 `tuning_preset` 字段 (postgres only)，`resolveTuningPreset()` helper 统一两个创建入口
- 新 API `GET /api/plugins/database/presets/postgres-tuning` 返回 4 个预设的元数据 (id/name/description/good_for) 供 UI 渲染
- 前端 `DatabaseInstances.jsx` 创建表单新增「Workload Tuning Preset」下拉 (engine=postgres 时显示)，选中预设时跳过手填 Config (后端推算)，i18n keys: `tuning_preset`, `tuning_preset_custom`
- 完整测试覆盖: parseMemoryLimitMB 边界 / IsValidPostgresPreset / 4 个预设的内存缩放 / 自定义 fallback / 解析失败的安全降级 / List 元数据完整性 (8 个测试用例)
- **兼容性**: 现有实例 `tuning_preset=""` 行为完全等价 (走原有 EngineConfig 路径)，无 schema 数据迁移

---

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 2 "Deploy UX Quick Win"

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
- Bundle 初始增加 ~90KB gzipped 到主 bundle (composerize 内部含 yargs-parser + deepmerge)；**Phase 2 审查修复中改为 lazy import**，主 bundle 净减少 ~90KB，composerize 独立 chunk 首次点击时加载

---

## [v0.11 内部变更流水 — 0.11.0 发布节点] 最终全量 Codex Review 修复

Pre-release sweep covering cross-phase interactions. Found 3 HIGH + 2 MEDIUM + 2 LOW issues the per-phase reviews missed.

### HIGH: 同 SHA 的 poll 可导致 webhook build 后再跑一次
- **问题**: webhook 触发 Build(X) 运行中，poller 观察到同一 SHA，调用 Build(X) 被合并为 pending。buildLoop 第一次完成后 pending=true 触发 runBuildOnce **再跑一次同样的 X**。一次 push → 两次相同构建
- **修复**: Service 新增 `IsBuildInflight(projectID)`；poller 在 trigger Build 前检查，若 inflight 则跳过(下次 tick <= 300s 内自然重试)
- **回归测试**: `TestPoller_SameSHAInFlight_NoDoubleBuild` 使用 release channel 冻结第一个构建，期间 poller 触发 — 期望 buildCalls=1 (而非 2)

### HIGH: detector.go git clone 无 SSRF 校验
- **问题**: `POST /api/plugins/deploy/detect` 接受用户 URL 直接 `git clone`。Phase 4 修了 poller 但遗漏同级的 detector
- **修复**: `DetectFrameworkFromURL` 调用 `validateGitPollTarget` 前置校验

### HIGH: appstore 源 URL 无 SSRF 校验 (每 6h 周期性探测向量)
- **问题**: admin 添加的 App Store 源 URL 直接 `git clone`/`git pull`，background updater 每 6h 拉取一次。一次恶意 URL 可变成周期性内网探测
- **修复**:
  - `plugins/deploy/poller.go` 导出 `ValidateGitRemoteTarget(url)` 公开入口
  - `plugins/appstore/source.go` `AddSource` 入口 + `gitSync` clone 前双重校验
  - 校验发生在 DB 记录创建**之前**，阻止坏 URL 写入

### MEDIUM: DockerOverview 异步关闭 race
- **问题**: `convertDockerRun` 在 composerize lazy-import 期间若用户关闭对话框，Phase 2 修的 `useEffect(!open)` reset 先跑，随后 `await` 返回时 `setComposeFile/setActiveTab/setConvertError` 仍会触发 — stale state 重新注入
- **修复**: `openRef = useRef(open)` 快照；async 完成路径检查 `openRef.current`，关闭期间结果直接丢弃

### MEDIUM: Poller 测试 bypass pollOne 真实路径
- **问题**: `TestPoller_TriggersBuildOnNewCommit` 和 `TestPoller_ConfigChangedDuringPoll_SkipsBuild` 都内联重现了 pollOne 的判断逻辑而非调用 pollOne — 修复失效则这两个测试仍绿
- **修复**: `Poller` 新增 `lsRemoteFn` 注入字段 (生产为 nil)。测试通过 stub 驱动真实 `pollOne()` 端到端，真正 pin 住 TOCTOU 修复

### LOW: composerize.test.mjs 没有 npm script 挂接
- **修复**: `web/package.json` scripts 新增 `test:composerize`。CI/发布 checklist 可加上 `npm run test:composerize`

### LOW: roadmap/changelog 文档与实际实施不符
- **修复**:
  - `docs/06-feature-roadmap-v0.11.md` F8 章节重写，反映最终"纯本地对比"实施而非"分层 + registry digest"计划
  - `changelog.md` Crit 预设预留声明 / composerize bundle ~25KB 声明更新为 Phase 审查修复后的实际状态

### Codex 核验为干净的项
- Phase 1 buildLoop 原子 pending 检查 + inflight 清除：正确
- Phase 5 世代计数器 locking：正确
- PG 预设 validation 在文件/DB 半创建前失败：正确
- AutoMigrate 覆盖 deploy/database/backup 的新列：正确
- 4 个 hardened HTTP client 站点 (notify/poller/monitoring/AI)：一致

### 验证
- `go test ./... -timeout 120s` 全绿
- `go test -race` clean on plugins/deploy + plugins/docker + plugins/monitoring
- `npm run build` 绿
- `npm run test:composerize` 10/10 pass

---

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 5 审查修复

基于 Codex Review 发现的 3 个问题修复 (2 MEDIUM + 1 LOW)。

### MEDIUM: PullImage 缓存失效时机错误 — badge 可滞后 5 秒
- **问题**: `PullImage(ctx, ref)` 返回进度 reader 后立即调用 `invalidateImageStatusCache()`。但拉取**实际发生在 reader 被消费时** — 此时并发 `ListContainers` 能 inspect 到旧 SHA 并缓存它。拉取结束后缓存仍保留旧值直到 cacheTTL (5s) 过期
- **修复**: 新 `invalidatingReader` 类型包装进度 reader，`Close()` 时触发一次性失效。调用方 `defer rc.Close()` 自然触发在拉取完成之后
- 同时防 double-close 两次 invalidate (使用 `closed bool` 标志)
- **回归测试**: `TestInvalidatingReader_InvalidatesOnClose`

### MEDIUM: In-flight resolveTagImageID 可将过期 SHA 写回缓存
- **问题**: Goroutine A 正在 Inspect (慢) — Goroutine B 完成 PullImage 调用 Invalidate 清空缓存 — A 的 Inspect 返回**pull 前的旧 SHA** — A 把旧 SHA 写回缓存。PullImage 后续 ListContainers 读到旧 SHA，badge 显示"updated"而实际 outdated
- **修复**: `imageStatusCache` 新增 `gen uint64` 世代计数器。`Invalidate` 增加 gen；`resolveTagImageID` 在 inspect 前 snapshot gen，写回时对比，不一致则丢弃结果 (让下次调用重新 inspect)
- **回归测试**: `TestImageStatusCache_InvalidateDuringResolve_RejectsStaleWrite` — 用 blocker channel 人为冻结 inspect 中段，期间调用 Invalidate，验证释放后 cache **不含**陈旧 entry

### LOW: context 取消/超时错误污染缓存 5 秒
- **问题**: handler 的 500ms sub-deadline 过期时，`ImageInspectWithRaw` 返回 `context.DeadlineExceeded`。原代码将空字符串 negative-cache 5 秒 → 后续 ListContainers 看到 "unknown" 即使 Docker socket 正常
- **修复**: `resolveTagImageID` 检测 `errors.Is(err, context.Canceled)` 和 `errors.Is(err, context.DeadlineExceeded)`，直接返回 "" **不写缓存**，允许下次请求立即重试
- **回归测试**: `TestImageStatusCache_ContextErrorNotNegativeCached` 验证超时错误后 cache 无条目，follow-up 请求立即 inspect 并缓存正确结果

### 非 bug (Codex 已核验)
- `ImageInspectWithRaw` 返回 `([]byte, ...)` 非 ReadCloser，无需关闭
- ImageID 格式双方一致 (均为 `sha256:hex`)，无误报 outdated
- 500ms timeout 下部分失败降级为 "unknown" 是预期行为
- 前端只显示 outdated 隐藏 unknown 是设计选择
- 包级 defaultImageStatusCache 在单 daemon 场景下线程安全；多 daemon 是未来问题

---

## [v0.11 内部变更流水 — 0.11.0 发布节点] HIGH-pattern 举一反三审计修复

基于 Phase 1-4 Codex Review 中 HIGH 类问题的模式提取，对整个代码库做一轮系统性搜索，发现并修复 **2 处相同 class 的 SSRF 缺失**。

### 模式识别
从已修复的 4 个 HIGH 归纳 4 个 pattern：
1. defer-based cleanup 与 atomic exit decision 冲突 (Phase 1)
2. TOCTOU: read DB → 异步 I/O → write DB by ID 无 re-read (Phase 4)
3. 相同操作的安全策略不一致 (Phase 4 SSH)
4. **用户可控 URL/输入直接喂给 subprocess/outbound 请求无验证** (Phase 4 GitURL SSRF)

### HIGH: `plugins/monitoring/alerter.go` 告警 webhook 无 SSRF 防护
- **问题**: `sendWebhook(url, ...)` 直接将 admin 配置的 `AlertRule.NotifyURL` 喂给 `http.Client.Post`，无 URL 校验 / 无 CheckRedirect / 无 SafeDialContext。即使 admin-only 配置路径，`http://169.254.169.254/` 等 metadata endpoint 仍可被 (误) 配置
- **对比**: 同项目 `internal/notify/notifier.go` 对完全相同场景 (webhook 通知) 有完整三层防护 — 这是 pattern 3 (**不一致的安全策略**) 和 pattern 4 (**未验证 URL**) 的 double hit
- **修复**:
  - 调用 `notify.ValidateWebhookURL(url)` 做 URL 层拒绝
  - `http.Client` 添加 `CheckRedirect` 拒绝重定向 (否则通过 302 到内部 IP 绕过 URL 验证)
  - `Transport: &http.Transport{DialContext: notify.SafeDialContext}` 做 dial 时二次校验 (防 DNS rebinding)
- **回归测试**:
  - `TestSendWebhook_SSRFBlocked` 覆盖 127.0.0.1 / localhost / 169.254.169.254 / [::1] / ftp:// 5 种场景
  - `TestSendWebhook_PrivateRFC1918Allowed` 确认自托管内网 (10.0.0.5) 仍可用

### MEDIUM: `plugins/ai/{client,embedding}.go` 缺少 redirect/dial SSRF 防护
- **问题**: admin 配置的 LLM `base_url` 若经 302 跳转到内部 IP，客户端透明跟随 — 泄露 Authorization bearer (API key) 及 prompt 内容到本地网络。同 pattern 4 的 defense-in-depth 版本
- **修复**: 两个客户端的 `http.Client` 添加 `CheckRedirect` + `Transport{DialContext: notify.SafeDialContext}`。因 baseURL 由 admin 信任配置，不加 URL 层 `ValidateWebhookURL` (过度限制合法公共 API endpoint)

### 其他审计结果 (已判定为非 bug)
- `internal/queue/queue.go`: defer semaphore release 无 state 一致性问题
- `internal/caddy/manager.go`: reload coalescing 在 lock 内 swap timer 干净
- `plugins/backup/service.go` UpdateConfig TOCTOU: admin-only 配置更新，影响限于配置短暂不一致 (非数据丢失)，v0.11 暂不处理
- `internal/versioncheck/checker.go`: URL 硬编码非用户可控，无 SSRF 向量

### 复盘
- **Pattern 4** 出现 3 次了 (notify original → Phase 4 git poller → 本次 monitoring + AI)。建议 v0.12 建立**项目约定**: 所有面向用户可配 URL 的 outbound HTTP 客户端 MUST 通过共享 helper 创建。考虑在 `internal/notify/` 加 `NewHardenedHTTPClient(timeout)` 或 `internal/httpclient/` 新包
- 监控/AI/通知三处 webhook 语义几乎相同但各自独立实现 — 未来重构应合并为单一 HardenedWebhookDispatcher

---

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 4 审查修复

基于 Codex Review 发现的 5 个问题修复 (3 HIGH + 2 MEDIUM)。F6 (备份保留地板) 本次 review 无发现。

### HIGH: Poller TOCTOU — 过时 Project 快照触发错误 Build
- **问题**: `runOnce` 一次性读完所有启用项目，然后逐个调用 `pollOne` 做 git ls-remote。若用户在网络 I/O 期间禁用 polling 或改 URL，旧配置的结果会触发新配置的 build，并覆盖 `LastPolledAt`
- **修复**: `pollOne` 在 `svc.Build()` 前 **重新读取** Project。若 `GitPollEnabled=false`、或 `GitURL`/`GitBranch` 已变，跳过本轮触发
- **回归测试**: `TestPoller_ConfigChangedDuringPoll_SkipsBuild`

### HIGH: Poller SSH 禁用了主机密钥校验 (MITM 窗口)
- **问题**: `configureGitSSH` 使用 `StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null` — 接受任何 host key，允许 MITM。同项目的 build path (`plugins/deploy/git.go:150`) 使用安全的 `accept-new` + 托管 `~/.ssh/known_hosts`
- **修复**: 改为 `StrictHostKeyChecking=accept-new -o UserKnownHostsFile=~/.ssh/known_hosts`，与 build path 对齐。保留 `IdentitiesOnly=yes` 避免使用非本次 key

### HIGH: GitURL 未校验 — SSRF 向量
- **问题**: 项目创建/更新接受任意 `git_url`，poller 直接喂给 git 子进程。恶意用户可配置 `http://169.254.169.254/...` 或 `ssh://127.0.0.1/...` 等 URL 让 WebCasa 每隔 5 分钟探测内部网络
- **修复**: 新增 `validateGitPollTarget(url string) error`:
  - scheme 白名单: `https / http / ssh / git`；拒绝 `file://` / `ftp://` 等
  - 识别 scp-style `git@host:path` 形式
  - 解析 literal IP 或 DNS — 拒绝 loopback / link-local / metadata endpoint (169.254/16)
  - **允许 RFC1918 私网** — 自托管 Gitea/GitLab 继续可用
- **回归测试**: `TestValidateGitPollTarget` 11 个子用例覆盖 public git / 私网 / 被阻止 scheme / 各类被阻止 IP / 空串

### MEDIUM: Poller 串行循环 — 慢 remote 拖垮整轮
- **问题**: 单 goroutine 顺序执行 `ls-remote`，每个可能阻塞最多 15s。100 项目 + 任一慢 remote → 后续全部延迟。`Stop()` 关闭 stop chan，但 `lsRemoteHead` 使用 `context.Background()` → 关机期间 in-flight 请求继续跑完
- **修复**:
  - `Poller` 引入 `ctx/cancel`，替代原 `stop` chan；`Stop()` 通过 `cancel()` 通知所有层级
  - `lsRemoteHead` 签名加 `ctx context.Context`，子进程和超时都派生自 stop-scoped context
  - `runOnce` 用 `pollerConcurrency=4` 有界 worker 池并发执行
  - worker 池 select 同时监听 `p.ctx.Done()`，关机时立即退出

### MEDIUM: runBuild 空 Commit 时 LastDeployedCommit 不更新
- **问题**: 若 `Builder.Build()` 返回 `Success=true, Commit=""` (例如 `GetCommitHash` 失败但构建完成)，`runBuild` 跳过 `LastDeployedCommit` 更新 → poller 每次轮询都以为 "远端有新 commit"，触发无限 redeploy 循环 (受最小间隔 60s 限制)
- **修复**: build 成功但 commit 为空时，fallback 读取本地 checkout 的 `git rev-parse HEAD` (`s.git.GetCommitHash(project.ID)`)。仍失败则记警告日志，但不破坏 build 成功语义

### 测试
- 新增 `TestValidateGitPollTarget` 11 子用例
- 新增 `TestPoller_ConfigChangedDuringPoll_SkipsBuild`
- 现有 `TestPoller_TriggersBuildOnNewCommit` / `TestPoller_RespectsInterval` 继续通过
- `go test -race` 在 plugins/deploy 包通过 (1.8s)

---

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 3 审查修复

基于 Codex Review 发现的 5 个问题修复 (1 CRITICAL + 1 HIGH + 2 MEDIUM + 1 LOW)：

### CRITICAL: Crit 预设描述与实现不一致 — 虚假安全承诺
- **问题**: `ListPostgresPresets()` Crit 描述声称 `synchronous_commit=on, full_page_writes=on, fsync=on`，但 `pgCrit()` 未输出任何这些字段 — `EngineConfig` 连字段都没有。用户选 Crit 以为获得强持久性保证，实际拿到的是 OLTP + 更高 work_mem
- **修复**:
  - `EngineConfig` 新增 `SynchronousCommit string`, `FullPageWrites *bool`, `Fsync *bool` 三个字段
  - `compose.go` `buildPostgresCommand()` 输出对应 `-c synchronous_commit=... -c full_page_writes=... -c fsync=...` 参数；新增 `boolToOnOff()` helper
  - `pgCrit()` 实际设置三个字段
- **回归测试**:
  - `TestApplyPostgresPreset_Crit_EmitsDurabilityFields` 断言 Crit 返回的 config 含 3 个 durability 字段
  - `TestListPostgresPresets_CritDescriptionMatchesImplementation` **meta-test**: 扫描 Crit 描述中承诺的字段，确认实现全部输出。未来如移除任一字段但忘记更新描述，此测试立即失败

### HIGH: Preset 内存地板可产生超过容器限制的设置
- **问题**: OLTP 有 64MB shared_buffers 下限、OLAP 128MB、Tiny 32MB。容器 `memory_limit=60m` 时 OLTP/OLAP 的 shared_buffers 占比过高 → PG 启动失败或 OOM
- **修复**:
  - 移除所有固定下限，`shared_buffers` 严格按百分比计算
  - 每个预设声明最小内存 (`minMemOLTP=256 / minMemOLAP=512 / minMemCrit=256 / minMemTiny=64`)
  - `ApplyPostgresPreset` 内存低于最小值时返回描述性错误，**不再 silent fallback**
  - `PostgresPresetInfo` 新增 `MinMemoryMB` 字段暴露给前端 UI 供客户端预校验
- **签名变化**: `ApplyPostgresPreset(...) *EngineConfig` → `(*EngineConfig, error)`，`resolveTuningPreset()` 传播错误 → 400 响应
- **回归测试**: `TestApplyPostgresPreset_BelowMinimumMemoryRejects` 覆盖 4 个预设各自的最小内存拒绝 / `TestApplyPostgresPreset_Tiny_ProportionalNoFloor` 验证 Tiny 按比例缩放

### MEDIUM: 预设选中时 UI 仍显示可编辑 Advanced 字段
- **问题**: `resolveTuningPreset` 在预设生效时 override `req.Config`，但前端创建表单仍渲染可编辑的 shared_buffers/work_mem/wal_level 字段。用户编辑后 payload 包含 `config`，被前端 `if (!payload.tuning_preset)` 逻辑丢弃 — 用户编辑**静默被忽略**
- **修复**: `DatabaseInstances.jsx` 高级配置区块条件从 `form.engine` 改为 `form.engine && !(form.engine === 'postgres' && form.tuning_preset)` — 选中 postgres 预设时整个 Advanced 折叠入口隐藏，视觉表达"预设接管调优"

### MEDIUM: 不支持的内存单位静默 fallback 到 512MB
- **问题**: `1Gi` / `1T` / `1_000m` 全部解析为 0，然后 fallback 到 512MB。K8s 用户发送 `1Gi` 期望 1024MB，实际得到 512MB 预设 — 与 `memory_limit` 指示的容器大小不一致
- **修复**:
  - 新增 `parseMemoryLimitMBStrict(s)` 严格模式，支持:
    - 传统单位: `k/m/g/t` + 可选 `b` 后缀 (`256m`, `1gb`, `2T`)
    - IEC 二进制单位: `Ki/Mi/Gi/Ti` (`512Mi`, `1Gi`, `2Ti`)
    - 无后缀默认 MB
  - 未识别单位 / 负值 / 空串返回 `error`，`ApplyPostgresPreset` 直接传播
  - 保留兼容性 `parseMemoryLimitMB(s)` (忽略错误，返回 0)
- **回归测试**: `TestParseMemoryLimitMBStrict` 覆盖 5 good + 5 bad 用例；`TestParseMemoryLimitMB` 扩充 IEC + T 单位

### LOW: 测试覆盖不足
- **修复**: 上述 4 个修复各自补充回归测试；测试用例数从 8 → 12

---

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 2 审查修复

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

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 1 审查修复

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

## [v0.11 内部变更流水 — 0.11.0 发布节点] Phase 1 "Core Infrastructure"

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
