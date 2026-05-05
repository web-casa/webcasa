# Preview Deploy — operator guide

> **Audience**: panel admins enabling preview deployments for the
> first time. If you're a developer wanting to USE previews on a
> project that already has them set up, just open a PR — the panel
> handles everything else.

This guide walks through setting up the v0.14+ preview-deploy feature
end-to-end on an existing Web.Casa install. ~15 minutes, mostly
DNS + GitHub config.

---

## What you get

When enabled on a project, every GitHub pull request gets its own
ephemeral container + subdomain:

```
https://pr-42-myapp-7.preview.example.com
       └────┬─────┘ └─────┬──────────┘
   PR # + slug        your wildcard domain
```

- **opened / reopened**: build + start a fresh container, post the URL
  as a PR comment
- **synchronize** (new push to PR branch): rebuild and update the
  container in place (zero-downtime via two-slot port alternation)
- **closed**: tear down everything (container, image, host, source dir,
  PR comment)

Plus a daily GC removes previews past their expiry (default 7 days).

---

## Prerequisites

- Web.Casa **v0.14+** installed (run `cat /opt/webcasa/VERSION` to check)
- Project deployed in **Docker mode** (`deploy_mode = docker`).
  Bare-metal projects don't support previews.
- A registered domain you control DNS for.
- A GitHub repo (GitLab / self-hosted Gitea aren't yet supported by
  the webhook handler — pull_request payload schema is GitHub-specific).

---

## Step 1 — Wildcard DNS

Pick a subdomain to dedicate to previews. Examples:
- `preview.example.com` → previews live at `pr-N-slug-id.preview.example.com`
- `pr.app.example.com` → previews at `pr-N-slug-id.pr.app.example.com`

Add an `A` record for the wildcard at your DNS provider:

```
*.preview.example.com    A    <your panel server's public IPv4>
```

If you have IPv6, also add:

```
*.preview.example.com    AAAA <your panel server's public IPv6>
```

**Verify**:

```bash
$ dig +short pr-test.preview.example.com
1.2.3.4    # ← your panel's IP
```

If `dig` returns nothing, wait for DNS propagation (up to 1 hour) and
retry.

---

## Step 2 — Register the wildcard with the panel

In the panel, go to **Settings → General → Wildcard preview domain**
and enter the bare suffix (no scheme, no path):

```
preview.example.com
```

Click **Save**. The panel validates it as a DNS-suffix and rejects
typos like `https://preview.example.com/` or `*.preview.example.com`.

---

## Step 3 — Project: enable previews + paste GitHub token

Open the project's detail page → **Webhook** tab. Scroll to
**Previews**:

1. Toggle **Enable preview deployments** ON
2. Adjust **Preview expiry (days)** if 7 isn't right (1-365)
3. Paste a **GitHub token** for PR comments (optional but recommended):
   - Personal Access Token (classic): `repo` scope is enough
   - Fine-grained PAT: read+write on `Pull requests` for the target repo
   - GitHub App installation token: works automatically if you've already
     configured GitHub App auth on the project

Save (auto-saves on input blur).

> **Note**: without a token, previews still build and become reachable
> at the subdomain — you just don't get the auto-generated PR comment
> with the link.

---

## Step 4 — GitHub: webhook setup

The panel's existing webhook URL handles both push events (auto-deploy)
AND `pull_request` events (preview deploys). One webhook covers both.

If you've already wired the webhook for auto-deploy, **just enable the
`Pull requests` event** in your existing webhook config:

1. GitHub repo → **Settings → Webhooks** → click the existing
   `https://your-panel.example.com/api/plugins/deploy/webhook/<token>`
2. Under **Which events would you like to trigger this webhook?** →
   "Let me select individual events"
3. Check ☑ **Pull requests** (in addition to ☑ Pushes)
4. Save

If this is a fresh setup, get the URL + secret from the panel:

- Project detail → **Webhook** tab → copy URL + secret
- Add to GitHub: `Content type: application/json`, `Secret: <paste>`,
  Events: `Pushes` + `Pull requests`

---

## Step 5 — Verify with a real PR

Open any PR against the configured project's main branch. Within
seconds:

1. **Project detail → Previews tab** shows a new row for the PR
   (status: `pending` → `building` → `running`)
2. **Build log** is viewable live by clicking the magnifying-glass icon
   on the row — Server-Sent Events stream the log as it's generated
