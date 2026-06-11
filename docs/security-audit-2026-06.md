# Security Audit & Remediation — 2026-06

A full security review of WebCasa (~56k LOC Go + installer + Docker) was carried
out across six domains: authentication/crypto, command injection, path
traversal/file ops, authorization/IDOR, SSRF/AI/MCP, and deployment/supply
chain. This document records every finding, its fix, and the residual risks /
behavioral changes operators need to know about.

Remediation landed in two commits:

- **Critical/High** — RCE, command injection, auth bypass, privilege escalation.
- **Medium/Low** — crypto-at-rest, 2FA replay, permissions, container &
  supply-chain hardening.

Overall posture going in was already solid: an effective SSRF layer
(`SafeDialContext` re-resolves at dial time, no redirect following), correct
AES-GCM usage (random nonce, no reuse), bcrypt cost 10, header-only auth (so
CSRF is not exploitable), sound CORS (no wildcard-with-credentials), and
zip-slip / upload-filename / cert-domain validation. The findings below are the
gaps that were closed.

---

## Critical / High

### H1 — API token scopes not enforced on REST routes (privilege escalation)
- **Where:** `internal/auth/auth.go` (`validateAPIToken`), `main.go`.
- **Problem:** `wc_` API tokens carry a `Permissions` scope list, but it was
  enforced **only** in the MCP tool layer. Presented directly to the REST API,
  `validateAPIToken` set `user_id` and returned without ever reading
  `Permissions`; RBAC then granted the token its **owner's full role**. A token
  scoped to `["hosts:read"]` could call `DELETE /api/hosts/:id`, `POST /api/users`,
  file writes, DB queries, etc. Tokens also defaulted to `["*"]`.
- **Fix:** `validateAPIToken` parses `Permissions` into the gin context. New
  `RequireTokenScope(scope)` / `RequireTokenScopeForMutations(scope)` /
  `RequireFullScopeForMutations()` middleware, wired onto the `protected`,
  `operatorOnly`, and `adminOnly` groups in `main.go`, **fail closed**: a scoped
  token may not perform any state-changing (POST/PUT/PATCH/DELETE) REST call —
  only an explicit `["*"]` token can. No-op for JWT/session auth (governed by
  RBAC). Empty/missing permissions = no access.

### C1 — MCP tokens grant root-equivalent RCE; default scope `["*"]`
- **Where:** `plugins/mcpserver/{token,handler,tools}.go`.
- **Problem:** `run_command` (`system:write`), `docker_run` (allowed
  `volumes: ["/:/host"]`), `write_file`/`delete_file`, and cron tools executed
  with no confirmation gate, and new tokens defaulted to full `["*"]` access. A
  captured token (note the `mitm_mcp_traffic.db` artifact) = host takeover.
- **Fix:** New tokens default to **no** permissions (empty), never `["*"]`.
  `docker_run` rejects bind mounts whose host side is or is under `/`, `/etc`,
  `/root`, `/var/run/docker.sock`, `/proc`, `/sys`, `/boot`, `/dev`.
  `run_command` keeps its `system:write` requirement, which a least-privilege
  token can no longer satisfy.

### C2 — RunCommand "dangerous command" blocklist trivially bypassable
- **Where:** `internal/plugin/coreapi.go`.
- **Problem:** Substring denylist (`rm -rf /`, `shutdown`, …) bypassed by
  `rm -r -f /`, `echo b64|base64 -d|bash`, `curl host|sh`, etc. Runs as root.
- **Fix:** Whitespace-normalized, lower-cased regex matching catches the
  reordered-flag and remote-pipe-to-shell shapes (`curl|wget … | sh/bash/python`,
  `base64 -d | bash`, `| sh -c`). Documented in code as **best-effort, not a
  security boundary** — the real fix is running command execution as a non-root,
  sandboxed user (tracked as residual risk below).

### H2 — Fork-PR webhook git argument injection (RCE)
- **Where:** `plugins/deploy/{git,handler,preview}.go`.
- **Problem:** Fork-PR payload `head.sha` / `head.ref` (attacker-controlled by
  the PR author) flowed unvalidated into `git fetch --depth 1 <url> <sha>` and
  `git clone --branch <ref>` as **positional** args. A value like
  `--upload-pack=<cmd>` is parsed by git as an option → arbitrary command
  execution. Amplified because webhook HMAC verification was only enforced when
  a secret was configured.
