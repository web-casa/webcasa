# v0.19.0 — Fork PR preview support (Vercel-style approval gate)

**Release date**: 2026-05-06
**Runtime**: Podman 5.6 — unchanged
**Supported OS**: AlmaLinux / Rocky Linux / RHEL / CentOS Stream / Fedora — 9 and 10 series

> **No breaking changes.** Fork PR support is opt-in per project
> (`accept_fork_pr_previews`, default off). Existing v0.14-v0.18
> deployments see no behavior change.

## What's new

The v0.14 Preview Deploy webhook handler rejected fork PRs because
the build pipeline wasn't safe (clone target was always the base
repo + `head.ref`, which doesn't exist in the fork). v0.19 lifts that
restriction with a security-first design.

### Vercel-style approval gate (Option A — gate at build, not URL)

When `accept_fork_pr_previews=true` is set on a project, the webhook
flow becomes:

1. PR opens → preview row created
   (`is_fork_pr=true, approved=false, status=awaiting_approval`).
   **No clone, no build, no container — the fork's code does NOT
   execute.**
2. Admin reviews the PR diff on GitHub + clicks **Approve** in the
   Previews tab.
3. Build runs against the **fork's clone_url**, **pinned to the
   exact SHA admin approved** (via `git fetch <url> <sha>` +
   `rev-parse` verification — not `git clone --branch`, which would
   race fork-author force-pushes).
4. Container starts, Caddy host created, URL goes live.
5. Subsequent pushes to the same PR are gated against the previously
   approved SHA: a **force-push resets approval AND tears down the
   host immediately**. Admin must re-review the new SHA before any
   new code runs.
6. Admin can **Revoke** at any time → host torn down, container
   kept for inspection.

Strict gate semantics: the URL stops resolving the moment status
flips to `awaiting_approval` — no Vercel-style "old version stays
live until new approval" trade-off. Safety over availability.

### Secret env var marking

Marking an env var as `secret=true` excludes it from fork PR builds
**even after approval**. Use for API keys / signing secrets that fork
authors must not be able to read by adding logging code:

```javascript
// In a malicious fork PR's build:
console.log(process.env.STRIPE_SECRET_KEY);  // empty if marked secret
```

UI: per-row Switch in the env vars tab. Value field auto-masks
(`type=password`) when toggled on.

### Twelve review rounds, 23 findings landed

9 rounds of Codex review + 1 round of Claude `/security-review` +
2 follow-up Codex rounds. All findings (10 H + 9 M + 2 L + gofmt)
fixed in-tree.

