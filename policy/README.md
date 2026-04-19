# WebCasa SELinux Policy

Custom SELinux Type Enforcement module that moves the WebCasa systemd
service from `unconfined_service_t` (v0.12 default) to a dedicated
`webcasa_t` domain with explicit allow rules.

## Why

v0.12 runs WebCasa under `unconfined_service_t` — works zero-config on
fresh AlmaLinux / Rocky / RHEL 9/10 but gives the panel process roughly
the same access as an unconfined user process. v0.13 narrows the blast
radius:

- Read only its own `/etc/webcasa/*` and `/var/lib/webcasa/*`
- Write only `/var/lib/webcasa/*` and `/var/log/webcasa/*`
- Connect only to the Podman unix socket (`var_run_t`)
- Bind `http_port_t` (panel port + forked Caddy 80/443)
- Execute allow-listed CLIs (`podman`, `podman-compose`, `docker` shim,
  `caddy`, `kopia`) without leaving `webcasa_t`

Anything outside this list triggers an AVC denial.

## Files

| File | Purpose |
|------|---------|
| `webcasa.te` | Type Enforcement source (the rules) |
| `webcasa.fc` | File-context definitions |
| `Makefile` | Wraps `/usr/share/selinux/devel/Makefile` to produce `webcasa.pp` |
| `webcasa.pp` | Pre-built binary module (regenerated per release, committed) |

## Building (maintainer flow)

```bash
dnf install -y selinux-policy-devel make
cd policy
make            # → webcasa.pp
make check      # syntax-only, doesn't need devel Makefile
make clean
```

The release pipeline rebuilds `webcasa.pp` in a pristine AlmaLinux 10
container and commits it before tagging — users don't need
`selinux-policy-devel` on their server.

## Installing (install.sh does this automatically)

```bash
sudo semodule -i policy/webcasa.pp
sudo restorecon -RvF /usr/local/bin/webcasa-server /etc/webcasa \
                     /var/lib/webcasa /var/log/webcasa
sudo systemctl restart webcasa
```

## Uninstalling

```bash
sudo semodule -r webcasa
```

## Debugging AVCs

```bash
# Reproduce the failing action in the panel, then:
sudo ausearch -m AVC -ts recent 2>/dev/null | grep webcasa_t
sudo ausearch -m AVC -ts recent | grep webcasa_t | audit2allow
```

Review audit2allow output carefully — it's permissive. Only add narrow
rules that match documented behaviour back into `webcasa.te`.

## References

- `docs/selinux.md` — operator SELinux guide (denial scenarios, basic debug)
- `docs/07-podman-v0.12.md` R5 — the risk item this module closes
- Fedora SELinux Policy: https://github.com/fedora-selinux/selinux-policy
