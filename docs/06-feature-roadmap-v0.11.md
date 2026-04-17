# Web.Casa v0.11 "Safe Wins Edition" 开发方案

**版本**: 0.11.0
**规划日期**: 2026-04-17
**预估工作量**: ~17-18 人日 (单人 3-4 周，双人 2 周)
**定位**: 从 8 个竞品分析中筛选**低风险、additive-only** 的高价值模式，完成一轮稳健打磨

---

## 设计原则

本版本刻意规避重大重构和破坏性变更，所有特性遵守：

1. **Additive-only** — 只新增字段/端点/配置，不修改现有 API 契约
2. **默认不改变现有行为** — 新功能 opt-in，未配置则与 v0.10.0 行为等价
3. **无二进制依赖** — 不引入 pgBackRest/PgBouncer/pg_exporter 等外部工具链
4. **无 schema 物理分裂** — 不改 SQLite 数据分片结构
5. **无权限模型变动** — 4 层 RBAC 保持不变

---

## Phase 划分

开发分 5 个 Phase 进行，每 Phase 独立 feature branch + PR，全部合并后打 `v0.11.0`。

| Phase | 主题 | 特性 | 工作量 | 依赖 | 可独立发布 |
|-------|------|------|--------|------|-----------|
| **Phase 1** | 核心基础设施层 | F3 + F4 + F5 | 3.5 天 | — | 无 (内部工具) |
| **Phase 2** | Deploy 插件 UX 快速胜利 | F2 | 2 天 | — | ✅ |
| **Phase 3** | Database 插件调优 | F1 | 3 天 | — | ✅ |
| **Phase 4** | 自动化与可靠性 | F7 + F6 | 6 天 | Phase 1 (F3) | ✅ |
| **Phase 5** | Docker 插件打磨 | F8 | 3 天 | — | ✅ |

### Phase 1: 核心基础设施层 (3.5 天)

**目标**: 为后续 Phase 铺底，提供可复用的横切能力

- F3 SingleFlight 部署去重 (0.5 天) — `plugins/deploy/service.go` 替换合并逻辑
- F4 Gzip 响应压缩 helper (1 天) — `internal/handler/helper.go` 新增 `SuccessGzipped()`
- F5 分层限流器服务 (2 天) — `internal/auth/ratelimit.go` 场景化重构

**Phase 里程碑**:
- `go test ./internal/... -timeout 60s` 绿色
- `/api/plugins/appstore/apps` 启用 gzip 实测压缩率 >80%
- 并发 10 次 redeploy 同 stack 只执行 1 次 build
- Login 限流 5/min、TOTP 10/5min 生效

**Feature branch**: `feat/v0.11-phase1-infra`

---

### Phase 2: Deploy 插件 UX 快速胜利 (2 天)

**目标**: 纯前端可见价值，v0.11 第一个用户可感知功能

- F2 docker run → compose 转换器 (2 天) — `web/` 集成 composerize，部署页新增导入入口

**Phase 里程碑**:
- 粘贴 `docker run -d -p 80:80 nginx` → 生成合法 compose.yaml → 一键部署成功
- env/port/volume/network 4 类场景转换 E2E 通过

**Feature branch**: `feat/v0.11-phase2-docker-run-import`

---

### Phase 3: Database 插件调优 (3 天)

**目标**: PG 实例从"能跑"到"可调优"，DBA 生产力

- F1 PG 调优预设 (3 天) — 4 个 preset 模板 + Instance model 加字段 + UI 下拉

**Phase 里程碑**:
- 4 个 preset (OLTP/OLAP/Tiny/Crit) 在 4GB RAM 节点 `shared_buffers`/`work_mem` 实测生效
- 现有实例 `tuning_preset="custom"` 保持配置不变
- PG14/15/16/17 兼容性矩阵通过

**Feature branch**: `feat/v0.11-phase3-pg-presets`

---

### Phase 4: 自动化与可靠性 (6 天)

