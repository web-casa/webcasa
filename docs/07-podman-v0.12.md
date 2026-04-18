# Web.Casa v0.12 "Podman Only" 设计文档

**版本**: 0.12.0
**规划日期**: 2026-04-17
**目标**: 从 v0.11 Docker 运行时全面切换为 Podman (AppStream 5.6.0)
**前提**: WebCasa 尚无用户，无升级/迁移约束，可视为 v0.12 为"向前 clean break"

---

## 决策快照

| 维度 | 决策 |
|------|------|
| v0.11.1 补丁 | ❌ 跳过 (无用户) |
| 容器运行时 | Podman 5.6.0 (EL9 + EL10 AppStream 同版本) |
| Compose backend | `podman-compose` 1.5.x (AppStream) |
| docker-ce 检测 | ❌ 不做 (无用户) |
| 运行模式 | **Rootful** (参见 Q4 rootless 风险分析) |
| systemd service user | **专用 `webcasa` 用户** + `podman` 组 |
| `/var/run/docker.sock` | 软链到 `/run/podman/podman.sock` (透明兼容) |
| 数据迁移 | ❌ 不做 (无用户) |
| UI 文案 | 明确标 "Podman" |
| v0.11 维护 | EOL 同步 v0.12 发布 |

---

## 为何切换

### Docker 在 RHEL 10 的已知问题
- RHEL 10 deprecate iptables 内核模块，默认不装 `kernel-modules-extra`
- Docker 启动失败 (`Extension addrtype revision 0 not supported`)
- 官方 fix 需用户手动 `dnf install kernel-modules-extra` + `modprobe xt_addrtype br_netfilter`
- 一键安装体验被破坏

### Podman 的 RHEL 原生优势
- AlmaLinux 9/10 AppStream 内置 (无需第三方 repo)
- 无 daemon 模型降低攻击面
- 与 SELinux / systemd / cgroupv2 深度集成
- RHEL 长期支持承诺

### Podman 5.6 + WebCasa Docker SDK 兼容性
- `client.WithAPIVersionNegotiation()` 自动协商到 Podman 的 API v1.41/v1.43
- WebCasa 使用 Docker Go SDK v27.5.1，兼容窗口充足
- `podman-docker` shim 让 `docker` CLI 调用透明转发 → service.go 零改动

### App store 兼容性 (静态审计)
269 个应用经 `scripts/compose-audit.py` 静态扫描 (podman-compose 1.5.0 parser
+ Podman 5.6 兼容规则)：
- ✅ **234 apps (87%) 完全 clean** — 无任何 finding
- 🟡 **10 apps warning** — 主要是 `network_mode: host` (9) + `gpu-reservation-ignored` (1, ollama-nvidia)
- 🔵 **25 apps info-only** — `docker-sock-mount` (13) + `needs-rootful` (8) +
  `elevated-caps` (6) + `device-passthrough` (6) + `dep-completed-condition` (3)
  全部在 v0.12 rootful + symlink 部署模型下可工作，但需逐个实测确认
- 🔴 **0 critical / hard failures**

完整 JSON 报告: `docs/podman-compose-audit.json` (CI 可用作 diff baseline)。

静态审计工具: `scripts/compose-audit.py` (committed Phase 4)

### Rootless 放弃的理由
详见 `ROOTLESS-RISKS` 附录。简要：

- 5% 应用 (~14 个) rootless 下不可用 (VPN 需 NET_ADMIN / 特权容器 / 设备映射 / `docker.sock` 挂载)
- systemd user-service 模型重构成本 ~1 周
- app store 核心用户价值 > 安全收益
- RHEL SELinux + 专用 `webcasa` 用户已提供合理隔离

决议: **v0.12 Rootful**。rootless 可作 v0.13+ 独立特性评估。

---

## 架构变更

### 运行时栈

```
v0.11                           v0.12
───────                         ───────
WebCasa (root systemd)          WebCasa (webcasa user + podman group)
  ↓ Go Docker SDK                 ↓ Go Docker SDK (API negotiation)
  ↓ exec docker CLI               ↓ exec docker CLI → podman-docker shim → podman
docker-ce / containerd          Podman 5.6 (system service)
  ↓                               ↓
/var/run/docker.sock            /run/podman/podman.sock
                                  ↖_symlink_/var/run/docker.sock (app compat)
```

