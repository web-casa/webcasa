# SELinux 运维指南 (Web.Casa v0.12 on EL9/EL10)

**适用版本**: v0.12.0+
**目标读者**: 运行在 SELinux enforcing 模式下的 Web.Casa 管理员

---

## TL;DR — 预期的"正确"状态

在全默认 AlmaLinux 9/10 上装完 Web.Casa v0.12 后：

```bash
# SELinux 必须是 enforcing（默认）
$ getenforce
Enforcing

# WebCasa systemd 单元跑在专用 webcasa 用户下
$ systemctl show webcasa --property=User --property=Group
User=webcasa
Group=webcasa

# 没有 denial 事件
$ sudo ausearch -m AVC -ts today 2>/dev/null | grep -iE "webcasa|caddy|podman" | head
<应为空或仅有无害的 granted 事件>
```

**如果以上 3 步都 OK — 本文档后面的内容都不用看。** 继续往下只是为了遇到问题时的排查参考。

---

## 为什么 v0.12 要专门讲 SELinux

v0.11 和更早版本 WebCasa 以 root 身份运行 Docker daemon，SELinux 对 daemon 的默认策略已经覆盖得很好，几乎零摩擦。v0.12 做了两个同时影响 SELinux 的改动：

1. **专用 `webcasa` 用户** — `User=webcasa` + `Group=webcasa` + `SupplementaryGroups=podman`
2. **Podman socket 非标准路径** — 挂 `/var/run/docker.sock` 软链到 `/run/podman/podman.sock`

这组合触发 R5 风险（见 `docs/07-podman-v0.12.md` 风险登记册）：SELinux 对**进程上下文 × 文件上下文**配对敏感，webcasa 用户访问 podman socket 如果标签不对会被拒绝，表现为 WebCasa 启动时 `docker client connection refused` — 表象像 Podman 没跑，真因是 SELinux 拒绝。

v0.12 的 install.sh 已经处理了基本情况，但**某些边缘场景需要管理员介入**，本文列出这些场景 + 修法。

---

## 标签与上下文速查

Web.Casa v0.12 在 SELinux 下涉及以下 type：

| 对象 | 期望的 SELinux type | 由谁管 |
|---|---|---|
| `/usr/local/bin/webcasa` 二进制 | `bin_t` 或自定义 `webcasa_exec_t` | install.sh `restorecon` |
| `/var/lib/webcasa/` 数据 | `container_file_t`（建议）或默认 `var_lib_t` | install.sh |
| `/var/lib/webcasa/stacks/*` app 数据 | `container_file_t` | install.sh（挂 volume 时）|
| `/etc/webcasa/` 配置 | `etc_t` | install.sh |
| `/var/log/webcasa/` 日志 | `var_log_t` | install.sh |
| `/run/podman/podman.sock` | `var_run_t` (RPM 默认；不是 `container_var_run_t`) | podman RPM |
| `/var/run/docker.sock` (symlink) | 继承 target — `var_run_t` | install.sh `ln -sf` |
| webcasa 进程运行时 | `unconfined_service_t` 或自定义 `webcasa_t` | systemd + policy |

查一个具体文件的标签：

```bash
ls -lZ /var/run/docker.sock
# lrwxrwxrwx. 1 root root system_u:object_r:container_var_run_t:s0
#   24 ... /var/run/docker.sock -> /run/podman/podman.sock
```

**关键点**：symlink 自己的标签不重要；SELinux 按 target 的标签鉴权。所以只要 `/run/podman/podman.sock` 是 `container_var_run_t`（Podman RPM 默认写进了 file_contexts），webcasa 进程就能连。

---

## install.sh 默认做的事

v0.12 install.sh 在 SELinux enforcing 主机上自动：

