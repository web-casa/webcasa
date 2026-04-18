# NVIDIA GPU 在 Web.Casa v0.12 (Podman) 上的配置

**适用版本**: v0.12.0+
**运行时**: Podman 5.6 (rootful) + podman-compose 1.5
**覆盖硬件**: NVIDIA GPU + CUDA 工作负载（Ollama / Stable Diffusion / 训练 / 推理等）

---

## TL;DR

v0.12 切到 Podman 后，Docker 的 `--gpus all` 和 compose 的
`deploy.resources.reservations.devices` 都**不再工作**。把 NVIDIA GPU 暴露给容器
需要一次性配置 NVIDIA Container Toolkit + CDI (Container Device Interface)：

```bash
# 1. 装 NVIDIA 驱动（如果还没装）+ nvidia-container-toolkit
sudo dnf config-manager --add-repo \
    https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo
sudo dnf install -y nvidia-container-toolkit

# 2. 让 Podman 感知 CDI
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml

# 3. 验证
podman run --rm --device nvidia.com/gpu=all \
    docker.io/nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
```

容器内看到 `nvidia-smi` 输出 = GPU 已暴露。然后在 compose 里把原来的
`deploy.resources.reservations.devices` 块替换成 `devices:`（见下文）。

---

## 为什么是 CDI 而不是 `--gpus`

`--gpus` 是 Docker 专有的 CLI flag，底层调了 nvidia-container-runtime 的 hook。
Podman **不支持** 这个 flag，podman-compose 也不识别 Docker Compose v3 的
`deploy.resources.reservations.devices` 扩展 — 它会静默 drop 这段，容器正常启动
但没有 GPU 设备节点可见，应用 fallback 到 CPU。

**CDI (Container Device Interface)** 是 CNCF 标准，把设备暴露方式抽象成声明式 JSON：

- 主机侧：`nvidia-ctk cdi generate` 探测所有 GPU，生成 `/etc/cdi/nvidia.yaml`
- 容器侧：`--device nvidia.com/gpu=all`（或 `nvidia.com/gpu=0` 指定单卡）
- 运行时：Podman 4.1+ / containerd 1.7+ / CRI-O 都原生支持

CDI 的好处是跨运行时，未来切到别的 OCI runtime 也不需要改 compose。

---

## 前置条件

| 项 | 要求 |
|---|---|
| GPU | NVIDIA with compute capability ≥ 3.5 (Kepler 或更新) |
| 驱动 | NVIDIA 驱动 ≥ 525（推荐 ≥ 550） |
| 内核模块 | `nvidia`, `nvidia_modeset`, `nvidia_uvm` 已 loaded |
| 发行版 | EL9 / EL10 AppStream |
| Podman | 4.1+（v0.12 默认装 5.6，远超最低要求） |

### 检查驱动是否就绪

```bash
# 1. 驱动模块装了吗
lsmod | grep nvidia

# 2. nvidia-smi 能跑吗（主机层面）
nvidia-smi

# 3. 节点存在吗
ls -la /dev/nvidia*
```

三项都 OK 再往下走。驱动没装的话先走 NVIDIA 官方 repo（EL10 参考 RHEL10 文档，
NVIDIA 可能还在 pre-release 状态）。

---

## 安装 NVIDIA Container Toolkit

EL9 / EL10 的 AppStream 里**没有** `nvidia-container-toolkit`，必须加 NVIDIA 官方 repo。

```bash
# 加 repo（libnvidia-container 和 nvidia-container-toolkit 在同一个 repo）
sudo dnf config-manager --add-repo \
    https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo

# 装包
sudo dnf install -y nvidia-container-toolkit

# 验证 CLI 可用
nvidia-ctk --version
```

> **注意**：不要装 `nvidia-docker2` 或 `nvidia-container-runtime`。那两个是
> Docker 时代的产物，在 Podman 上无用且会引入混乱的 hook。

---

## 生成 CDI Spec

这一步探测主机所有 GPU，生成 Podman 能读取的 CDI 文件：

```bash
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
```

输出里应该能看到每张卡：

```text
INFO[0000] Auto-detected mode as 'nvml'
INFO[0000] Selecting /dev/nvidia0 as /dev/nvidia0
INFO[0000] Using driver version 550.xx.xx
INFO[0000] Generated CDI spec with version 0.5.0
```

**每次主机驱动升级或增删 GPU 后都要重新跑一次**。可以写个 systemd
`ConditionPathExists` 单元或放到安装脚本里自动化。

查看生成的文件：

```bash
cat /etc/cdi/nvidia.yaml | head -30
```

里面会列出设备名（`nvidia.com/gpu=0`、`nvidia.com/gpu=1`、`nvidia.com/gpu=all`
等）、要挂载的库（`libcuda.so`、`libnvidia-ml.so` 等）、设备节点。

---

## 验证

```bash
# 用 NVIDIA 官方测试镜像
podman run --rm \
    --device nvidia.com/gpu=all \
    docker.io/nvidia/cuda:12.4.1-base-ubuntu22.04 \
    nvidia-smi
```