### Socket 兼容策略 (C1)

install.sh 创建符号链接：
```bash
ln -sf /run/podman/podman.sock /var/run/docker.sock
```

效果：
- 8 个挂载 `docker.sock` 的 app store 应用 (portainer/dockge/dozzle/uptime-kuma/crowdsec/cup/beszel-agent/homarr) 零修改可用
- WebCasa Go SDK 默认查找 `/var/run/docker.sock` 也命中
- `docker` CLI (podman-docker shim) 内部亦指向 Podman

### 用户权限模型 (A2)

```bash
# install.sh 新增步骤
useradd -r -s /sbin/nologin -d /var/lib/webcasa webcasa
usermod -aG podman webcasa
chown -R webcasa:webcasa /var/lib/webcasa /var/log/webcasa /etc/webcasa
```

systemd unit 改动:
```ini
[Service]
User=webcasa
Group=webcasa
SupplementaryGroups=podman
```

---

## install.sh 改造清单

### 新增

```bash
install_podman() {
    info "Installing Podman 5.6 stack from AppStream..."
    dnf install -y podman podman-docker podman-compose

    info "Enabling podman.socket (system)..."
    systemctl enable --now podman.socket

    # 等待 socket 就绪 (最多 10s)
    for i in {1..10}; do
        [ -S /run/podman/podman.sock ] && break
        sleep 1
    done
    [ -S /run/podman/podman.sock ] || fatal "podman.socket failed to start"

    # Docker socket 路径兼容 (app store 透明支持)
    install -d -m 755 /var/run
    ln -sf /run/podman/podman.sock /var/run/docker.sock

    # 验证 podman-docker shim
    if ! docker version --format '{{.Server.Version}}' &>/dev/null; then
        fatal "podman-docker shim not responsive; check 'podman info'"
    fi

    local podman_ver
    podman_ver=$(podman version --format '{{.Version}}')
    info "Podman ${podman_ver} installed and responding via docker shim"
}

create_service_user() {
    if ! getent passwd webcasa >/dev/null; then
        info "Creating webcasa service user..."
        useradd -r -s /sbin/nologin -d /var/lib/webcasa \
            -c "Web.Casa panel service" webcasa
    fi
    usermod -aG podman webcasa
}
```

### 删除

- 现有 Docker check (`if ! command -v docker...`) 替换为 podman check
- `SKIP_DOCKER_GROUP` 标志 → 新 `SKIP_PODMAN_GROUP`
- "Note: Docker is not installed" 提示删除 (install.sh 现在强制装 podman)

### 保留

- WebCasa 二进制下载 / 校验
- Caddy 安装
- systemd service 安装 (但 `User=root` 改为 `User=webcasa`)
- ALTCHA PoW 测试
- firewalld 配置

---

## WebCasa 代码改动

### 已在 Phase 2 交付 (commit 8467225 + review fixes)

| 文件 | 改动 |
|------|------|
| `plugins/docker/runtime.go` (新) | `Runtime` enum + `DetectRuntime()` + `SystemdUnit()` + `RuntimeVersion()` |
| `plugins/docker/daemon.go` | `RestartDockerDaemon` 在 Podman 下返回 `ErrDaemonConfigNotSupportedOnPodman` 哨兵错误 |
| `plugins/docker/plugin.go` | `checkDockerAlreadyReady` runtime-aware，带 3 次指数退避重试 |
| `plugins/docker/plugin.go` | `installDocker` 在 Podman 主机拒绝运行 EasyDocker |
| `plugins/docker/plugin.go` | `dockerStatus` 优先使用 `RuntimeVersion()` |

### Phase 3 (UI + 品牌 — 待做)

