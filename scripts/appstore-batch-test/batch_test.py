#!/usr/bin/env python3
"""
Phase 4 TestAppStoreBatchUpDown harness.

Spins up each high-risk app-store entry via podman-compose, waits for it to
reach a running state, tears it down, and records pass/fail. Intentionally
standalone (not a Go test) because the run takes 5–30 minutes depending on
network + image sizes and needs a live Podman host.

Inputs:
    --seed      Path to seed_apps.json.gz (default: plugins/appstore/seed_apps.json.gz)
    --apps      Comma-separated app_id list (default: the 27 high-risk set
                from docs/07-podman-v0.12.md)
    --timeout   Per-app timeout in seconds (default: 300)
    --keep      Keep containers around after success (for manual poking)
    --report    JSON report path (default: docs/podman-app-test-report.json)

Usage:
    WEBCASA_RUN_PODMAN_TESTS=1 python3 scripts/appstore-batch-test/batch_test.py

Output:
    Per-app PASS / FAIL / TIMEOUT lines on stdout, plus a JSON report
    that mirrors the Status column of the validation matrix in
    docs/07-podman-v0.12.md. CI (and maintainers doing release VPS
    runs) can diff the report against previous runs to spot regressions.
"""
from __future__ import annotations

import argparse
import gzip
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from dataclasses import asdict, dataclass
from pathlib import Path

# Default 27-app high-risk set — synced with docs/07-podman-v0.12.md.
# Keep this list hand-curated: the compose-audit.py output is noisier than
# what we actually want to live-test.
DEFAULT_APPS = [
    # docker.sock mount (8)
    "portainer", "dockge", "dozzle", "uptime-kuma", "crowdsec", "cup",
    "beszel-agent", "homarr-1",
    # rootful/privileged (8)
    "dashdot", "gladys", "homebridge", "kasm-workspaces", "scrypted",
    "sshwifty", "stirling-pdf", "unmanic",
    # network_mode: host (4 new vs above)
    "cloudflared", "matter-server", "mdns-repeater", "plex",
    # cap_add (4)
    "netdata", "transmission-vpn", "windows", "zerotier",
    # devices (1 new)
    "zigbee2mqtt",
    # Phase 4 Codex additions
    "ollama-nvidia", "n8n-1",
]


@dataclass
class Result:
    app_id: str
    status: str        # "pass" | "fail" | "timeout" | "skipped"
    duration_s: float
    error: str = ""
    stderr_tail: str = ""


def require_env():
    """Fail fast if the operator didn't opt in or Podman isn't present."""
    if os.environ.get("WEBCASA_RUN_PODMAN_TESTS") != "1":
        print(
            "refusing to run — set WEBCASA_RUN_PODMAN_TESTS=1 to confirm "
            "this is a test host (not production)", file=sys.stderr,
        )
        sys.exit(2)
    if shutil.which("podman-compose") is None and shutil.which("docker") is None:
        print("no podman-compose or docker CLI on PATH — install first",
              file=sys.stderr)
        sys.exit(2)


def load_apps(seed: Path) -> dict:
    with gzip.open(seed, "rt", encoding="utf-8") as f:
        data = json.load(f)
    return {a["app_id"]: a for a in data if "app_id" in a}


def compose_tool() -> list[str]:
    """Return the compose CLI the harness will shell out to. Prefers
    podman-compose when present, falls back to the docker CLI (which under
    v0.12 is also podman-docker → the same underlying engine)."""
    if shutil.which("podman-compose"):
        return ["podman-compose"]
    return ["docker", "compose"]


