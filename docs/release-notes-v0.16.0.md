# v0.16.0 — Defer-cleanup batch

**Release date**: 2026-05-05
**Runtime**: Podman 5.6 (RHEL AppStream) + podman-compose 1.5 (EPEL) — unchanged
**Supported OS**: AlmaLinux / Rocky Linux / RHEL / CentOS Stream / Fedora — 9 and 10 series

> **No breaking changes.** v0.16 is concurrency / security hardening on
> the deploy plugin. No new features, no config changes required.
> Upgrade in place.

## Three deferred items shipped

v0.14 (Phase A) and v0.15 (Phase B) each closed long Codex review
loops but consciously deferred a handful of Medium-severity items to
keep the release scope manageable. v0.16 is the cleanup pass — no
new features, just collecting all three deferrals into one ship.

### #7 — Panel-wide concurrent-build cap (NEW)

**Problem**: `Build()` per-project dedup (the v0.11 SingleFlight) only
prevents the SAME project from running twice concurrently. An
unrelated webhook flood — say, 20 projects' git pollers all firing
after a GitHub outage — would spawn N goroutines that each `git
clone` + `docker build`, OOM-ing a 2 GiB minimum-spec host.

**Fix**:
- New `WEBCASA_MAX_CONCURRENT_BUILDS` env var, default 3, capped at 64.
- `Build()` does a non-blocking semaphore acquire; on full returns the
  new `ErrBuildQueueFull` which API handlers map to **HTTP 503**.
- GitHub webhooks retry on 503 per their backoff schedule, so we never
  grow an in-memory queue (the failure mode this fixes).
- Slot held for the entire coalesced loop — a same-project pending
  rebuild reuses our slot rather than re-acquiring (its Docker context
  + clone dir are still committed).
- Released exactly once via `defer` in `buildLoop`.

**Operator notes**: tune with `WEBCASA_MAX_CONCURRENT_BUILDS=N` in
your systemd unit / env file. Default 3 is conservative for a 2 GiB
host. Upper bound (64) prevents runaway misconfiguration.

### #5 — Per-(project_id, pr_number) preview lock (R11-M1)

**Problem**: the v0.14 `createMu` was a single global mutex covering
ALL preview Create + Delete operations. `DeletePreview`'s destructive
cleanup phase (Caddy DeleteHost + container removal + image rmi +
RemoveAll) holds the lock for several seconds. At scale (many active
PRs across many projects) this serialized the entire panel.

**Fix**:
- `previewLocks sync.Map` keyed by `previewKey{ProjectID, PRNumber}`.
- Lazily LoadOrStore'd per PR via `previewLock(projectID, prNumber)`;
  evicted in DeletePreview after the row is successfully deleted (PR
  is terminal, no more webhooks expected for that number).
- All R10/R11/R12 critical sections preserved — same scope, just
  per-key rather than global.

**Two webhooks for different previews now run in parallel**; only
same-PR Create/Delete serialize.

### #6 — Main `Build()` token-via-env (R8-M4)

**Problem**: preview deploy used `GIT_CONFIG_COUNT` env-var token
delivery since v0.14 R6-H1, but the **main project Build path** still
used `ConvertToHTTPS` which embeds the GitHub App / OAuth token
directly in the URL. The token surfaced in `git remote -v` output and
was visible on disk in the repo's `.git/config`.

**Fix**:
- `GitClient.Clone` and `GitClient.Pull` take `httpsToken` as a
  separate parameter (was: token embedded in URL).
- `injectHTTPSTokenEnv` helper extracted; reused by **all four** call
  sites: `Clone`, `Pull`, `CloneToDir`, and `lsRemoteHead`.
- `Builder.Build` signature: takes `httpsToken`.
- `service.go runBuildOnce` and `poller.go lsRemoteHead` now resolve
  credentials, call `ConvertSSHToCleanHTTPS` to derive the clean URL,
  and pass the token separately.
- **Token never appears in argv (visible to `ps`), URL (visible to
  `git remote -v`), or on-disk git config.**

**Migration**: existing v0.15 installs have GitHub tokens already
embedded in the repo's `git remote origin` URL. The first Pull after
upgrading silently overwrites the remote with the clean URL. No
manual migration needed.

#### GitHub-host guard (v0.16-R1-H1)

Added in Codex review Round 1. `ConvertSSHToCleanHTTPS` accepts any
SSH/HTTPS host. With `auth_method=github_app/github_oauth` paired
with a misconfigured non-GitHub `git_url` (e.g.
`git@gitlab.com:owner/repo`), our code would have injected the
GitHub installation token into Authorization headers sent to
gitlab.com — a token leak via that host's access logs.

**Fix**: hard-error when `extractHost(converted) != "github.com"`
before any git command runs. Both `runBuildOnce` and `lsRemoteHead`
apply the guard.

## Codex review summary

Two rounds, both small (defer items had already been Codex-reviewed
when originally identified):

| Round | Findings landed |
|-------|-----------------|
| R1    | 1 High + 1 Low (2) |
| R2    | _(clean — verification pass)_ |

## Compatibility

- Go 1.26+ (unchanged)
- React 19 / Vite 6 (unchanged)
- Podman 5.6 (unchanged from v0.13)
- SQLite (unchanged)

No new system dependencies. No new mandatory configuration.
`WEBCASA_MAX_CONCURRENT_BUILDS` is optional (default 3).

## Upgrade path

```bash
# Pre-built binary
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade

# From source
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade-from-source
```

## What's NOT in this release

- **Fork PR previews** (still v0.17+ scope — needs admin-approval UI
  gate + per-PR clone URL + security review).
- **Build queue settings UI** — `WEBCASA_MAX_CONCURRENT_BUILDS` is an
  env var only; a panel setting can land in v0.17 if there's demand.

---

**Full Changelog**: https://github.com/web-casa/webcasa/compare/v0.15.0...v0.16.0