| 文件 | 改动 |
|------|------|
| `web/src/components/DockerRequired.jsx` | 文案改为 "Podman is required" (组件名保留) |
| `web/src/locales/en.json` / `zh.json` | `docker.*` i18n keys 文案更新 (key 名保留避免破坏) |
| `README.md` | "Docker" → "Podman 5.6 (auto-installed from AppStream)" |
| daemon-config UI | 识别 `ErrDaemonConfigNotSupportedOnPodman` 并渲染 Podman 特定说明页 |

### Phase 4 (App Store 静态审计 + 27 高风险应用测试)

**已完成**:

| 交付 | 路径 |
|------|------|
| 静态审计脚本 | `scripts/compose-audit.py` (CI 可重复运行；`--strict` 模式可作为 PR gate) |
| 269-app 全量报告 | `docs/podman-compose-audit.json` |
| 27 高风险应用清单 | 本文件「重点验证清单」表（Status 列待 VPS 实测填充） |

**待执行（需 VPS Podman 5.6 环境）**:

- 27 高风险应用逐个 stack up/down + 功能验证 (步骤见上方实测执行步骤)
- 任何 🔴 fail 应用：判定 (a) 修 manifest，(b) Podman-only 标记 unavailable，(c) 文档化 workaround

### Phase 5 候选 (视 Phase 4 实测决定)

| 文件 | 可能改动 |
|------|---------|
| `plugins/docker/client.go:NewClient` | 若发现 Podman socket 路径非 `/var/run/docker.sock`，加 `WEBCASA_DOCKER_SOCKET` env override |
| `plugins/docker/service.go` | 若 `podman compose logs --follow` 输出格式与 Docker 有差异，调整 parser |
| `plugins/database/service.go` | PG/MySQL/Redis compose 调用路径同上 |

### 不改

- `plugins/docker/imagestatus.go` — 本地 SHA 对比，podman 行为等价
- `plugins/docker/plugin.go` ID 保留 `docker` (内部标识符；DB 表名 + 路由前缀 + 前端 key 向后兼容)
- Go Docker SDK 调用 — 自动协商，无需改
- app store compose 文件 — symlink 透明
- 端点路径 / API 契约 — 全部保留

---

## App store 兼容性矩阵

### 全体 269 app 静态审计

权威来源：`docs/podman-compose-audit.json` summary 块（每次 `scripts/compose-audit.py` 运行重新生成）。

```
✅ Clean                     : 234 (87.0%)
🟡 Warning                   :  10  (network-host, gpu-reservation-ignored)
🔵 Info-only                 :  25  (sock 挂载 / rootful / device passthrough)
🔴 Critical                  :   0

findings histogram:
  docker-sock-mount         13
  network-host               9
  needs-rootful              8
  elevated-caps              6
  device-passthrough         6
  dep-completed-condition    3
  gpu-reservation-ignored    1
```

### 重点验证清单 (27 个高风险 app 实测)

每个应用列出 audit 检测到的 codes (可在 `docs/podman-compose-audit.json` 查到详情)。
**Status** 字段在 VPS 实测后由 Phase 4 执行人填写：✅ pass / 🟡 partial / 🔴 fail / ⏳ pending。