**目标**: 运维自动化 + 备份合规性，最重的 Phase

- F7 Git polling 自动 redeploy (3 天) — `plugins/deploy/poller.go` + Project model 加字段 + UI 开关
- F6 分层备份保留策略 (3 天) — Schedule model 加 `*int` 三层字段 + `enforceRetention()` 优先级应用

**Phase 里程碑**:
- Git push 5 秒内检测到变更并触发 deploy
- webhook + polling 并发触发只执行 1 次 build (依赖 Phase 1 F3)
- 10 备份 + tier count=5 → 保留最新 5 个
- 旧 `RetentionDays` 与新 tier 字段叠加执行不误删

**依赖**: **Phase 1 F3 必须先合并**

**Feature branch**: `feat/v0.11-phase4-automation`

---

### Phase 5: Docker 插件打磨 (3 天)

**目标**: 容器管理体验精细化，v0.11 收尾

- F8 镜像状态分层缓存 (3 天) — `plugins/docker/imagestatus.go` 双层缓存 + 容器列表响应加字段 + UI 红点

**Phase 里程碑**:
- 100 容器列表响应 <500ms
- `docker pull nginx:latest` 后老容器标记 `outdated`
- Registry 超时不阻塞主响应，5s HTTP 超时 + 失败 60s 负缓存防风暴

**Feature branch**: `feat/v0.11-phase5-image-status`

---

### 时间线示意 (单人节奏)

```
Week 1: ████▓▓░░░░░░░░░░░░░░░░  P1 (3.5d) + P2 start (1.5d)
Week 2: ▓░███░░████░░░░░░░░░░░  P2 end (0.5d) + P3 (3d) + P4 start (1.5d)
Week 3: ██████░░░░░░░░░░░░░░░░  P4 continue (5 of 6d)
Week 4: ▓░███▓▓▓░░░░░░░░░░░░░░  P4 end (0.5d) + P5 (3d) + 回归测试
```

### Pre-tag 策略 (可选)

每 Phase 合并后可打 alpha tag 便于灰度测试：

- `v0.11.0-alpha.1` (Phase 1 合并后)
- `v0.11.0-alpha.2` (Phase 2 合并后)
- ... 直至 `v0.11.0` (Phase 5 合并后正式发布)

若不需灰度可省略 alpha tag，只在 Phase 5 结束后打 `v0.11.0`。

---

## 特性列表

### F1: PostgreSQL 调优预设 (Pigsty Pattern 1)

**工作量**: 3 天 | **影响插件**: `plugins/database/`

**范围**:
- 新增 `plugins/database/presets/` 目录，4 个 Go text/template 文件:
  - `oltp.tmpl` — 事务密集 (shared_buffers=25%, work_mem=4MB, max_parallel_workers=2)
  - `olap.tmpl` — 分析工作负载 (shared_buffers=40%, work_mem=64MB, max_parallel_workers=CPU/2)
  - `tiny.tmpl` — 资源受限 (shared_buffers=128MB, max_connections=20)
  - `crit.tmpl` — 强一致性 (synchronous_commit=on, full_page_writes=on)
- Instance model 增字段 `tuning_preset string` (默认 `custom` 保持现有行为)
- 容器启动时根据预设生成 `postgresql.conf` 片段并挂载
- UI: 创建 PG 实例时下拉选择 + 每项悬浮说明

**兼容性**:
- 现有实例 `tuning_preset="custom"` = 不注入任何配置 = 行为等价
- 选择 preset 后首次生效需重启容器，UI 明确提示

**验收**:
- 单元测试: 4 个模板渲染正确
- 集成测试: 基于 4GB RAM 节点，每个 preset 实测 `shared_buffers`/`work_mem` 生效
- 文档: 每个 preset 的适用场景 + 参数依据

---

### F2: docker run → compose.yaml 转换器 (Dockge Pattern 3)

**工作量**: 2 天 | **影响插件**: `plugins/deploy/` + `web/`

