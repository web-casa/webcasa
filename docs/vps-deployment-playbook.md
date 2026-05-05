# VPS Deployment Playbook (DigitalOcean)

Reusable playbook for spinning up a fresh test VPS on DigitalOcean
(or similar provider) and installing Web.Casa from `main` for E2E
verification. **Per-second billing — destroy as soon as testing is
done.**

## Prereqs

- DO API token in `docs/.env` as `doapi=dop_v1_...`
- Local SSH key at `~/.ssh/id_ed25519.pub` (or override `PUBKEY_PATH`)

## One-shot create + install

```bash
DO_TOKEN=$(grep ^doapi= docs/.env | cut -d= -f2)
SSH_PUBKEY=$(cat ~/.ssh/id_ed25519.pub)

# 1. Upload SSH key (one-time per account; check existing first via GET)
SSH_KEY_ID=$(curl -sS -X POST \
  -H "Authorization: Bearer $DO_TOKEN" \
  -H "Content-Type: application/json" \
  https://api.digitalocean.com/v2/account/keys \
  -d "{\"name\":\"webcasa-test-$(date +%s)\",\"public_key\":\"$SSH_PUBKEY\"}" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['ssh_key']['id'])")

# 2. Create droplet — sgp1, 2vcpu/2GB, AlmaLinux 9
DROPLET_ID=$(curl -sS -X POST \
  -H "Authorization: Bearer $DO_TOKEN" \
  -H "Content-Type: application/json" \
  https://api.digitalocean.com/v2/droplets \
  -d "{\"name\":\"webcasa-e2e\",\"region\":\"sgp1\",\"size\":\"s-2vcpu-2gb\",\"image\":\"almalinux-9-x64\",\"ssh_keys\":[$SSH_KEY_ID],\"tags\":[\"webcasa-test\"]}" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['droplet']['id'])")

# 3. Wait for active + capture IP
for i in $(seq 1 30); do
  STATUS=$(curl -sS -H "Authorization: Bearer $DO_TOKEN" \
    "https://api.digitalocean.com/v2/droplets/$DROPLET_ID" \
    | python3 -c "import json,sys; d=json.load(sys.stdin)['droplet']; ips=[n['ip_address'] for n in d['networks']['v4'] if n['type']=='public']; print(d['status'], ips[0] if ips else '')")
  echo "$i $STATUS"
  case "$STATUS" in active*) DROPLET_IP=$(echo "$STATUS" | awk '{print $2}'); break;; esac
  sleep 5
done

# 4. Wait for SSH (10-30s after active)
for i in $(seq 1 20); do
  ssh -o StrictHostKeyChecking=no -o BatchMode=yes -o ConnectTimeout=5 \
      root@$DROPLET_IP true 2>/dev/null && break
  sleep 5
done

# 5. Install Web.Casa
ssh -o StrictHostKeyChecking=no root@$DROPLET_IP \
  'curl -fsSL https://raw.githubusercontent.com/web-casa/webcasa/main/install.sh | bash -s -- -y --pro'

# 6. Verify panel is up
ssh root@$DROPLET_IP 'systemctl is-active webcasa caddy podman; \
  ss -tlnp | grep -E "39921|2019|443|80"'
```

## Configuration choices (and why)

| Choice | Value | Why |
|--------|-------|-----|
| Region | `sgp1` (Singapore) | Closest to operator (HK/CN) — lower SSH latency during testing |
| Size | `s-2vcpu-2gb` | Project floor is 2GB; smallest size that meets the spec |
| Image | `almalinux-9-x64` | Project supports EL9/EL10. Avoid AlmaLinux 10 + Docker (kernel iptables `addrtype` module bug). Use Podman instead per v0.12 default. |
| Backups/IPv6/Monitoring | off | Test droplet — kept lean to minimize charge per second |
| Tag | `webcasa-test` | Bulk teardown via `--tag-name=webcasa-test` |

## Smoke checklist

After install:

1. `systemctl is-active webcasa` → `active`
2. `systemctl is-active caddy` → `active`
3. `podman version` → `5.6.x`
4. `curl -sS http://127.0.0.1:39921/api/system/info` → JSON
5. Visit `http://$DROPLET_IP:39921` in a browser → login page renders
6. `journalctl -u webcasa -n 50 --no-pager` → no panic / no fatal