| App | Audit codes | 类别 | 验证点 | Status |
|-----|------------|------|--------|--------|
| portainer | `docker-sock-mount` | sock | symlink 透明访问，UI 能看到 podman 容器 | ⏳ |
| dockge | `docker-sock-mount` | sock | compose stack 列表正确加载 | ⏳ |
| dozzle | `docker-sock-mount` | sock | 容器日志实时流可读 | ⏳ |
| uptime-kuma | `docker-sock-mount` | sock | 监控创建容器 | ⏳ |
| crowdsec | `docker-sock-mount` | sock | log scan API 调用 | ⏳ |
| cup | `docker-sock-mount` | sock | 镜像 update 检查 | ⏳ |
| beszel-agent | `docker-sock-mount`,`network-host` | sock+net | 端口监听 + 容器枚举 | ⏳ |
| homarr-1 | `docker-sock-mount` | sock | dashboard 整合容器卡片（注意：`homarr` 主条目无 sock 挂载，flagged 的是 `-1` 变种） | ⏳ |
| dashdot | `needs-rootful` | priv | 系统 metrics 采集 | ⏳ |
| gladys | `docker-sock-mount`,`needs-rootful`,`network-host` | priv+sock+net | 物联网设备发现 | ⏳ |
| homebridge | `needs-rootful`,`network-host` | priv+net | mDNS 广播 + HomeKit pair | ⏳ |
| kasm-workspaces | `needs-rootful` | priv | 容器化桌面会话启动 | ⏳ |
| scrypted | `device-passthrough`,`needs-rootful`,`network-host` | priv+net+dev | 摄像头流 | ⏳ |
| sshwifty | `needs-rootful` | priv | SSH 终端 | ⏳ |
| stirling-pdf | `needs-rootful` | priv | PDF 处理 | ⏳ |
| unmanic | `needs-rootful` | priv | 转码任务运行 | ⏳ |
| cloudflared | `network-host` | net | tunnel 建立 | ⏳ |
| matter-server | `network-host` | net | Matter pair | ⏳ |
| mdns-repeater | `network-host` | net | 跨网段 mDNS | ⏳ |
| plex | `network-host` | net | 流媒体 + DLNA 发现 | ⏳ |
| netdata | `docker-sock-mount`,`elevated-caps` | sock+cap | system metrics | ⏳ |
| transmission-vpn | `device-passthrough`,`elevated-caps` | cap+dev | `/dev/net/tun` + NET_ADMIN | ⏳ |
| windows | `device-passthrough`,`elevated-caps` | cap+dev | KVM `/dev/kvm` 可访问 | ⏳ |
| zerotier | `device-passthrough`,`elevated-caps`,`network-host` | priv+net+cap+dev | TUN 设备 + 路由 | ⏳ |
| zigbee2mqtt | `device-passthrough` | dev | `/dev/ttyUSB0` 串口适配 | ⏳ |
| ollama-nvidia | `gpu-reservation-ignored` | gpu | 验证 fallback：无 NVIDIA CDI 时降级 CPU；配置 CDI 后 GPU 可用 | ⏳ |
| n8n-1 | `dep-completed-condition` | dep | `service_completed_successfully` 在 podman-compose 1.5 下 init 容器顺序行为（有历史 issue #575） | ⏳ |

### 实测执行步骤 (per-app)

```bash
# 1. 通过 Web.Casa UI 安装 (走 stack 路径)
# 2. 等待容器到 running 状态
podman ps --filter name=<app>
# 3. 检查日志无 fatal
podman logs <container> --tail 100
# 4. 验证应用功能 (上面表格的"验证点"列)
# 5. 删除并清理
# 6. 在表格 Status 列填结果
```

### 特殊处理

**ollama-nvidia**:
- 原 compose 用 `deploy.resources.reservations.devices.driver=nvidia`
- Podman 需 NVIDIA CDI (Container Device Interface)
- 完整指引见 [`docs/nvidia-gpu.md`](nvidia-gpu.md):
  `dnf install nvidia-container-toolkit` + `nvidia-ctk cdi generate /etc/cdi/nvidia.yaml`
- compose 改成 `devices: ["nvidia.com/gpu=all"]` 即可

**8 个 docker.sock 应用**:
- symlink 后 API v1.41 访问 podman 容器
- Portainer 官方文档确认 Podman 支持
- Dockge / Dozzle 社区验证可用
- 无需修改 compose，只需实测确认

---

## CI 矩阵

```yaml
# .github/workflows/ci.yml  (v0.12 修改)
jobs:
  build:
    runs-on: ubuntu-latest  # Go build 不受容器运行时影响
    # 保留

  integration:
    strategy:
      matrix:
        container_os:
          - almalinux:9   # Podman 5.6.0
          - almalinux:10  # Podman 5.6.0
    steps:
      - name: Setup container with Podman
        # 替换当前的 docker-in-docker / systemd 逻辑
      - name: Install WebCasa
        # 跑 install.sh
      - name: Run app smoke tests
        # 现有 API 测试套件 (31 步)
      - name: Spin up 25 high-risk apps from store
        # 新增: 逐个 stackUp + healthcheck + stackDown
```

### 新测试:

