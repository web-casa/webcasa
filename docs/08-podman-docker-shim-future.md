# `podman-docker` Shim 的未来定位

**写作日期**: 2026-04-19
**状态**: 评估报告（非决策）
**对应 Epic**: v0.13 Epic 2（Tier 2 #5 — podman-docker vs 显式 Podman CLI）

---

## TL;DR

**目前保留 shim，v0.13 / v0.14 不做迁移**。理由：

1. 代价具体且不小（~39 个调用点、10 个文件，多数在 AI agent 工具层）
2. 收益是架构清洁 + 轻量化，**用户侧无任何可感知差异**
3. 去掉 shim 会反过来**破坏 app-store 的 8 个挂 `/var/run/docker.sock` 的 app**（它们读路径是硬编码 `docker.sock`）
4. Podman 社区对 `podman-docker` 的长期维护承诺稳定，没有弃用信号

写下一个明确的 **trigger 列表**，满足任一条时才重新评估（见文末）。

---

## 当前 shim 依赖面

### Go 代码里调 `docker` CLI 的所有点

| 文件 | 调用数 | 典型用途 |
|------|-------|---------|
| `internal/plugin/coreapi.go` | 15 | AI agent 工具 (`create_container` / `exec_sql` / `list_containers` 等) — 给 LLM 的 function-calling layer |
| `plugins/deploy/docker_runner.go` | 12 | Deploy plugin 把项目打进 Docker 镜像并 run (`run`/`stop`/`rm`/`logs`/`inspect`) |
| `plugins/docker/service.go` | 3 | Compose 操作的 wrapper（`docker compose logs --follow` 等） |
| `plugins/docker/plugin.go` | 3 | 运行时健康检查 (`docker --version` / `docker compose version`) |
| `plugins/docker/runtime.go` | 1 | Fallback 版本探测（当 Podman binary 缺但 Docker CLI 存在时） |
| `plugins/docker/daemon.go` | 1 | `systemctl restart docker`（Podman 路径短路这个函数，所以实际走不到） |
| `plugins/{php,monitoring,database,backup,appstore}/service.go` | 5 | 每个插件 1 处 — 大多数是 `docker exec` / `docker ps` 类状态查询 |

**合计 39 个调用点 / 10 个文件**。

### Shim 之外的 Docker-特定依赖

- **Go Docker SDK v27** (`plugins/docker/client.go`)：通过 `/var/run/docker.sock` 走 API，**不经过 shim**。这是 WebCasa 和 Podman 之间最大的交互面，Podman 的 compat API 已经兼容。
- **App-store compose 文件里的 `/var/run/docker.sock:/var/run/docker.sock` bind mount**：由 `install.sh` 的软链 (`/var/run/docker.sock → /run/podman/podman.sock`) 处理，也不经过 `docker` CLI shim。
- **`podman-compose`**：独立二进制，和 shim 无关。

所以 shim 的作用范围就是上表那 39 个 `exec.Command("docker", ...)` 点。

---

## 移除 shim 的两种路径

### 路径 A：硬切换（把 `docker` 替换成 `podman`）

**做法**：全仓库 `sed` 把 `exec.Command("docker", ...)` 换成 `exec.Command("podman", ...)`。

**风险**：
- `docker compose version` → `podman-compose version`（不同命令而非简单改名）
- `docker build -t name:tag .` vs `podman build -t name:tag .` — CLI 大致兼容，但某些标志行为不同（`--progress=auto` 等）
- 最关键：**app-store 里 8 个挂 `/var/run/docker.sock` 的 app**（portainer / dockge / dozzle 等）期望容器内路径是 `docker.sock`。硬切换不影响它们（install.sh 的软链还在），但若同时**也移除软链**就全部 break。
- `setup.sh` / `scripts/*.sh` / 文档里的 `docker` 命令示例全部要改

**工作量**：~2-3 天 + 回归测试。

### 路径 B：双栈并行（保留 shim + 默认走 podman）

**做法**：新增一个配置开关 `WEBCASA_RUNTIME_CLI=podman|docker`，默认 `podman`。Go 代码里所有 `exec.Command("docker", ...)` 用 wrapper `runtimeExec(ctx, ...)` 替换，wrapper 查配置选 binary。

