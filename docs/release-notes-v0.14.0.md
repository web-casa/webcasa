# v0.14.0 — Preview Deploy Phase A

**Release date**: 2026-04-19
**Runtime**: Podman 5.6 (RHEL AppStream) + podman-compose 1.5 (EPEL) — unchanged from v0.13
**Supported OS**: AlmaLinux / Rocky Linux / RHEL / CentOS Stream / Fedora — 9 and 10 series

> **No breaking changes.** v0.14 adds a new opt-in feature (preview deployments)
> on the deploy plugin without touching existing project / Docker / Caddy
> behavior. Upgrade in place from v0.13.

## Headline feature: ephemeral per-PR preview environments

GitHub `pull_request` webhook → ephemeral subdomain
(`pr-N-slug-id.<wildcard>`) → isolated container build → Caddy reverse-
proxy host. PR `closed` → full teardown. Backend-complete; frontend
"Previews" tab and build-log streaming arrive in **Phase B**.

### What ships

- **Webhook handler** for GitHub `pull_request` events (HMAC-verified,
  shares the existing webhook token + secret). Routes:
  - `opened` / `reopened` / `synchronize` → build-and-expose pipeline
  - `closed` → full teardown
- **`PreviewDeployment` table** tracks per-PR state with composite unique
  index `(project_id, pr_number)`, allocated `BasePort` in
  `[20000, 25000)`, two-slot alternation, and a monotonic `Generation`
  token for fence-style concurrency control.
- **Admin endpoints**:
  - `GET /api/plugins/deploy/projects/:id/previews`
  - `DELETE /api/plugins/deploy/previews/:previewId`
- **Daily GC** sweeps preview rows past `expires_at` regardless of
  status (default 7 days, configurable per project via `PreviewExpiry`).
- **Plugin lifecycle**: `PreviewService.Stop()` cancels in-flight
  goroutines via root context + `WaitGroup` with 30s drain ceiling so
  plugin teardown doesn't leave zombie git/podman children.

### Configuration

Per-project knobs (UI lands in Phase B; settable via API/DB today):

```
preview_enabled   bool   // gate the feature per project
preview_expiry    int    // days to keep preview alive (default 7)
github_token      string // for posting PR comments (Phase B)
```

Panel-wide:

```
wildcard_domain   string // e.g. "preview.example.com" — required
                         // for the subdomain pr-N-slug-id.<domain>
```

A wildcard DNS record must point at the panel's host. TLS is handled by
Caddy automatically once the upstream creates the host (per existing
project flow).

### Concurrency model — twelve rounds of Codex review

The preview pipeline is a state machine over external resources
(container × 2 slots, Caddy host, image, on-disk source/log dirs)
driven by webhooks that can fire multiple times in milliseconds and
DB rows that can be deleted mid-build. Twelve rounds of independent
Codex review hardened the design against every race we could
enumerate:

#### Two-slot port alternation

Each rebuild flips to the unused slot:

```
slot 0 → BasePort           (e.g. 20137)
slot 1 → BasePort + 5000    (e.g. 25137)
```

The Caddy upstream points at the currently-serving slot. New container
starts on the other slot's port; old container stops only after Caddy
traffic has moved. A failed rebuild leaves the previous version live
— **no rename, no port-rebind, no 502 windows**.

#### Generation token on every DB write

`PreviewDeployment.Generation` is a monotonic int. `CreatePreview`
(rebuild trigger) and `DeletePreview` both bump it. `runPreview`
snapshots gen on entry; **every** subsequent DB write
(`setStatus` / `markFailed` / `host_id update` / final slot transition)
is gated by `WHERE generation = snapshot`. A stale goroutine whose
`ctx.Cancel()` arrived too late finds its writes rejected with
`RowsAffected==0` and tears down its own staging container instead of
corrupting state.

#### Lock discipline

A per-`PreviewService` `createMu` mutex serializes the critical
sections that must be atomic — `upsert + jobs-map atomic swap`
(CreatePreview) and `bump + capture + job-snapshot + cleanup`
(DeletePreview). The 30-second drain windows release the lock so
unrelated webhooks aren't blocked.