1. `TestPodmanSocketCompat` — Go SDK 连接 Podman socket，ListContainers / ListImages / Inspect 基本操作
2. `TestDockerShimTransparency` — `exec.Command("docker", "compose", "version")` 返回 Podman Compose 版本
3. `TestAppStoreBatchUpDown` — 27 高风险 app 全部 stack up/down 通过
4. `TestSocketSymlinkIntegrity` — `/var/run/docker.sock` 符号链接完整性

---

## 风险登记册 (Final)

| ID | 风险 | 概率 | 影响 | 缓解 |
|----|-----|-----|------|------|
| R1 | podman-compose 1.5 对 compose v3 某 edge case 行为不同 | 低 | 1 app (ollama-nvidia) | 已静态审计确认仅此 1 例，文档处理 |
| R2 | Go SDK v27 ↔ Podman API v1.43 协商失败 | 低 | WebCasa 无法访问 Podman | CI 矩阵 + VPS 实测 |
| R3 | `docker compose version` parser 遇到 Podman 输出格式 | 中 | 启动检测误报 "compose not installed" | parser 放宽 (必改) |
| R4 | `webcasa` 用户 + `podman` 组权限不足 | 中 | WebCasa 无法调用 podman.sock | install.sh 验证 + 文档 |
| R5 | rootful podman socket 在 SELinux enforce 下 permission denied | 中 | WebCasa 连不上 socket | 需正确的 `system_u:object_r:container_runtime_t` 标签 + SELinux policy tweak (通过 `semanage fcontext`) |
| R6 | 27 高风险 app 实测中出现未预期不兼容 | 中 | 个别 app 文档化不可用 | CI 覆盖 + README 维护兼容矩阵 |
| R7 | `fuse-overlayfs` 默认导致存储性能不如 Docker | 低 | 构建/部署慢 30% | `/etc/containers/storage.conf` 配 native overlay (内核 6.12 支持) |
| R8 | systemd `User=webcasa` 下 Caddy 权限绑 80/443 | 高 | 反代不工作 | 需 `CAP_NET_BIND_SERVICE` capability 或 Caddy 独立 root 运行 |

### R8 详细说明

Caddy 绑 80/443 特权端口。当前 WebCasa service 以 root 跑，包含 Caddy reload 调用。切 `webcasa` 用户后，两个方案：

**方案 X**: Caddy 独立 systemd service 以 root 跑，WebCasa 通过 API/CLI 触发 reload
**方案 Y**: WebCasa systemd unit 加 `AmbientCapabilities=CAP_NET_BIND_SERVICE` 允许绑特权端口

v0.11 已用 方案 X (Caddy 独立 service)，v0.12 保持不变，WebCasa 只需 `systemctl reload caddy` 权限 (sudoers 或 polkit)。

---

## 实施路线图 (3 人周)

### Week 1: 基础设施 + install.sh

| 天 | 任务 | 交付物 |
|----|------|--------|
| 1 | install.sh Podman 改造 | 新 install_podman() + create_service_user() |
| 2 | systemd unit 改造 (`User=webcasa`) | 权限验证通过 |
| 3 | Docker socket symlink + podman-docker shim 验证 | `docker version` 返回 podman server |
| 4 | WebCasa plugin.go compose version parser 修补 | 启动检测通过 |
| 5 | VPS fresh install 验证 (AlmaLinux 9 + 10) | 两 OS 冒烟通过 |

### Week 2: App store 兼容性 + UI

| 天 | 任务 | 交付物 |
|----|------|--------|
| 1-2 | 27 高风险 app VPS 实测 | 兼容性矩阵表 + bug fix commits |
| 3 | UI 文案更新 (DockerRequired → PodmanRequired) + i18n | 前端 PR |
| 4 | CI 改造 (AlmaLinux 9/10 matrix + Podman setup) | ci.yml 绿 |
| 5 | ollama-nvidia NVIDIA CDI 文档 | docs/nvidia-gpu.md |

### Week 3: 打磨 + 发布