**优势**：软着陆，管理员可以回滚，双栈共存阶段还能在 CI 验证。

**工作量**：~3-5 天（wrapper + 测试 + 文档），长期维护两条代码路径直到某天彻底去 `docker=true` 分支。

---

## 用户侧实际影响

| 路径 | App-store 8 个挂 sock 的 app | Deploy plugin Docker 模式 | AI agent 工具 | 运维认知 |
|------|------|------|------|------|
| A (硬切) | 不影响（靠软链）| 不影响（CLI 兼容）| 需适配少量输出 parsing | **用户改脚本** — `docker ps` 不再可用需 `podman ps` |
| B (双栈) | 不影响 | 不影响 | 不影响 | 用户改不改随意 |

**关键洞察**：**用户看不到任何功能差异**。shim 是 WebCasa 内部实现细节，迁不迁移不影响他们使用 app-store、部署项目、跑 AI agent 的体验。

这是为什么我不推荐现在做：**只是为了架构清洁付两天工时**，没有用户价值。

---

## 社区信号

截至 2026-04-19：

- `podman-docker` 包在 Fedora / RHEL / Debian / Arch 所有主流发行版**持续维护**
- Red Hat 官方文档把 shim 作为 "从 Docker 迁移的推荐过渡方案"
- Podman upstream 没有 deprecation 声明
- 常见"Docker 用户迁 Podman"教程都推荐 shim 作为第一步

**结论**：Shim 不会在可预见的 18-24 个月内消失。

---

## 移除 Trigger 列表

满足以下**任一**条件时，重新启动评估：

1. **Upstream Deprecation**: Red Hat / Fedora 宣布 `podman-docker` 进入 deprecation 窗口
2. **CLI 分歧**: Podman 5.x 的某个版本让 `docker` 和 `podman` 的常用子命令行为显著不同（日志格式、exit code 语义、标志名），导致 shim 翻译层出现用户可见 bug
3. **Shim 性能问题**: 大量 `docker` CLI 调用的场景下 shim 层延迟显著（目前无证据）
4. **安全事件**: shim 本身出了 CVE
5. **架构重构**: 有其他理由在 `runtime.go` 层做大改，顺手把 shim 调用集中成 wrapper（低成本时机）

---

## 具体代码清理候选（即使不移除 shim 也值得做）

以下 2 处是"误用"，应该在 v0.13 顺手清掉：

### `plugins/docker/daemon.go:160`

```go
case RuntimeDocker:
    return exec.Command("systemctl", "restart", "docker").Run()
```

Podman 路径早在函数入口就短路（`RestartDockerDaemon` 返回 `ErrDaemonConfigNotSupportedOnPodman`）。这段代码**只在 v0.11 还用 Docker 的主机上会走到**。v0.12 用户看不到。清理与否不影响行为，但留着会让"v0.12 为什么还有 systemctl restart docker"的 code review 问题反复出现。

**建议**：加注释明确标注 "legacy path，v0.11 and earlier only"。或者彻底删掉 `RuntimeDocker` 分支，让 daemon.go 只支持 Podman（更激进）。

### `plugins/docker/runtime.go:109`

```go
case RuntimeDocker:
    if out, err := exec.Command("docker", "--version").Output(); err == nil {
        return strings.TrimSpace(string(out))
    }
```

同上，legacy path。保留无害，删掉也行。

---

## 建议 Phase 6 / v0.13 行动项

1. **v0.13**: 不动 shim，保留现状
2. **v0.13**: 顺手给上面两个 legacy path 加注释（10 分钟）
3. **持续观察**: 每 6 个月（或看到社区信号时）重新读这份文档一次，检查 trigger 列表
4. **v1.0 前**: 如果届时还没满足任一 trigger，再评估一次 "硬切" 路径 B 的双栈方案作为长期清洁动作

---

## 参考

- v0.12 设计文档：`docs/07-podman-v0.12.md` 第"为何切换"节
- Red Hat Podman docs: https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html/building_running_and_managing_containers/
- `podman-docker` upstream: https://github.com/containers/podman/blob/main/docs/source/markdown/podman.1.md