## Teardown (CRITICAL — per-second billing)

```bash
# Single droplet
curl -sS -X DELETE \
  -H "Authorization: Bearer $DO_TOKEN" \
  "https://api.digitalocean.com/v2/droplets/$DROPLET_ID"

# Or all test droplets at once
curl -sS -X DELETE \
  -H "Authorization: Bearer $DO_TOKEN" \
  "https://api.digitalocean.com/v2/droplets?tag_name=webcasa-test"

# Verify gone
curl -sS -H "Authorization: Bearer $DO_TOKEN" \
  "https://api.digitalocean.com/v2/droplets?tag_name=webcasa-test" \
  | python3 -c "import json,sys; print('remaining:', len(json.load(sys.stdin)['droplets']))"
```

## Known pitfalls

- **AlmaLinux 10 + Docker**: kernel 6.12 lacks iptables `addrtype` —
  Docker 29 fails to start. Use AlmaLinux 9 OR EL10 + Podman.
- **First SSH connect**: 10-30s gap between droplet `active` and
  SSH listening. Always loop with `BatchMode=yes -o ConnectTimeout=5`.
- **DO droplet limit**: account default is 3 concurrent droplets.
  `GET /v2/account` reports `droplet_limit`.
- **SSH key already exists**: `POST /v2/account/keys` returns 422 if
  the public key is already on the account; check via `GET` first
  in production scripts.
- **`bash install.sh ... | tail -N` swallows the entire log**: tail
  buffers everything until stdin closes, so if the install hangs
  partway you see zero output. Always pipe to `tee` (or just don't
  pipe) so you can watch progress in real time.
- **`ausearch -m AVC -ts recent`**: blocks indefinitely on a freshly-
  installed AlmaLinux 9 droplet (no AVC entries yet). Wrap with
  `timeout 3` if you need to query AVCs during a smoke test.

## v0.19.0 E2E findings (2026-05-06)

Tested on droplet `569184194` (sgp1, s-2vcpu-2gb, AlmaLinux 9.6,
kernel 5.14.0-570).

**What worked:**
- `webcasa-server v0.19.0` boots cleanly; all 9 plugins load
- Caddy 2.11.2 + Podman 5.6.0 install via install.sh
- Panel HTTP at `:39921` serves React shell + responds on
  `/api/auth/login`, `/api/auth/me`
- New v0.19 endpoints `/api/plugins/deploy/previews/{id}/approve` and
  `/revoke` respond `401` (auth-gated) — wiring confirmed
- systemd unit, `cap_net_bind_service` setcap, podman socket all
  work as expected

**Bug 1: install.sh hangs in `setup_systemd` (heisenbug)**

Reproducer: `bash install.sh -y --pro 2>&1 | tail -150` over SSH.
After install_caddy + install_podman + install_prebuilt complete,
the script reaches `setup_systemd` and stalls. Process state:

- Parent install.sh (`do_wait`) waiting on subshell
- Subshell (`pipe_read`, fd1 → `webcasa.service`) blocked
  reading from a pipe whose write end is held by an unrelated
  `caddy run --pingback` process (PID parent → init)
- `webcasa.service` file truncated to 0 bytes; never written

Standalone reproduction of the same heredoc completes in <1s, so the
hang is environmental. Hypothesis: install path is leaking an fd to
a backgrounded `caddy start`/`caddy run` invocation that the systemd
heredoc subshell inherits as its read end. Worth tracking down before
v0.20 install changes.

Workaround: kill the install, manually write `/etc/systemd/system/
webcasa.service`, run `systemctl daemon-reload && systemctl enable
--now webcasa`. All other install steps were idempotent and complete.

**Bug 2: deploy plugin warns `jwt_secret not set`**

Pre-existing (not v0.19-specific). install.sh writes
`WEBCASA_JWT_SECRET=...` to `/etc/webcasa/webcasa.env`, but the deploy
plugin's encryption-key lookup falls back to a random key with a
warn-level log on first boot:

```
WARN jwt_secret not set, generated a random encryption key for deploy
plugin module=plugin plugin=deploy
```

Effect: deploy-plugin secrets (env vars marked `secret=true`,
deploy keys, GitHub tokens) re-encrypt on every restart because the
key is regenerated. File for v0.20 fix — read JWT secret from env
explicitly, or persist the generated key to `settings` table on
first boot and reuse.