预期输出：标准 `nvidia-smi` 表格，列出主机所有卡、驱动版本、CUDA 版本。

如果报错 `Error: could not open /dev/nvidiactl` 或 `failed to run nvidia-smi`：

1. 检查 `/etc/cdi/nvidia.yaml` 是否存在
2. `podman info | grep -i cdi` 应该看到 `cdi: true`
3. 如果是 rootless，记得 `--security-opt label=disable`（v0.12 是 rootful，
   这条不适用）

---

## 修改现有 Compose 文件

Web.Casa app-store 的 **ollama-nvidia** 应用是唯一被静态审计 flag 到的 GPU 应用。
原 compose：

```yaml
# ❌ v0.12 下不工作
services:
  ollama:
    image: docker.io/ollama/ollama
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]
```

改成 CDI 写法：

```yaml
# ✅ Podman + podman-compose 1.5 下工作
services:
  ollama:
    image: docker.io/ollama/ollama
    devices:
      - nvidia.com/gpu=all     # 全部 GPU
      # - nvidia.com/gpu=0     # 或指定单卡
      # - nvidia.com/gpu=0,1   # 或多卡
    environment:
      - NVIDIA_VISIBLE_DEVICES=all     # 有些镜像还看这个环境变量
```

### 在 Web.Casa UI 里改

1. **Container → Compose Stacks**
2. 找到 ollama 相关的 stack，点 **Edit**
3. 删除 `deploy:` 整块
4. 在服务下加 `devices: [nvidia.com/gpu=all]`
5. **Save & Restart**

---

## 一台机器多张 GPU 的场景

```yaml
services:
  ollama-a:
    image: docker.io/ollama/ollama
    devices:
      - nvidia.com/gpu=0

  ollama-b:
    image: docker.io/ollama/ollama
    devices:
      - nvidia.com/gpu=1
```

每张卡独立分配，两个容器互不干扰。

MIG（Multi-Instance GPU，A100/H100）分片也支持：

```yaml
devices:
  - nvidia.com/mig=0:7
```

具体分片名字 `nvidia-ctk cdi generate` 生成的 yaml 里能查到。

---

## 故障排查

### 容器能启动但 nvidia-smi 找不到

```bash
# 主机侧确认 CDI 正确
podman info --format json | jq '.host.cdiSpecDirs'
# 应该返回 ["/etc/cdi", "/var/run/cdi"]

# 直接跑 nvidia-smi 验证 CDI 有效
podman run --rm --device nvidia.com/gpu=all \
    docker.io/nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
```

如果上面那个 direct run 成功但 compose 不行，问题在 compose 文件语法
(podman-compose 把 `devices:` 正确传下去了吗)。

### `failed to create shim: OCI runtime create failed`

常见于驱动版本和容器内 CUDA 版本不兼容。容器内的 `libcuda.so.xxx` 必须 ≤ 主机
驱动支持的最高 CUDA 版本。查 [NVIDIA 兼容矩阵](https://docs.nvidia.com/deploy/cuda-compatibility/)。

### SELinux 拒绝访问 `/dev/nvidia0`

EL9/EL10 默认 SELinux enforcing。如果日志里看到 `denied  { read } ... nvidia0`：

```bash
# 临时：把容器改到 permissive 上下文
podman run --security-opt label=type:container_runtime_t ...

# 永久：写 SELinux policy（推荐）
sudo setsebool -P container_use_devices 1
```

### 每次重启主机后 GPU 不可用

NVIDIA 持久化模式没开，驱动在卡空闲时卸载：

```bash
sudo nvidia-smi -pm 1  # enable persistence mode
sudo systemctl enable nvidia-persistenced
```

### 升级驱动后 CDI spec 失效

CDI 文件里记录了具体的驱动版本和库路径。升级驱动后必须重新生成：

```bash
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
```

忘了这一步会看到 `libcuda.so.550.xx.xx: cannot open shared object file`
之类的错误。

---

## Web.Casa 未来的自动化 (v0.13+ 候选)

当前需要管理员手动执行上面的 3 步（装 toolkit → 生成 CDI → 改 compose）。
v0.13 候选计划：

- `install.sh --gpu=nvidia` 标志：自动加 NVIDIA repo + 装 toolkit + 生成 CDI
- App Store GPU 标记：ollama-nvidia / sd-webui 等应用有 GPU badge，
  UI 提供 "自动配置 GPU" 按钮
- 可选：cron 监听驱动 RPM 升级事件，自动重新生成 CDI

这些都不是 v0.12 阻塞项，Phase 5 以后评估。

---

## 参考

- [CDI 规范 (CNCF)](https://github.com/cncf-tags/container-device-interface)
- [NVIDIA Container Toolkit 安装指南](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
- [Podman 4.1 CDI 支持公告](https://github.com/containers/podman/releases/tag/v4.1.0)
- WebCasa compose 审计报告: `docs/podman-compose-audit.json`