| Round | Severity | Description |
|-------|----------|-------------|
| R1-H1 | High | ApprovePreview race left `approved=true, host_id=0` — fixed with cheap pre-gate re-read of approved column |
| R1-H2 | High | `head.repo.clone_url` not validated as github.com — could leak GitHub App token to attacker-controlled host |
| R1-M1 | Medium | `createApprovedHost` failure left row stuck approved-but-hostless |
| R1-M2 | Medium | "Preview is live" PR comment posted before host actually existed |
| R1-L1 | Low | env var value input plain text even when `secret=true` |
| R2-H1 | High | Race window between gate re-read and final status write — fixed with post-finalize reconciliation under per-PR lock |
| R2-H2 | High | `extractHost` stripped `@` everywhere, allowing `https://evil.test/a@github.com/repo` past the github.com guard — fixed with `net/url.Parse` + `Hostname()` |
| R2-H3 | High | RevokePreview cleared `host_id` even on DeleteHost failure — orphan Caddy host serving traffic |
| R2-M1 | Medium | `nixpacks_installer` SSE writer not goroutine-safe — interleaved corrupted frames |
| R3-H1 | High | Main host-create branch ran lock-free — RevokePreview race could land `approved=false, host_id>0` |
| R4-H1 | High | **Design**: approval only gated URL, not build/run — fork code executed with non-secret env vars regardless of approval. Fixed by deferring goroutine spawn until ApprovePreview triggers `spawnPreviewBuild` (Option A) |
| R4-H2 | High | RevokePreview retry skipped when `approved=false && host_id>0` — fixed idempotency check to require both |
| R4-H3 | High | Approval persisted across force-push — fork author could approve-then-push-malicious. Fixed with `ApprovedHeadSHA` per-SHA approval |
| R4-M1 | Medium | clone_url scheme/path not strictly validated — required `https://github.com/<head.repo.full_name>` exact match |
| R5-M1 | Medium | CreatePreviewWithFork released lock before status write — first approval click could be silently lost |
| R6-H1 | High | Drain of previous build job ran AFTER unlock — second runPreview could spawn against the same preview |
| R7-H1 | High | Deadlock between runPreview holding per-PR lock at host gate and CreatePreviewWithFork waiting to drain. Fixed by removing lock from runPreview entirely; CAS-style conditional UPDATEs (`WHERE host_id=0 AND generation=gen AND approved=true`) with DeleteHost-orphan rollback |
| R8-H1 | High | Webhook handler validated head.ref non-empty but not head.sha — empty SHA bypassed R4-H3 force-push fence |
| R8-M1 | Medium | DeleteHost ran AFTER 30s drain — URL stayed live up to 30s after status flipped to awaiting_approval |
| R9-M1 | Medium | DeleteHost moved under per-PR lock before drain (strict gate semantics) |
| R9-M2 | Medium | DeleteHost failure path lost host_id reference — admin couldn't retry. Fixed: only clear host_id on success |
| **R10-H1** | **High** | **Claude `/security-review` finding**: `git clone --depth 1 --branch` fetches HEAD-of-branch at clone-execution time, not the approved SHA. Fork author force-push between admin approval and the clone could substitute unapproved code into the build with project's non-secret env vars. The Caddy host CAS-gate kept the URL dark, but the container ran the substituted code on the panel's network. **Fix**: new `CloneAtSHA` pins clone via `git fetch <url> <sha>` + `git rev-parse HEAD` verification |
| R11-H1 | High | runPreview's first DB-read of `HeadSHA` could see a force-push update committed AFTER goroutine spawn. **Fix**: capture `spawnGen + spawnSHA` at goroutine spawn; runPreview uses snapshots over DB-read for clone target |
| R11-L1 | Low | CloneAtSHA failure paths left partial dstDir — fixed with `defer cleanup-on-error` flag pattern |
| R12 | (clean) | `v019-R12 clean — ready to ship` |

## Compatibility

- Go 1.26+ (unchanged)
- React 19 / Vite 6 (unchanged)
- Podman 5.6 (unchanged from v0.13)
- SQLite (unchanged)

No new system dependencies. Fork PR support is OFF by default per
project — existing deployments unaffected.

## Migration

`AutoMigrate` adds new columns (`accept_fork_pr_previews`,
`is_fork_pr`, `head_repo`, `head_clone_url`, `head_sha`, `approved`,
`approved_at`, `approved_by_user_id`, `approved_head_sha`) with
safe defaults. EnvVars are stored as JSON; the new `secret` field
is optional and defaults to false on parse.

## Upgrade path

```bash
curl -sSL https://web.casa/install.sh | bash -s -- --upgrade
```

To enable fork PR previews on a project:

1. **Audit env vars** — mark API keys / signing secrets as
   `secret` first
2. Project Webhook tab → toggle **Accept fork PR previews** ON
3. (Optional) Set up a notification webhook so you're alerted when
   a fork PR needs approval — v0.20+ scope; for now, the Previews
   tab badge is the surface

## Known scope (v0.20+)

- **Per-PR notification** when a fork PR is awaiting approval
  (Discord / email / Slack)
- **Fork allowlist / approver delegation** — restricting approval
  rights to specific GitHub usernames or org members
- **Build queue UI** persistence (env var → DB setting was v0.17;
  per-project caps not yet)

---

**Full Changelog**: https://github.com/web-casa/webcasa/compare/v0.18.0...v0.19.0