1. **不关闭 SELinux**。之前某些面板的做法是 `setenforce 0` — 我们**明确拒绝**这样做，SELinux 是发行版的默认安全层，关掉会让整机所有服务都降级。
2. 创建 `/var/lib/webcasa/` 等目录后**不手动 `chcon`**。默认 `var_lib_t` 标签足够，WebCasa 不跑有 SELinux 特殊要求的行为。
3. 让 `webcasa` 用户加入 `podman` 组，**靠 DAC 权限**（`/run/podman/podman.sock` 是 `srw-rw----. root podman`）访问 socket。SELinux 层面：webcasa 进程跑在 `unconfined_service_t` 域（systemd 默认 service 域），对 `var_run_t` socket 的访问默认放行。**注意**：socket 的 SELinux type 是 `var_run_t`（不是早期文档误写的 `container_var_run_t`），可以用 `ls -laZ /run/podman/podman.sock` 验证。
4. 不写自定义 SELinux policy module。WebCasa 的所有行为（HTTP 服务、读 SQLite、启动 podman CLI、读日志文件）都能用 `unconfined_service_t` 默认策略覆盖。

结果：**默认安装零 SELinux 摩擦**。

---

## 常见 denial 场景 + 解法

### 场景 1：app-store 应用挂载本地目录，容器里读不到

**症状**：

```
2026-04-19 ... kernel: audit: type=1400 audit(...):
  avc:  denied  { read } for  pid=12345 comm="nginx" name="index.html"
  dev="dm-0" ino=... scontext=system_u:system_r:container_t:s0:c123,c456
  tcontext=unconfined_u:object_r:var_lib_t:s0 tclass=file permissive=0
```

`scontext` 是**容器进程**（`container_t`），`tcontext` 是宿主文件（`var_lib_t`）。默认策略禁止容器读写非 `container_file_t` 的文件。

**修法 A（推荐，单个挂载）**：Compose 里给 volume 加 `:Z` 标记，Podman 自动 relabel：

```yaml
services:
  nginx:
    volumes:
      - ./html:/usr/share/nginx/html:Z   # ← 注意 :Z（大写，私有 relabel）
```

`:Z` 会把宿主目录递归改成 `container_file_t:s0:c<random>`（每个容器独立类别），容器停止后会保留（不是 ephemeral）。

**修法 B（共享挂载）**：用 `:z`（小写）让多个容器共享相同标签（丢失跨容器隔离）：

```yaml
    volumes:
      - ./shared:/data:z
```

**修法 C（永久 fcontext）**：声明某个路径"永远"是 container_file_t：

```bash
sudo semanage fcontext -a -t container_file_t '/opt/appdata(/.*)?'
sudo restorecon -Rv /opt/appdata
```

以后即使 `restorecon` 全盘扫也不会改回去。适合你有**固定的数据目录约定**（比如都放 `/opt/appdata/`）。

### 场景 2：systemd 容器单元读 `/etc/webcasa/` 被拒

**症状**：某个 stack 用 Quadlet 或 podman generate systemd 做成了系统服务，运行时报不能读 WebCasa 的配置文件。

**原因**：容器进程 `container_t` 读宿主 `etc_t` 默认被拒。

**修法**：绝大多数情况不应该把 WebCasa 的配置挂到 app-store 容器里。如果确实需要（比如备份插件要读 `/etc/webcasa/config.yaml`），改成把**只读副本**放到 `container_file_t` 区域：

```bash
sudo install -m 644 /etc/webcasa/config.yaml /var/lib/webcasa/exported/config.yaml
sudo chcon -t container_file_t /var/lib/webcasa/exported/config.yaml
```

挂这份副本而不是原文件。

### 场景 3：Caddy 绑 :80/:443 被 SELinux 拒绝

**症状**：

```
avc: denied { name_bind } for pid=... comm="caddy"
  src=80 scontext=system_u:system_r:init_t:s0
  tcontext=system_u:object_r:http_port_t:s0 tclass=tcp_socket
```

**原因**：SELinux 的 `name_bind` 是**独立于** Linux capabilities 的检查 —
拿到 `CAP_NET_BIND_SERVICE` 只是满足了 DAC 层（允许绑特权端口），SELinux 还要额外
在 MAC 层允许 `scontext` 类型对目标 port type 做 `name_bind`。两层都要通过。