- **Fix:** Webhook boundary validates `head.sha` against `^[0-9a-fA-F]{7,40}$`
  and `head.ref` against `^[A-Za-z0-9._/-]+$` (reject leading `-`/`/`, `..`),
  rejecting with HTTP 400 before any git op (same check re-applied in
  `runPreview`). All externally-influenced git invocations gain
  `--end-of-options`. Fork-PR previews now **require** a configured webhook
  secret.

### H3 — Installer accepts unverified / mismatched binary as root
- **Where:** `install.sh`.
- **Problem:** Checksum verified only "if available", and on actual mismatch it
  merely `warn`ed and continued installing/executing as root. Caddy/Go/Node
  downloads had no verification. Core of a `curl … | sudo bash` install → root
  RCE on a tampered/MITM'd release.
- **Fix:** WebCasa-binary checksum verification is **mandatory** — `fatal`
  (abort) on missing checksum or mismatch. Downloads/extraction moved to
  `mktemp -d` to avoid `/tmp` symlink races. Best-effort SHA256 verification
  added for Caddy, Go, and Node (hard-fail on mismatch, `warn`+continue only if
  the upstream checksum metadata is momentarily unfetchable).

---

## Medium

### M1 — JWT signing method not pinned
- **Where:** `internal/auth/auth.go`.
- **Fix:** `ParseWithClaims` now passes `jwt.WithValidMethods(["HS256"])` and the
  keyfunc asserts `*jwt.SigningMethodHMAC` before returning the secret — closes
  alg-confusion / `alg:none`.

### M2 — TOTP code replay
- **Where:** `internal/service/totp.go`, `internal/model/model.go`.
- **Fix:** `User.LastTOTPTimestep` records the last accepted timestep; codes at
  or below it are rejected and the new timestep is persisted on success. The
  enabling code's timestep is also recorded so it can't be replayed at login.

### M3 — Encryption-at-rest key derived from JWT secret with no KDF
- **Where:** `internal/crypto/crypto.go`, `internal/service/totp.go`.
- **Problem:** `key = SHA256(jwtSecret)`, no salt, no domain separation — one key
  encrypted every credential and TOTP secret.
- **Fix:** HKDF-SHA256 with distinct `info` labels (`webcasa-credentials-v1`,
  `webcasa-totp-v1`). **Backward compatible:** decryption tries the HKDF key,
  then falls back to the legacy SHA256 key, so existing data still decrypts and
  re-encrypts under HKDF on next save. No manual migration required.

### M4 — Login username enumeration (timing)
- **Where:** `internal/handler/auth.go`.
- **Fix:** When the username is not found, a dummy bcrypt comparison runs so the
  response timing matches the user-exists path. Same generic "Invalid
  credentials" response.

### M5 — Docker image runs as root
- **Where:** `Dockerfile`, `docker-compose.yml`.
- **Fix:** Non-root `webcasa` user; `cap_net_bind_service` granted to the Caddy
  binary via `setcap` so it can bind :80/:443 without root; Caddy version pinned
  and checksum-verified; Caddy state moved off `/root` to `/app/caddy` via
  `XDG_DATA_HOME`/`XDG_CONFIG_HOME` (compose volumes updated to match).

### M6 — Data dir / DB world-readable; placeholder secret accepted
- **Where:** `internal/config/config.go`, `scripts/webcasa.env`.
- **Fix:** `dataDir` / logs / backups created `0700`; SQLite DB `chmod 0600`;
  `CHANGE_ME_TO_RANDOM_STRING` added to the rejected insecure-defaults list so
  deploying the template env verbatim fails fast.

---

## Low / Defense-in-depth

### L1 — File manager containment is a no-op when `root_path="/"`
- **Where:** `plugins/filemanager/fileops.go`, `plugin.go`.
- **Problem:** `root_path` defaults to `/` (never set elsewhere) and `safePath`
  short-circuited all symlink/containment checks when root was `/`.
- **Fix:** Removed the `rootResolved == "/"` bypass so symlink-escape resolution
  always runs (root `/` still trivially contains all real paths). Documented
  that `root_path="/"` grants full-filesystem access and is intended for trusted
  single-admin installs. (Default left unchanged to avoid breaking upgrades.)

### L2 — AI endpoints reachable by viewer; read tools can read secrets; self-heal injection
- **Where:** `plugins/ai/{plugin,tools_builtin,selfheal}.go`.
- **Fix:** `/chat`, `/confirm`, `/generate-compose`, `/generate-dockerfile`,
  `/diagnose` moved from any-authenticated to the **operator** router.
  `isPathSafe` now also denies `.env` / `*.env` / `*.pem` / `*.key` / SSH private
  keys / `.jwt_secret`. Self-heal `auto` mode validates that a model-supplied
  `container_id` corresponds to a real local container before acting (bounds the
  DoS surface from prompt-injected container names).

