# v0.12.0 — Podman Only Edition

**Release date**: 2026-04-19
**Runtime**: Podman 5.6 (RHEL AppStream) + podman-compose 1.5 (EPEL)
**Supported OS**: AlmaLinux / Rocky Linux / RHEL / CentOS Stream / Fedora — 9 and 10 series

> ⚠️ **Breaking change**: v0.12 switches the default container runtime from
> Docker Engine to Podman 5.6. If you're running v0.11, you **must rerun**
> `install.sh` to migrate. Existing `docker` / `docker-compose` CLI commands
> keep working via the `podman-docker` shim and `podman-compose`, but the
> underlying engine is different and existing Docker containers / images /
> networks / volumes need to be rebuilt under Podman (see migration section
> in `docs/07-podman-v0.12.md`).
>
> v0.11 is EOL as of this release.

## Why switch

- **RHEL 10 + Docker is broken.** Docker 29 fails to start on RHEL 10 with
  `Extension addrtype revision 0 not supported` because the distro
  deprecated iptables kernel modules. The official fix requires manual
  `dnf install kernel-modules-extra` + `modprobe`, defeating the one-line
  install.
- **Podman is RHEL-native.** Ships in AppStream, no daemon, deeply
  integrated with SELinux / systemd / cgroupv2, long-term support.
- **SDK compatibility is preserved.** The Docker Go SDK v27 auto-negotiates
  to Podman's v1.41/v1.43 compat API; WebCasa needed **zero** API-layer
  changes in service/client code. The `podman-docker` shim keeps the
  `docker` CLI working transparently.

## What's in the box

### App-store compatibility (269 apps)

Validated via static analysis + three rounds of live VPS testing on a fresh
AlmaLinux 9 + Podman 5.6 host:

- **Static audit** (`scripts/compose-audit.py`, `docs/podman-compose-audit.json`):
  234 clean / 10 warning / 25 info / 0 critical
- **Live VPS validation**: 20/25 apps PASS. Remaining fails are all explainable:
  - 2 catalog bugs (upstream image tag deleted): `sshwifty`, `scrypted`
  - 3 user-config required: `crowdsec`, `mdns-repeater`, `zigbee2mqtt`
  - 2 skipped for hardware: `windows` (needs KVM), `ollama-nvidia` (needs GPU)

Seven production bugs found and fixed during validation (see commits in
`changelog.md [0.12.0]` section), including short-name resolution,
SELinux bind-mount relabeling, docker.sock `label=disable` injection, and
quoted-volume handling.

### New documentation

- `docs/07-podman-v0.12.md` — complete design document + migration notes
- `docs/selinux.md` — EL9/EL10 operator guide for running under SELinux enforcing
- `docs/nvidia-gpu.md` — CDI setup for GPU workloads (Ollama, Stable Diffusion)

### Cross-cutting security fixes

In addition to the Podman migration, v0.12 lands two systematic security
pattern fixes uncovered during the Docker plugin audit:

- **WebSocket authentication**: moved the JWT from `?token=<jwt>` query
  parameters to the `Sec-WebSocket-Protocol` subprotocol header across
  every WebSocket surface (terminal, database logs, monitoring metrics,
  Docker logs). Tokens no longer appear in proxy access logs or browser
  devtools URL bars.
- **Subprocess lifecycle**: new `internal/execx` helper wraps
  `exec.CommandContext` with `Setpgid + Cancel hook + WaitDelay` so SSE
  client disconnect kills the entire subprocess tree, not just the outer
  bash. Applied to the Docker / Kopia / firewalld installers, project
  build commands, cron task runner, and the AI agent's shell exec tool.

## Upgrading from v0.11

1. Stop the WebCasa service: `systemctl stop webcasa`
2. Back up: panel data (`/var/lib/webcasa/`) + any Docker volumes holding
   app state you care about
3. Fetch the new install script: `curl -fsSL https://raw.githubusercontent.com/web-casa/webcasa/main/install.sh -o /tmp/install.sh`
4. Run: `sudo bash /tmp/install.sh` — it will install Podman, migrate the
   systemd unit to the `webcasa` service user, and link the docker.sock
   compatibility symlink
5. Re-create your app-store installations — Podman doesn't automatically
   inherit Docker's storage. WebCasa UI will guide you on first login.

Detailed troubleshooting: `docs/07-podman-v0.12.md` appendix A (rootless
compatibility) + `docs/selinux.md` (four common denial scenarios).

## GPU users

Ollama, Stable Diffusion, and other CUDA workloads need a one-time CDI
setup because Podman does not honour Docker's `--gpus all` or compose
`deploy.resources.reservations`. The full 3-command setup is in
`docs/nvidia-gpu.md`; the short version:

```bash
sudo dnf config-manager --add-repo \
    https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo
sudo dnf install -y nvidia-container-toolkit
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
```

Then in your compose, replace the Docker-style `deploy.resources.reservations.devices`
block with `devices: ['nvidia.com/gpu=all']`.

## Known issues

- Catalog seeds: two upstream images have been deleted
  (`niruix/sshwifty:0.4.3`, `koush/scrypted:20`). Scheduled for Phase 6
  catalog refresh.
- Rootless Podman evaluation: v0.12 runs rootful by default to preserve
  app-store compatibility (14 concrete compatibility risks documented in
  `docs/07-podman-v0.12.md` appendix A). Rootless is a v0.13+ candidate.

## Credits

Designed, implemented, and validated via collaborative AI coding sessions
with Claude Code + Codex, across 29 commits on main and four rounds of
Codex review catching 20+ findings that shipped as fixes within the same
release cycle.