**范围**:
- 前端方案: 引入 `composerize` npm 包到 `web/package.json`
- UI: `plugins/deploy/` 项目创建页新增"从 docker run 命令导入"入口
  - 输入框粘贴 `docker run ...`
  - 点击转换 → 生成 compose.yaml 预览 → 确认后作为新项目模板
- 后端零改动 (纯前端特性)

**兼容性**:
- 纯新 UI 入口，不影响现有项目
- `composerize` 库约 200KB 压缩后，bundle 大小可接受

**验收**:
- 单元测试 (前端): 常见 docker run 命令 (env/port/volume/network) 转换正确
- E2E: 复制 nginx/postgres 官方 README 的 docker run，一键转 compose 并部署成功

---

### F3: SingleFlight 部署去重 (Portainer Pattern 3)

**工作量**: 半天 | **影响插件**: `plugins/deploy/`

**范围**:
- 替换 `plugins/deploy/service.go` 现有 build 合并逻辑 (`ErrBuildCoalesced` 路径)
- 引入 `golang.org/x/sync/singleflight` 依赖
- 包级 `var deployGroup singleflight.Group`
- `Deploy(stackID)` 包在 `deployGroup.Do(stackID, ...)` 中
- 保留外部行为 (同时触发 → 第二个等待第一个结果)

**兼容性**:
- 对外 API 等价，`ErrBuildCoalesced` 不再抛出 (现在是"共享结果"而非"拒绝")
- 现有测试需更新：断言"第二次触发得到相同结果"而非"被 coalesced"

**验收**:
- 并发 10 个 redeploy 同一 stack，只执行 1 次 build
- webhook + 手动触发并发，共享结果

---

### F4: Gzip 响应压缩 Helper (1Panel Pattern 4)

**工作量**: 1 天 | **影响**: `internal/handler/`

**范围**:
- 新增 `internal/handler/helper.go` 的 `SuccessGzipped(c *gin.Context, data interface{})` 函数
- 内部使用 `compress/gzip` + `Accept-Encoding: gzip` 协商
- 响应头: `Content-Encoding: gzip`
- 显式调用，**不作为全局中间件**（避免影响流式响应）
- 优先使用场景:
  - `GET /api/plugins/appstore/apps` (大 JSON 列表)
  - `GET /api/plugins/docker/containers` (多容器场景)
  - 其他 >10KB 响应

**兼容性**:
- 只有显式调用 `SuccessGzipped` 的端点生效，其他端点行为不变
- 客户端不支持 gzip 时 fallback 到未压缩响应

**验收**:
- 大响应 (50KB JSON) 传输大小降至 ~5KB
- `curl -H "Accept-Encoding: gzip"` 得到 gzip 响应头

---

### F5: 分层限流器服务 (Dockge Pattern 2)

**工作量**: 2 天 | **影响**: `internal/auth/`

**范围**:
- 重构 `internal/auth/ratelimit.go` 为 **场景化预实例化** 模式
- 启动时创建:
  - `LoginLimiter` — 5 次/分钟 (防暴破)
  - `APIReadLimiter` — 300 次/分钟 (常规 GET)
  - `APIWriteLimiter` — 60 次/分钟 (POST/PUT/DELETE)
  - `TOTPLimiter` — 10 次/5 分钟 (防 2FA 暴破)
- 路由挂载时显式选择限流器: `r.POST("/login", LoginLimiter.Middleware(), loginHandler)`
- 保留通用 `DefaultLimiter` 兼容未显式选择的路由

**兼容性**:
- 现有路由继续走 `DefaultLimiter` = 行为等价
- 敏感路由 (login/TOTP) 显式升级到更严格限流

**验收**:
- 单元测试: 各限流器按阈值拒绝超量请求
- 集成测试: 100 并发登录请求，只前 5 个通过

---

### F6: 分层备份保留策略 (Pigsty Pattern 7)

**工作量**: 3-4 天 | **影响插件**: `plugins/backup/`

