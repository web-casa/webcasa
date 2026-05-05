# v0.17.0 — Build queue UI + Preview Deploy operator guide

**Release date**: 2026-05-05
**Runtime**: Podman 5.6 — unchanged
**Supported OS**: AlmaLinux / Rocky Linux / RHEL / CentOS Stream / Fedora — 9 and 10 series

> **No breaking changes.** v0.17 is two small UX follow-ups to v0.16
> defer-cleanup. Upgrade in place.

## What's new

### Build queue UI

The v0.16 panel-wide concurrent-build cap was env-var only
(`WEBCASA_MAX_CONCURRENT_BUILDS`). v0.17 surfaces it in the panel:

- **Settings → General → Max concurrent builds** field, range 1-64
- Persisted in the panel DB
- Takes effect on next panel restart (UI shows a clear callout)
- Env var still wins — useful for systemd unit pinning that survives
  DB resets

Backend allowlist now accepts `max_concurrent_builds` with
strict 1-64 integer validation (`internal/handler/setting.go`).
The deploy plugin's `parseMaxConcurrentBuilds` resolves precedence
env var > DB setting > default 3.

### Preview Deploy operator guide

New `docs/preview-deploy-guide.md` walks through end-to-end Preview
Deploy setup for a fresh install:

1. Wildcard DNS record
2. Panel `wildcard_domain` setting
3. Per-project `preview_enabled` toggle + GitHub token
4. GitHub webhook event subscription
5. Verifying with a real PR

Plus tuning concurrency, troubleshooting common errors, and brief
architecture notes pointing at the v0.14/v0.15/v0.16 audit trail (62
findings landed across 18 Codex review rounds).

## Migration

None required. v0.16 → v0.17 is purely additive.

## Compatibility

- Go 1.26+ (unchanged)
- React 19 / Vite 6 (unchanged)
- Podman 5.6 (unchanged from v0.13)
- SQLite (unchanged)

## Upgrade path

```bash
# Pre-built binary
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade

# From source
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade-from-source
```

---

**Full Changelog**: https://github.com/web-casa/webcasa/compare/v0.16.0...v0.17.0
