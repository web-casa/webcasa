# v0.15.0 — Preview Deploy Phase B

**Release date**: 2026-05-05
**Runtime**: Podman 5.6 (RHEL AppStream) + podman-compose 1.5 (EPEL) — unchanged from v0.13/v0.14
**Supported OS**: AlmaLinux / Rocky Linux / RHEL / CentOS Stream / Fedora — 9 and 10 series

> **No breaking changes.** v0.15 layers UI + observability on top of
> the v0.14 backend. Upgrade in place.

## Headline: Preview deploys are now usable from the panel

v0.14 shipped the backend pipeline (webhook → ephemeral container →
Caddy host → daily GC) but had no UI — admins had to poke the API or
watch the Go logs to see what was happening. v0.15 closes the loop:

- a **Previews tab** on every project shows live + historical preview
  deployments,
- per-preview **build logs stream live** to the browser via Server-
  Sent Events,
- successful deploys **comment the live URL on the GitHub PR** (one
  comment per PR, edited on rebuilds, deleted on teardown),
- **wildcard domain** + per-project **preview settings** are
  configurable from the UI — no SQL required.

## Six features (B1–B6)

### B1 — Previews tab on the project detail page

Gated by the project's `preview_enabled` flag. Renders a table with
PR number, branch, the live URL (clickable when status=running),
status badge, allocated host port, expiry date, and per-row
**View build log** + **Delete** actions. Auto-polls every 3 s while
any preview is mid-build so the badge updates without a manual
refresh.

### B2 — Per-project preview settings (Webhook tab)

Inline switches + inputs:

- `preview_enabled` — gate the feature per project
- `preview_expiry` (days, default 7) — tuned per project
- `github_token` — optional PAT or App installation token used for
  PR comments

All save on blur — no extra Save button per field.

### B3 — Wildcard preview domain (Settings → General)

Required for preview deploys. Validated client + server with the
RFC 1035 rule (≤253 total chars, ≤63 per label, no scheme / path /
wildcard syntax). Empty value disables previews panel-wide.

### B4 — Static build log fetch

`GET /api/plugins/deploy/previews/:id/log` returns the raw
`build.log`, capped at 2 MiB tail (with a `[…truncated to last N
bytes…]` marker). Used for terminal-state previews where streaming
is unnecessary.

### B5 — Live build log streaming via SSE

`GET /api/plugins/deploy/previews/:id/log/stream` opens a Server-
Sent Events connection that:

- ships the existing log content as `event: log` lines,
- polls the file every 500 ms for new bytes (caps each poll at 2
  MiB; emits a marker when truncating),
- polls the row's status every 2 s; emits `event: status` on change,
- emits `event: reset` when the file shrinks (PR rebuild rotated
  `build.log`),
- emits `event: error` on real IO errors (instead of silently
  hanging),
- emits `event: done` when status leaves the in-progress set, then
  closes,
- has a 20-minute hard ceiling so a stuck client can't pin a
  goroutine forever.

The frontend uses a hand-rolled `streamSSE` helper (fetch +
`ReadableStream` + tiny SSE parser) so the JWT can be sent via
`Authorization: Bearer ...` header — browser-native EventSource
doesn't support custom headers. The React state buffer is itself
capped at 1 MiB (with a head-drop marker) to prevent the page from
being OOMed by an exploding build.

### B6 — Automatic PR comments

After a successful deploy, the bot POSTs a "🚀 Preview deployment is
live" comment to the GitHub PR with the URL. The comment ID is
persisted on the row so subsequent rebuilds PATCH the same comment
instead of spamming the PR thread. PR close → DELETE the comment.

Comment posting:

- best-effort — failures (rate limit, transient errors, missing
  token) never fail the deploy
- only falls back to POST when GitHub clearly says the comment is
  gone (404/410); other PATCH failures retry on next deploy to
  avoid duplicate comments
- delete runs as the LAST step of teardown, AFTER the row delete
  succeeds, so partial-cleanup-failure paths preserve the comment
  ID for retry