---

## Residual risks & accepted items (not changed in this round)

- ~~**Command execution still runs as root.**~~ **Addressed (v0.20, Release A).**
  `coreapi.RunCommand` now runs arbitrary commands inside a privilege-dropping
  systemd sandbox (`execx.SandboxBashContext`: `DynamicUser`, `NoNewPrivileges`,
  `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp/Devices`, memory/task caps,
  `RuntimeMaxSec`). On hosts without systemd (Docker) the process already runs
  as a non-root user, so the plain fallback is acceptable. The denylist is now
  documented as a fast-fail speed bump, not the boundary. Trade-off: sandboxed
  commands run read-only as a nobody-class user — privileged/mutating ops must
  use dedicated audited tools, not the arbitrary-command path.
- ~~**App Store installs unsigned remote Compose.**~~ **Partly addressed (v0.20,
  Release B).** Images are now pulled and rewritten to immutable
  `name@sha256:<digest>` before `compose up` (install aborts rather than run an
  unpinned image), pinned refs are persisted for drift detection, and installs
  from an unsigned source require explicit acknowledgement (`AcknowledgeUnsigned`
  → HTTP 412 listing the images that would run). **Still open:** a signed-manifest
  / commit-pinning trust model (operator chose digest-pin + warning over a
  signing system for now); the frontend must catch the 412 and resubmit with the
  flag.
- ~~**versioncheck manifest is unsigned.**~~ **Addressed (v0.20, Release B).**
  `install_scripts` are stripped from the client-facing API via a DTO
  (regression-locked by a test), so the UI can never render manifest-supplied
  commands; optional ed25519 manifest signature verification activates when
  `WEBCASA_VERSIONCHECK_PUBKEY` is configured (no-op with a warning otherwise).
- ~~**JWT has no revocation.**~~ **Addressed (v0.20, Release B).** `User.TokenVersion`
  + a `tv` claim checked per request; bumped on password/role change;
  `POST /auth/logout-all` invalidates all of a user's sessions.
- ~~**ALTCHA PoW solutions are replayable.**~~ **Addressed (v0.20, Release B).**
  Solved payloads are recorded in a TTL'd in-memory store and rejected on replay
  within the 120s window.
- ~~**`web/node_modules/` is committed to git.**~~ **Addressed (v0.20, Release B).**
  Untracked via `git rm -r --cached web/node_modules`; builds use `npm ci`.

---

## Operator-facing behavioral changes

These are intended consequences of the fixes, not regressions:

1. **Scoped API tokens can no longer perform write operations via REST** — only
   `["*"]` tokens may mutate. Integrations relying on narrow-scope tokens for
   writes will receive HTTP 403. Per-route `category:action` gating can be wired
   later via the exported `RequireTokenScope(scope)` middleware.
2. **New MCP tokens default to no permissions** — scopes must be specified
   explicitly at creation. As of v0.20 the root-equivalent scopes
   (`system:write`, `files:write`, `docker:write`, `cronjob:write`) are **not**
   granted by a wildcard `["*"]` token — they must be listed explicitly, so a
   broad convenience token cannot hand an external MCP client unattended
   privileged automation.
3. **Encryption-at-rest** — existing data still decrypts (automatic fallback to
   the legacy key) and re-encrypts under HKDF on next save. No migration step.
4. **Docker** — Caddy data moved from `/root/.local/share/caddy` to
   `/app/caddy/data` (compose volumes updated). On upgrade, existing
   certificates in the old volume need a one-time migration.
5. **Installer** — now requires the release to publish a matching `.sha256`;
   installation aborts on a missing or mismatched checksum.
6. **JWT sessions are revocable** — changing a user's password or role, or
   calling `POST /auth/logout-all`, invalidates that user's outstanding tokens
   immediately (they get 401). Adds one indexed DB lookup per request on
   JWT-authenticated routes.
7. **App Store installs require acknowledgement** — installing from an unsigned
   source returns HTTP 412 until the request sets `acknowledge_unsigned`; the
   frontend should surface the returned image list as a warning and resubmit.
8. **Manifest signature (optional)** — set `WEBCASA_VERSIONCHECK_PUBKEY` and
   publish a detached ed25519 signature to enforce update-manifest integrity;
   unset leaves the previous behavior with a warning log.

---

## Verification

`go build ./...` passes; `go test ./...` is green (16 packages ok, 0 failures);
all edited Go files are `gofmt`-clean; `bash -n install.sh` passes.