| 天 | 任务 | 交付物 |
|----|------|--------|
| 1 | SELinux fcontext tuning (R5) + storage.conf native overlay (R7) | install.sh 生产 ready |
| 2 | Codex review v0.12 所有改动 | review findings 归零 |
| 3 | 版本号 bump (0.12.0) + changelog | commit |
| 4 | VPS 全量 smoke test (269 app 中抽 50 个) | 测试报告 |
| 5 | v0.12.0 tag + GitHub Release | 公开发布 |

---

## 附录 A: ROOTLESS 风险清单 (Q4 分析)

```
🔴 硬阻塞 (rootless 下完全不可用)
  - cap_add: NET_ADMIN/NET_RAW (内核限制 userns grant)
    影响: transmission-vpn, zerotier
  - /var/run/docker.sock 路径差异 (rootless 为 $XDG_RUNTIME_DIR)
    影响: portainer/dockge/dozzle/uptime-kuma/crowdsec/cup/beszel-agent/homarr
  - firewall 插件失效 (rootless 不操作 iptables/nftables)
    影响: WebCasa firewall 插件整体

🟡 需额外配置才能工作
  - 端口 < 1024 (需 sysctl net.ipv4.ip_unprivileged_port_start=80)
    影响: 90%+ web 应用默认暴露 80/443
  - privileged: true (rootless 只在 userns 内生效)
    影响: 8 app (部分功能降级)
  - devices: 映射 (需 webcasa user 加入 dialout/input/tty 等组)
    影响: 3 app (zigbee2mqtt/windows/transmission-vpn)
  - systemd user service + loginctl enable-linger 配置

🟢 可解决但有工程量
  - Volume 路径迁移 (/var/lib/containers → ~/.local/share/containers)
  - cgroupv2 delegation (systemd Delegate=memory cpu pids)
  - fuse-overlayfs → native overlay 切换
  - Go SDK 连接 $XDG_RUNTIME_DIR/podman/podman.sock
```

**v0.12 采用 rootful** 规避上述风险，rootless 评估留 v0.13+ (若用户反馈安全诉求强烈)。

---

## 附录 B: Podman 命令速查 (对 WebCasa 运维有用)

```bash
# 查看 socket 健康
ss -l -t -n -p 2>/dev/null | grep podman
curl --unix-socket /run/podman/podman.sock http://d/info | jq .

# 列容器 (Docker CLI 兼容)
docker ps                           # 经 podman-docker shim → podman ps
podman ps                           # 原生

# Compose
podman-compose -f docker-compose.yml up -d
docker compose -f docker-compose.yml up -d   # 经 shim

# 切换 SELinux 标签 (如需)
chcon -R -t container_file_t /var/lib/webcasa/stacks

# 诊断
podman info | grep -E "ociRuntime|graphDriverName|version"
journalctl -u podman.socket -n 50
```

---

## 开放问题 / v0.13 候选

1. **Rootless evaluation**: 若用户反馈要求，重评估 per-user socket + selinux 组合方案
2. **NVIDIA CDI 自动化**: `install.sh --gpu=nvidia` 标志自动化 NVIDIA CDI 配置
3. **Quadlet 模型**: 将 app store stack 转为 Quadlet 原生 systemd 单元 (Podman 4.4+ 特性)
4. **SELinux policy refinement**: 为 webcasa 服务定义专用 SELinux 类型 (`webcasa_t`) 进一步隔离
5. **`podman-docker` vs 显式 Podman CLI**: 长期评估是否保留 `docker` shim 或要求用户直接用 `podman`

---

## 发布标准 (v0.12.0 合并 main 前)

- [ ] install.sh 在 AlmaLinux 9 + 10 fresh VM 上干净安装
- [ ] 27 高风险 app 全部 stack up/down 验证通过
- [ ] CI matrix (AlmaLinux 9/10) 绿
- [ ] `go test ./... -timeout 120s` + `-race` 全绿
- [x] `docs/07-podman-v0.12.md` (本文档) + `docs/nvidia-gpu.md` (新) 完整
- [ ] `changelog.md` [0.12.0] 节含 breaking change warning
- [ ] `VERSION` + `web/package.json` → 0.12.0
- [ ] README "supported runtime: Podman 5.6+" 更新
- [ ] GitHub Release notes 链接迁移说明 (即使无用户，为未来 star watcher 准备)