## Fork PR rejection

`pull_request` webhooks where `head.repo.full_name` !=
`base.repo.full_name` are rejected explicitly with a clear message:

```json
{
  "ok": true,
  "message": "fork PR previews are not supported; only same-repo PRs trigger preview deploys",
  "head": "fork-user/repo",
  "base": "owner/repo"
}
```

The clone path uses the project's base repo URL + `head.ref`, which
silently breaks for fork PRs (head branch doesn't exist in the base
repo). Cross-repo support is intentionally v0.16+ scope — it
requires per-PR clone URLs, a security review of running fork code
with the project's secrets, and an admin-approval gate.

## Six rounds of Codex review

| Round | Findings landed |
|-------|-----------------|
| R1    | 2 High + 5 Medium + 1 Low (8) |
| R2    | 2 High + 2 Medium (4)         |
| R3    | 1 Medium + 2 Low (3)          |
| R4    | clean ✅                      |
| R5    | 2 High + 1 Medium + 1 Low (4) |
| R6    | 2 Low (2)                     |

**19 findings landed across the run, 0 deferred.**

The non-trivial ones (worth knowing if you operate previews):

- **R1-H1** SSE log streaming didn't handle `build.log` truncation
  (a rebuild that re-creates the log file): the stream silently
  stopped emitting bytes
- **R1-H2 / M2** unbounded log read could OOM the panel and the
  React tab — both now capped (2 MiB backend, 1 MiB frontend)
- **R2-H1** SSE `reset` event was emitted but the frontend SSE
  helper had no `onReset` callback — silently dropped
- **R2-H2** the `/settings` PUT endpoint had a hardcoded allowlist
  that rejected `wildcard_domain` — UI saves silently 400'd
- **R3-M1** removing the `binding:"required"` tag to allow empty
  `wildcard_domain` regressed ALL settings (including
  `auto_reload`) — fixed by switching to `*string` to distinguish
  missing from empty
- **R5-H1 / H2** fork PR rejection + FQDN total-length validation
  (described above)

The full per-round commit history is preserved on
`feat/v0.15-preview-phase-b`. The squashed result on `main` keeps
`git blame` readable.

## Compatibility

- Go 1.26+ (unchanged)
- React 19 / Vite 6 (unchanged)
- Podman 5.6 (unchanged from v0.13)
- SQLite (unchanged)

No new system dependencies. No new mandatory configuration.

## Migration

- Pre-v0.14 `PreviewDeployment` rows are dropped on first start by
  the v0.14 guard in `Init()` — unchanged from v0.14.
- The `pr_comment_id` column is added by `AutoMigrate` on first
  start; default 0 means "no comment posted yet" and the next
  successful deploy will POST one. Existing v0.14 previews keep
  working.

## Upgrade path

```bash
# Pre-built binary
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade

# From source
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade-from-source
```

If you don't intend to use preview deployments, no further action is
needed — the feature stays gated by per-project `preview_enabled`
(default `false`).

## Known scope (v0.16+)

- **Fork PR previews** — requires admin-approval UI gate and a
  separate clone URL per preview
- **Per-(project, PR) lock** — DeletePreview's destructive cleanup
  phase still briefly blocks unrelated CreatePreview webhooks via a
  panel-wide `createMu`. Acceptable at low PR volumes; planned
  `sync.Map`-keyed lock will land in v0.16
- **Main `Build()` token-via-env migration** — preview path uses
  `GIT_CONFIG_COUNT` env var so the token never appears in argv;
  main project Build still uses `ConvertToHTTPS` which embeds the
  token in the URL. Not a v0.14/v0.15 regression — a v0.16 cleanup
- **Build queue depth knob** — no panel-wide concurrent-build cap
  yet; a flood of `synchronize` webhooks could thrash the host

---

**Full Changelog**: https://github.com/web-casa/webcasa/compare/v0.14.0...v0.15.0