v0.12 Caddy 以 root 独立 systemd service 运行（方案 X，见 `07-podman-v0.12.md`
R8），进程 type 是 `init_t` 或 `unconfined_service_t`，两者默认都被策略允许绑
`http_port_t` (80/443)，所以**正常部署不会触发这个 denial**。以下仅适用于
你改了默认部署（比如让 Caddy 跑在 webcasa 用户下）并确实看到了这条 AVC。

**修法 A（推荐）**：把 Caddy 监听的额外端口登记到 `http_port_t`：

```bash
# 比如想让 Caddy 监听 8080：
sudo semanage port -a -t http_port_t -p tcp 8080
# 列出当前 http_port_t 包含哪些端口
sudo semanage port -l | grep '^http_port_t'
```

**修法 B（自定义 policy module）**：如果你让 Caddy 跑在自定义 domain 里、
希望它能绑所有 `http_port_t` 端口，用 audit2allow 从 AVC 生成 policy：

```bash
sudo ausearch -m AVC -ts recent | audit2allow -M caddy-binds
sudo semodule -i caddy-binds.pp
```

生成的 `.te` 文件要人工审查 — audit2allow 只是给 raw 起点，不应直接 semodule -i
而不看内容（可能比你想的放得更宽）。

**不是修法（易混淆）**：`AmbientCapabilities=CAP_NET_BIND_SERVICE` 或
`setcap cap_net_bind_service=+ep` 都只解决 capability 层的问题，**不影响** SELinux
`name_bind`。v0.12 install.sh 给 Caddy 二进制打了 setcap，是为了支持以非 root
启动的场景 — 不是 SELinux 解法。

### 场景 4：app-store 容器挂 docker.sock 但报"can't connect to docker"

**症状**：portainer/dockge/dozzle 等挂 `/var/run/docker.sock` 的应用启动后立刻
fatal，日志典型如：

```
{"level":"fatal","message":"Could not connect to any Docker Engine"}
```

`podman exec` 或宿主上 `curl --unix-socket /run/podman/podman.sock http://d/_ping`
工作正常，证实 socket 本身是好的 —— 问题在容器内 `container_t` 域无法跨 SELinux
策略边界访问宿主 `var_run_t` 的 socket。

**v0.12 默认修法**：app-store 渲染器（`plugins/appstore/renderer.go`）在安装时
自动给任何挂载 docker.sock / podman.sock 的 service 注入：

```yaml
security_opt:
  - label=disable
```

这会关闭**该单个容器**的 SELinux 标签隔离，允许它访问 `var_run_t` socket。
其他容器仍受完整策略保护。这是 v0.12 发布前 VPS 实测推翻了"用 SELinux
boolean 修"的猜测后的结论 —— `container_manage_cgroup`、`container_use_*`
等现成 boolean 都与 `var_run_t` socket 访问无关，实测无效；写自定义 policy
module 又超出发布时间表。

代价：挂 docker.sock 的容器（portainer / dockge / dozzle / uptime-kuma /
crowdsec / cup / beszel-agent / homarr-1）在 SELinux 层面等价于"非隔离"。
这类应用本来就是 root-equivalent 管理工具，标签隔离能带来的额外防护有限。

**自己写的 compose** (不经过 renderer) 需要手动加这两行。

**不应该做**：改 `/etc/selinux/config` 把 SELinux 整机切到 permissive 或
disabled —— 这等于把标签隔离对所有容器全部关闭，remediation 远超必要。

### 场景 5：重装 / 恢复备份后 `/run/podman/podman.sock` 连不上

**症状**：WebCasa 报 `connect: permission denied`，但 `webcasa` 用户在 `podman` 组里、DAC 看着没问题。

**原因**：podman.socket 的父目录 `/run/podman/` 被恢复/rsync 时丢了 SELinux 标签。

**验证**：

```bash
ls -Zd /run/podman/
# 期望：system_u:object_r:container_var_run_t:s0
# 异常：system_u:object_r:default_t:s0 或 var_run_t
```

**修法**：`restorecon` 把标签刷回官方策略定义：

```bash
sudo restorecon -Rv /run/podman/
sudo systemctl restart podman.socket
```

---

## 诊断命令速查