3. **PR comment** appears on the GitHub PR with the live URL (if you
   added a token in Step 3)
4. The URL `https://pr-N-slug-id.preview.example.com` is live behind
   automatic Caddy TLS

Push a new commit to the PR branch — the preview rebuilds in place.
The PR comment is **edited** (not duplicated).

Close or merge the PR — preview tears down within seconds.

---

## Tuning concurrency (v0.16+)

If your panel handles many concurrent PRs across many projects, the
default cap of 3 simultaneous builds may be too tight. Two ways to
adjust:

### From the UI (v0.17+)

**Settings → General → Max concurrent builds**: set 1-64. Takes effect
on the next panel restart (`systemctl restart webcasa`).

### Via env var (overrides UI)

Edit `/etc/systemd/system/webcasa.service`:

```ini
[Service]
Environment="WEBCASA_MAX_CONCURRENT_BUILDS=8"
```

Then `systemctl daemon-reload && systemctl restart webcasa`.

When the cap is hit, new builds return HTTP 503. GitHub webhook
delivery automatically retries on 503 per its exponential backoff
schedule, so no preview is lost — only delayed during a burst.

---

## Troubleshooting

### "wildcard_domain not configured" error on PR webhook

You forgot Step 2. Check **Settings → General → Wildcard preview
domain** is non-empty.

### "preview deployments require Docker deploy mode" error

The project was created in Bare mode. Convert via the project edit
dialog (`deploy_mode = docker`) and rebuild.

### "preview deployments are not enabled" error on PR webhook

The project's `preview_enabled` toggle is off. Step 3.

### "fork PR previews are not supported" message

Working as designed — fork PRs aren't supported until v0.17+. Same-
repo branch PRs (the typical workflow with branch protection +
required reviews) work fine.

### Preview never gets past "building"

Click the build log icon on the Previews tab row — the SSE stream
shows what `git clone` and `docker build` are doing in real time.
Common causes:

- **Token wrong / expired**: see "convert git URL for token auth"
  errors → re-enter the token in Step 3
- **Dockerfile / nixpacks build failure**: same as a normal failed
  deploy; the AI diagnosis (if AI plugin enabled) shows up after
  the build fails
- **Container OOM during build**: bump host RAM or
  `WEBCASA_MAX_CONCURRENT_BUILDS` down

### Preview shows running but URL 502s

Two-slot alternation should make this impossible (Caddy upstream is
swapped only after the new container's port is reachable via
`waitForPortOpen`). If it happens, file an issue with the build log
+ `docker ps | grep webcasa-preview-<id>`.

### Caddy host count grows unbounded after PR closes

GC sweeps daily. To force-clean now:

```bash
# In the panel: Previews tab → Delete (per row)
# OR via API:
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://your-panel.example.com/api/plugins/deploy/previews/<id>
```

The cleanup removes BOTH slot containers + Caddy host + image +
source dir + log dir + DB row + PR comment. If any step fails, the
row is preserved as `cleanup_failed` so you can retry.

---

## Architectural notes (for the curious)

The Phase A backend (v0.14) and Phase B UI (v0.15) went through 18
rounds of independent Codex review across 62 findings. Some things
worth knowing as an operator:

- **Two-slot port alternation**: each preview has a fixed `BasePort`
  (allocated once in `[20000, 25000)`); deploys alternate between
  `BasePort` and `BasePort+5000`. Caddy upstream tracks the
  currently-serving slot. A failed rebuild leaves the previous version
  live — no 502 windows.
- **Generation token**: every preview row has a `generation` int that
  bumps on each rebuild + delete. All in-flight build goroutines
  fence their DB writes by `WHERE generation = snapshot`, so a stale
  goroutine whose ctx-cancel arrived too late silently aborts instead
  of corrupting state.
- **Per-PR lock (v0.16+)**: same-PR Create + Delete serialize via a
  per-`(project_id, pr_number)` `sync.Mutex`. Different PRs run
  fully in parallel.
- **Token handling**: GitHub App tokens are delivered to git via the
  `GIT_CONFIG_COUNT` env var ladder (scoped to `github.com` origin).
  The token never appears in `ps` argv, in `git remote -v` output, or
  in the repo's `.git/config`.

For the gory details, see `changelog.md` `[0.14.0]` / `[0.15.0]` /
`[0.16.0]` sections + `docs/release-notes-v0.{14,15,16}.0.md`.