#### TCP readiness probe

`waitForPortOpen` (500ms tick / 30s cap) replaces the old fixed sleep.
A crashing container fails fast and is torn down before Caddy ever
sees it.

#### Token via env var

HTTPS clones for `github_app` / `github_oauth` projects deliver the
installation token via the `GIT_CONFIG_COUNT` env-var ladder, scoped
to `http.https://<host>/.extraHeader`. The token never appears in
`ps` output or `git remote -v`, and a redirect to a different origin
cannot inherit the Authorization header.

### Bug-bash audit trail

12 review rounds, **43 findings landed** across the run:

| Severity | Count |
|----------|-------|
| Critical | 3     |
| High     | 22    |
| Medium   | 14    |
| Low      | 4     |

3 Mediums consciously deferred to v0.15:

- **R8-M3**: `DeleteProject` continued past preview-cleanup failure.
  *Effectively resolved* by R9-M1 (abort-on-error + reordered to run
  preview cleanup first).
- **R8-M4**: main `Build()` still uses `ConvertToHTTPS` which embeds
  the GitHub App token in the URL (visible to `git remote -v`).
  Preview path uses the env-var approach; main-path migration is a
  multi-file refactor scoped to v0.15.
- **R11-M1**: `createMu` is per-PreviewService, so DeletePreview's
  destructive cleanup phase briefly blocks unrelated CreatePreview
  webhooks. Acceptable at single-project / low-PR-rate volumes;
  planned `sync.Map`-based per-`(project_id, pr_number)` lock is
  v0.15.

The full per-round commit history is preserved on
`fix/v0.14-preview-codex-review` (10 commits). The squashed result
on `main` keeps `git blame` readable.

### Migration

Pre-v0.14 `PreviewDeployment` rows (none in production — Phase A was
unreleased) are dropped on first start by a guard in `Init()` that
detects the missing `base_port` column. **Other plugin data is
unaffected.**

If you've been running a pre-release Phase A build and have manually-
created preview rows, re-trigger them via PR webhook after the
upgrade — the new schema requires `BasePort`, `Generation`, `Slot`
columns that weren't in the prior layout.

### Operator notes

- **Wildcard DNS prerequisite**: set `wildcard_domain` in panel
  settings + a `*.preview.example.com` A record before enabling
  previews on any project. Misconfiguration causes a clear error at
  webhook time, not silent failure.
- **Disk usage**: each active preview holds one container image
  (~hundreds of MB depending on builder), one source checkout, and
  one log directory. The daily GC cleans expired rows; manual
  pruning via the admin endpoint is also available.
- **Concurrent build limit**: there's no panel-wide cap. A flood of
  `synchronize` webhooks across many projects could thrash the host;
  v0.15 will add a build-queue depth knob.

## Other changes

- **`backup` plugin goroutine lifecycle hardening** (R5+ sweep):
  AI-triggered backups now run via `Service.TriggerAsync` which
  registers with the service `WaitGroup`. Plugin `Stop()` waits up
  to 60s for in-flight `kopia` snapshots to finish, preventing the
  prior race where a backup write could land after the DB handle
  closed.
- **`deploy` plugin Rollback re-read** (R5+ sweep): `Rollback` now
  verifies the project row still exists after starting the rollback
  container; if the project was deleted concurrently, the new
  container is removed and a clear error returned.

## Compatibility

- Go 1.26+ (unchanged)
- React 19 / Vite 6 (unchanged)
- Podman 5.6 (unchanged from v0.13)
- SQLite (unchanged)

No new system dependencies. No new mandatory configuration.

## Upgrade path

```bash
# Pre-built binary
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade

# From source
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade-from-source
```

If you don't intend to use preview deployments, no further action is
needed — the feature is gated by the per-project `preview_enabled`
flag (default `false`).

---

**Full Changelog**: https://github.com/web-casa/webcasa/compare/v0.13.0...v0.14.0