```bash
# 1. 最近的 AVC denial，按时间倒序
sudo ausearch -m AVC,USER_AVC -ts recent 2>/dev/null

# 2. 把 raw audit log 翻译成人类可读
sudo ausearch -m AVC -ts today 2>/dev/null | audit2why

# 3. 实时观察 denials（调试期间）
sudo tail -F /var/log/audit/audit.log | grep -iE "avc.*denied"

# 4. 某个进程当前的 SELinux context
ps -eZ | grep webcasa

# 5. 某个端口的 SELinux type
sudo semanage port -l | grep -E '(^|\s)80\s'
# http_port_t                    tcp      80, 81, 443, ...
```

---

## 关闭 SELinux 是 ❌ 不要做

有时候网上会看到"WebCasa 装不上？`setenforce 0` 试试" — **不要听这种建议**。

- SELinux 不是 WebCasa 装不上的原因（v0.12 默认安装已经验证过 EL9/EL10 enforcing 零摩擦）
- 关闭 SELinux 会让**整机**所有服务都失去 MAC 保护，WebCasa 只是其中之一
- 如果真的遇到 SELinux 阻塞，上面"常见 denial 场景"里 90% 的 case 都有针对性修法
- 如果以上都不行，至少用 `setenforce 0` 临时切到 permissive **诊断**完问题再 `setenforce 1` 恢复，永远不要在 `/etc/selinux/config` 里改 `SELINUX=disabled`

唯一可接受的永久关闭场景：你运行在根本不用 SELinux 的容器 / minimum install / 嵌入式环境，那本文其他内容对你也没用。

---

## webcasa_t 自定义 policy (v0.13 Preview — 默认关闭)

v0.13 附带一个自定义 SELinux policy module (`policy/webcasa.te` → `webcasa.pp`)
把 WebCasa 服务从 `unconfined_service_t` 切到专用 `webcasa_t` 域。**默认不启用**
—— baseline 规则在首次 VPS 实测时发现若干 exec'd 子进程 (caddy / curl)
继承 `webcasa_t` 后缺规则，需要更多迭代（可能要做 domain transition 拆到
`webcasa_caddy_t` / `webcasa_podman_t` 等子域）。

**启用**（建议先在非生产机验证）：

```bash
ENABLE_SELINUX_POLICY=1 bash install.sh ...
```

或升级已有 v0.13 安装：

```bash
cd /path/to/webcasa-source
cd policy && make && sudo semodule -i webcasa.pp
sudo restorecon -RvF /usr/local/bin/webcasa-server /etc/webcasa /var/lib/webcasa /var/log/webcasa
sudo systemctl restart webcasa
# Watch for AVCs:
sudo ausearch -m AVC -ts recent | grep webcasa_t
```

**遇到 AVC 怎么办**：开 issue 贴 `ausearch` 输出，我们会把规则补进 `.te` 下一版。

**v0.14 计划**：根据 v0.13 preview 用户反馈完善规则集，达到"所有 27 个高风险
app-store app + 10 个插件全跑过不触 AVC"后，把 default 从 unconfined 切到
webcasa_t。

## 未来工作 (v0.14+ 候选)

除了完善上面的 webcasa_t policy：

- 声明 `webcasa_t` domain、`webcasa_exec_t` 二进制 type
- 明确允许：读 `/etc/webcasa/*`、写 `/var/log/webcasa/*`、连 `container_var_run_t`、绑 http_port_t
- 拒绝：读其他用户 home、访问 `/etc/shadow` 等

这是 v0.13 候选，不阻塞 v0.12 发布。参考实现可以从 Caddy 的 SELinux policy 开始（`caddy-selinux` package）。

---

## 参考

- [RHEL SELinux User's and Administrator's Guide](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html-single/using_selinux/index)
- [Podman SELinux docs](https://docs.podman.io/en/latest/markdown/podman-run.1.html#security-options-security-opt-option)
- [`semanage`, `setsebool`, `restorecon` cheatsheet](https://wiki.centos.org/HowTos/SELinux)
- WebCasa v0.12 设计文档风险登记册 R5：`docs/07-podman-v0.12.md`