**范围**:
- `plugins/backup/model.go` Schedule 模型新增字段 (所有 `*ptr` 可空):
  - `RetentionTierCount *int` (按数量层)
  - `RetentionTierAgeDays *int` (按时间层)
  - `RetentionTierTotalSizeMB *int` (按总量层)
- 现有 `RetentionDays` 字段保留 (向后兼容)
- `enforceRetention()` 按优先级应用: 先按 count → 再按 age → 最后按 size
- UI: 备份策略编辑页新增 "高级保留" 折叠区域，三层分别配置

**兼容性**:
- 未设置新字段 = 继续走旧 `RetentionDays` 逻辑 = 行为等价
- 若设置新字段则新旧逻辑**叠加**执行 (更严格保留，不误删)

**验收**:
- 单元测试: 10 个备份 + tier count=5 → 保留最新 5 个
- 单元测试: 10 个备份 (5 老 5 新) + tier age=30 → 保留近 30 天
- 集成测试: 三层全配时优先级正确

---

### F7: Git Polling 自动 Redeploy (Portainer Pattern 8)

**工作量**: 3 天 | **影响插件**: `plugins/deploy/`

**范围**:
- `plugins/deploy/model.go` Project 模型新增:
  - `GitPollEnabled bool`
  - `GitPollIntervalSec int` (默认 300 = 5 分钟)
  - `LastDeployedCommit string`
- 新增 `plugins/deploy/poller.go`:
  - 后台定时 goroutine (per project)
  - 每间隔 git `fetch` 并对比 `HEAD` commit hash
  - 变化则调用现有 `Deploy(projectID)` (复用 F3 的 SingleFlight)
- UI: 项目设置页新增 "Git 轮询" 开关 + 间隔选择 (1/5/15/60 分钟)

**兼容性**:
- 默认 `GitPollEnabled=false` = 不启用轮询 = 行为等价
- 已有 webhook 机制不变，轮询与 webhook 并行互补
- F3 SingleFlight 保证 webhook + 轮询并发时只 build 一次

**验收**:
- 集成测试: 启用轮询 → 推送新 commit → 5 秒内检测到变更并触发 deploy
- 并发测试: webhook 和轮询同时触发 → 只执行 1 次 build

---

### F8: 镜像状态分层缓存 (Portainer Pattern 5)

**工作量**: 3 天 | **影响插件**: `plugins/docker/`

**范围**:
- 新增 `plugins/docker/imagestatus.go`:
  - 本地镜像 digest 缓存 (24 小时 TTL)
  - 远程 registry digest 缓存 (5 秒 TTL)
  - Goroutine-safe (`sync.Map` 或 `hashicorp/golang-lru`)
- 容器列表响应新增可选字段 `imageStatus: "updated"|"outdated"|"error"|"unknown"`
- 对比逻辑:
  - 从 registry HEAD 请求获取 `Docker-Content-Digest`
  - 对比容器当前镜像 digest
  - 一致 = `updated`, 不一致 = `outdated`, 拉取失败 = `error`
- UI: 容器列表"状态"列显示红点指示过时镜像

**兼容性**:
- 响应字段新增，旧前端忽略该字段 = 行为等价
- 缓存未命中时不阻塞主响应 (异步填充)
- 首次冷启动 5 秒内 `imageStatus="unknown"` (缓存填充中)

**验收**:
- 单元测试: 缓存 TTL 生效 (24h 后刷新本地 digest)
- 集成测试: `docker pull nginx:latest` 后老容器显示 `outdated`
- 性能测试: 100 容器列表响应 <500ms (registry 并发查询)

---

## 跨特性工作

### 测试基础设施增强

- `go test ./... -timeout 120s` 全量跑通为合并门槛
- 每个 PR 附带新测试 (F1/F2/F5/F6/F7/F8 均有单元+集成测试要求)
- CI 矩阵: AlmaLinux 9 + 10 双版本验证

### 文档更新