def run_one(app_id: str, compose_body: str, timeout_s: int, keep: bool) -> Result:
    """Bring an app up, verify at least one container entered Running,
    then bring it down. Returns a Result dataclass regardless of outcome."""
    start = time.monotonic()
    tool = compose_tool()

    with tempfile.TemporaryDirectory(prefix=f"webcasa-batch-{app_id}-") as tmp:
        compose_path = Path(tmp) / "docker-compose.yml"
        compose_path.write_text(compose_body, encoding="utf-8")

        # podman-compose writes state under --project-directory, so pin it
        # to the tmp dir for easy cleanup on failure.
        base = tool + ["-f", str(compose_path), "-p", f"webcasa-batch-{app_id}"]

        def cleanup() -> str:
            """Tear down the stack. Returns an error string on failure so the
            caller can attach it to the Result — silently ignoring cleanup
            failures lets orphans leak into subsequent apps and makes one
            broken teardown cascade into downstream false FAILs."""
            if keep:
                return ""
            try:
                down = subprocess.run(
                    base + ["down", "--remove-orphans"],
                    capture_output=True, text=True, timeout=120,
                )
            except subprocess.TimeoutExpired:
                return "down: timed out after 120s (containers may have leaked)"
            if down.returncode != 0:
                return f"down exit {down.returncode}: {down.stderr[-400:]}"
            return ""

        try:
            up = subprocess.run(
                base + ["up", "-d"],
                capture_output=True, text=True, timeout=timeout_s,
            )
            if up.returncode != 0:
                cleanup_err = cleanup()
                err = f"compose up exit {up.returncode}"
                if cleanup_err:
                    err += f"; cleanup also failed ({cleanup_err})"
                return Result(
                    app_id=app_id, status="fail",
                    duration_s=time.monotonic() - start,
                    error=err,
                    stderr_tail=up.stderr[-800:],
                )

            # Give the containers a moment to settle — podman-compose returns
            # as soon as `podman run` acks, but actual readiness is later.
            time.sleep(3)

            # Verify at least one container is Up. We don't try to parse
            # healthchecks: the audit already classified GPU/network/etc
            # quirks, so here we just need "container started and stayed
            # running for ~3s".
            ps = subprocess.run(
                base + ["ps", "--format", "json"],
                capture_output=True, text=True, timeout=30,
            )
            # podman-compose json output can be line-delimited or array.
            # Parse leniently.
            running = False
            out = ps.stdout.strip()
            if out:
                try:
                    arr = json.loads(out) if out.startswith("[") else [
                        json.loads(line) for line in out.splitlines() if line
                    ]
                    for entry in arr:
                        state = (entry.get("State") or entry.get("state") or "").lower()
                        if state in ("running", "up"):
                            running = True
                            break
                except json.JSONDecodeError:
                    # Fall back to string sniff for older podman-compose
                    running = "Up" in ps.stdout or "running" in ps.stdout.lower()

            cleanup_err = cleanup()
            if not running:
                return Result(
                    app_id=app_id, status="fail",
                    duration_s=time.monotonic() - start,
                    error="no container reached running state",
                    stderr_tail=ps.stdout[-800:] or ps.stderr[-800:],
                )
            if cleanup_err:
                # up + ps succeeded but teardown didn't. Downgrading to fail
                # keeps the JSON report honest (a PASS would wrongly imply
                # the app leaves no state behind); the stderr_tail points at
                # which down command needed manual cleanup.
                return Result(
                    app_id=app_id, status="fail",
                    duration_s=time.monotonic() - start,
                    error=f"teardown failed: {cleanup_err}",
                )

            return Result(
                app_id=app_id, status="pass",
                duration_s=time.monotonic() - start,
            )

        except subprocess.TimeoutExpired as e:
            cleanup_err = cleanup()
            err = f"timed out after {timeout_s}s: {e.cmd}"
            if cleanup_err:
                err += f"; cleanup also failed ({cleanup_err})"
            return Result(
                app_id=app_id, status="timeout",
                duration_s=time.monotonic() - start,
                error=err,
            )
        except Exception as e:  # noqa: BLE001 — we want catch-all for the CI path
            cleanup_err = cleanup()
            err = f"{type(e).__name__}: {e}"
            if cleanup_err:
                err += f"; cleanup also failed ({cleanup_err})"
            return Result(
                app_id=app_id, status="fail",
                duration_s=time.monotonic() - start,
                error=err,
            )


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--seed", type=Path,
                    default=Path("plugins/appstore/seed_apps.json.gz"))
    ap.add_argument("--apps", type=str, default=",".join(DEFAULT_APPS),
                    help="comma-separated app_id list")
    ap.add_argument("--timeout", type=int, default=300,
                    help="per-app up/down timeout in seconds")
    ap.add_argument("--keep", action="store_true",
                    help="don't tear down after success (for manual inspection)")
    ap.add_argument("--report", type=Path,
                    default=Path("docs/podman-app-test-report.json"))
    args = ap.parse_args()

    require_env()

    if not args.seed.exists():
        print(f"seed not found: {args.seed}", file=sys.stderr)
        return 2

    catalog = load_apps(args.seed)
    targets = [a.strip() for a in args.apps.split(",") if a.strip()]
    results: list[Result] = []

    # Ctrl+C sets a flag so the outer loop stops scheduling new apps and
    # writes the partial report. We do NOT interrupt the current in-flight
    # subprocess — that would risk leaking containers mid-up. Press Ctrl+C a
    # second time (KeyboardInterrupt) if you need to force-abort; the default
    # Python handler will raise through subprocess.run and the finally/except
    # paths will still call cleanup().
    interrupted = False

    def sigint_handler(signum, frame):
        nonlocal interrupted
        if interrupted:
            # Second Ctrl+C — restore default handler so the next one raises
            # KeyboardInterrupt through the running subprocess.
            signal.signal(signal.SIGINT, signal.SIG_DFL)
            print("\n[sigint x2] raising KeyboardInterrupt — may leak "
                  "containers if down hangs", file=sys.stderr)
            return
        interrupted = True
        print("\n[sigint] finishing current app then stopping… "
              "(press again to force-abort)", file=sys.stderr)

    signal.signal(signal.SIGINT, sigint_handler)

    print(f"Running {len(targets)} apps against {tool()} "
          f"(timeout {args.timeout}s each)")
    print("-" * 72)

    for i, app_id in enumerate(targets, 1):
        if interrupted:
            results.append(Result(app_id=app_id, status="skipped",
                                  duration_s=0.0, error="sigint"))
            continue
        app = catalog.get(app_id)
        if not app:
            print(f"[{i:02d}/{len(targets)}] {app_id:<22} MISSING in seed")
            results.append(Result(app_id=app_id, status="skipped",
                                  duration_s=0.0, error="not in seed"))
            continue
        compose = app.get("compose_file", "")
        if not compose:
            print(f"[{i:02d}/{len(targets)}] {app_id:<22} NO COMPOSE")
            results.append(Result(app_id=app_id, status="skipped",
                                  duration_s=0.0, error="empty compose"))
            continue
        res = run_one(app_id, compose, args.timeout, args.keep)
        results.append(res)
        badge = {"pass": "PASS", "fail": "FAIL",
                 "timeout": "TIME", "skipped": "SKIP"}[res.status]
        line = f"[{i:02d}/{len(targets)}] {app_id:<22} {badge}  {res.duration_s:6.1f}s"
        if res.error:
            line += f"  — {res.error}"
        print(line)

    # Write report.
    summary = {
        "total": len(results),
        "passed": sum(r.status == "pass" for r in results),
        "failed": sum(r.status == "fail" for r in results),
        "timedout": sum(r.status == "timeout" for r in results),
        "skipped": sum(r.status == "skipped" for r in results),
    }
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps({
        "summary": summary,
        "results": [asdict(r) for r in results],
    }, indent=2, ensure_ascii=False))

    print("-" * 72)
    for k, v in summary.items():
        print(f"  {k:<10} {v:>3}")
    print(f"\nReport: {args.report}")

    # Exit 1 if any app failed — CI gate.
    return 1 if summary["failed"] or summary["timedout"] else 0


def tool() -> str:
    return " ".join(compose_tool())


if __name__ == "__main__":
    sys.exit(main())