- `changelog.md`: 每特性合并时追加 0.11.0 条目
- `docs/06-feature-roadmap-v0.11.md`: 本文档 (规划)
- `stack.md`: F7 引入 `golang.org/x/sync/singleflight` 依赖需记录
- `README.md`: F1/F7 面向用户的新能力简述

### 版本号

- `VERSION`: 0.11.0
- `web/package.json`: 0.11.0

---

## 开发顺序建议

遵循 **Phase 划分** 章节定义的 5 阶段顺序，每 Phase 独立 feature branch 合并至 main，避免并行带来的 merge 冲突。

Phase 4 F7 依赖 Phase 1 F3 (SingleFlight)，因此 Phase 1 必须先完成。其他 Phase 无依赖，可按进度灵活调整顺序。

---

## 风险与未决项

### 已识别的小风险

1. **F7 Git polling 资源消耗**: 100 个项目 × 5 分钟轮询 = 20 次/分钟 git fetch。
   - **缓解**: 轮询间隔最低限制 60 秒；goroutine 复用 (全局 ticker 分发，而非 per-project goroutine)
2. **F8 Registry 访问超时**: Docker Hub 限流 / 私有 registry 认证失败。
   - **缓解**: 5 秒 HTTP 超时 + 失败标记 `error` 并缓存 60 秒避免风暴
3. **F2 composerize 前端依赖**: npm 包维护度需验证。
   - **缓解**: 若 `composerize` 长期未维护，fallback 到 `composerize-ts` 或自写轻量 parser
4. **F1 PG 参数与版本兼容**: PG14 不支持的参数在 PG13 可能报错。
   - **缓解**: 仅支持 PG14+ (与 Pigsty 一致)；版本探测后条件渲染

### 推迟到 v0.12+ 的大特性

以下在 v0.11 risk 分析中被标记为**中高风险**，推迟处理:

- 统一 WebSocket 流式 + Socket handler 注册表 (耦合度高)
- 任务回滚链系统 (需 in-flight 升级策略)
- PG 角色模板 + PgBouncer + pg_exporter + PITR (需二进制依赖规划)
- HTTP Transport RBAC 中间件 (需 label backfill 策略)
- 多 DB 连接分片 + 任务日志文件隔离 (需 schema 迁移方案)
- i18n embed + 错误响应 i18n key (需前端协同重构)

v0.12 将专门规划其中 3-4 个中风险项，每个配独立设计文档和迁移方案。

---

## 发布标准

### Phase 级合并准则 (每 Phase 合并 main 前)

- [ ] Phase 内所有特性完成并通过 self-review
- [ ] Phase 里程碑验收项全部通过
- [ ] Phase 相关单元/集成测试新增并绿色
- [ ] `changelog.md` 追加 Phase 对应条目 (draft 状态)

### v0.11.0 正式发布准则 (Phase 5 合并后)

- [ ] 5 个 Phase 全部合并到 `main`
- [ ] 全量 `go test ./... -timeout 120s` 绿色
- [ ] CI (AlmaLinux 9/10 矩阵) 绿色
- [ ] 核心 UX 路径手动回归 (登录 / 创建项目 / 部署 / 备份 / 监控)
- [ ] 从 v0.10.0 升级测试 (现有 SQLite + 配置文件在 v0.11 正常运行)
- [ ] `changelog.md` v0.11.0 条目完整
- [ ] VPS 实机验证新特性 (F1 调优生效、F7 轮询触发、F8 状态显示)
- [ ] `VERSION` 和 `web/package.json` 更新为 `0.11.0`

---

## 决策快照

| 维度 | 决策 |
|------|------|
| 版本号 | 0.11.0 |
| 发布窗口 | 2026-05 中旬 (4 周后) |
| 开发人员 | 单人/双人 |
| Scope 冻结 | 本文档定稿后不加新特性 (加入则走 v0.12) |
| 兼容性承诺 | 从 v0.10.0 零停机升级 |
| 回滚路径 | 所有新字段可空，降级到 v0.10.0 仅丢失新配置 |
