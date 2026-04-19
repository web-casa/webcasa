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


def sanitize_compose(text: str) -> str:
    """Port of plugins/appstore/renderer.go:SanitizeCompose.

    The seed compose files came from the Tipi catalogue and reference an
    `external: tipi_main_network` plus `traefik.*` / `runtipi.*` labels that
    only make sense in a Tipi runtime. WebCasa's renderer strips them at
    install time; we mirror the same logic here so the harness exercises
    what an actual WebCasa install would produce, not the raw catalogue
    data. Keep this in sync with the Go implementation.
    """
    out: list[str] = []
    skip_top_network = False
    # Track entry/exit from a `volumes:` block so the bind-mount relabel
    # doesn't apply to port mappings (same `host:container` shape).
    volumes_indent = -1

    for line in text.splitlines():
        trimmed = line.strip()
        indent = len(line) - len(line.lstrip(" \t"))

        if trimmed == "volumes:":
            volumes_indent = indent
        elif volumes_indent >= 0 and trimmed and indent <= volumes_indent:
            volumes_indent = -1
        in_volumes = volumes_indent >= 0 and indent > volumes_indent

        # Strip traefik.* / runtipi.* labels in both mapping and list forms.
        if trimmed.startswith(("traefik.", "runtipi.")):
            continue
        stripped = trimmed.removeprefix("- ").strip("\"'")
        if stripped.startswith(("traefik.", "runtipi.")):
            continue

        # Service-level network reference.
        if trimmed in ("- tipi_main_network", "- tipi-main-network"):
            continue

        # Top-level networks block — once we see "tipi_main_network:" at
        # the network-key indent, swallow all of its child indented lines.
        if skip_top_network:
            if line.startswith(("  ", "\t")):
                continue
            skip_top_network = False
        if trimmed in ("tipi_main_network:", "tipi-main-network:"):
            skip_top_network = True
            continue

        # Mirror renderer.go relabelHostBindMount: append :Z to host-path
        # bind mounts so container_t can write to them on EL9/EL10
        # enforcing. Skips named volumes, the docker/podman socket, ports
        # (in_volumes guard), and specs that already carry an option.
        out.append(_relabel_host_bind(line, in_volumes))

    result = "\n".join(out)
    # Drop empty `networks:` / `labels:` sections that the strips above leave
    # behind. podman-compose 1.5 fails parse on bare `networks:` with no
    # children, so we have to do this — matches Go cleanEmptySection.
    result = _clean_empty_section(result, "networks:")
    result = _clean_empty_section(result, "labels:")
    # Inject security_opt: [label=disable] for services that mount
    # docker.sock / podman.sock — mirrors renderer.go injectSocketLabelDisable.
    # Without this, EL9/EL10 enforcing silently denies container_t access
    # to the var_run_t socket via dontaudit (no AVC logged) and the
    # container fails with a "Could not connect to Docker Engine" error.
    result = _inject_socket_label_disable(result)
    return result


def _inject_socket_label_disable(content: str) -> str:
    """Mirror renderer.go injectSocketLabelDisable with indent-agnostic
    service detection and smart merge into an existing security_opt: block.
    """
    lines = content.split("\n")
    services_indent = -1
    services: list[dict] = []
    cur: dict | None = None
    in_volumes = False
    vol_indent = -1
    in_sec_opt = False

    def is_service_header(trimmed: str, indent: int) -> bool:
        if services_indent < 0 or indent <= services_indent:
            return False
        code = trimmed.split("#", 1)[0].strip()
        if not code.endswith(":"):
            return False
        key = code[:-1]
        if not key or any(c.isspace() for c in key):
            return False
        if cur is not None and indent != cur["indent"]:
            return False
        return True

    for i, line in enumerate(lines):
        trimmed = line.strip()
        indent = len(line) - len(line.lstrip(" \t"))

        if services_indent < 0 and trimmed == "services:":
            services_indent = indent
            continue
        if services_indent < 0:
            continue

        if is_service_header(trimmed, indent):
            if cur is not None:
                cur["end"] = i - 1
                services.append(cur)
            cur = {
                "start": i, "indent": indent,
                "mounts_sock": False,
                "sec_opt_index": -1, "sec_opt_indent": -1,
                "sec_opt_list_item_indent": -1,
                "has_label_disable": False,
            }
            in_volumes = False
            vol_indent = -1
            in_sec_opt = False
            continue

        if cur is not None and trimmed and not trimmed.startswith("#") and indent <= services_indent:
            cur["end"] = i - 1
            services.append(cur)
            cur = None
            services_indent = -1
            continue

        if cur is None:
            continue

        if trimmed == "volumes:":
            in_volumes = True
            vol_indent = indent
            in_sec_opt = False
            continue
        if in_volumes and trimmed and indent <= vol_indent:
            in_volumes = False
        if in_volumes:
            if "docker.sock" in trimmed or "podman.sock" in trimmed:
                cur["mounts_sock"] = True

        if trimmed == "security_opt:":
            cur["sec_opt_index"] = i
            cur["sec_opt_indent"] = indent
            in_sec_opt = True
            in_volumes = False
            continue
        if in_sec_opt:
            if trimmed and indent <= cur["sec_opt_indent"]:
                in_sec_opt = False
            elif trimmed.startswith("- "):
                if cur["sec_opt_list_item_indent"] < 0:
                    cur["sec_opt_list_item_indent"] = indent
                item = trimmed.removeprefix("- ").strip().strip("\"'")
                if item == "label=disable":
                    cur["has_label_disable"] = True

    if cur is not None:
        cur["end"] = len(lines) - 1
        services.append(cur)

    inserts: list[tuple[int, str]] = []
    for s in services:
        if not s["mounts_sock"] or s["has_label_disable"]:
            continue
        if s["sec_opt_index"] >= 0:
            list_indent = s["sec_opt_list_item_indent"]
            if list_indent < 0:
                list_indent = s["sec_opt_indent"] + 2
            inserts.append((s["sec_opt_index"] + 1, " " * list_indent + "- label=disable"))
        else:
            body_indent = " " * (s["indent"] + 2)
            list_indent_str = " " * (s["indent"] + 4)
            inserts.append((s["start"] + 1,
                            f"{body_indent}security_opt:\n{list_indent_str}- label=disable"))

    if not inserts:
        return content
    for at, text in reversed(inserts):
        lines.insert(at, text)
    return "\n".join(lines)


def _relabel_host_bind(line: str, in_volumes: bool) -> str:
    if not in_volumes:
        return line
    trimmed = line.strip()
    if not trimmed.startswith("- "):
        return line
    stripped = trimmed.removeprefix("- ")

    # Preserve quote state — see relabelHostBindMount in renderer.go for
    # rationale (Codex Critical: naive strip + append produced
    # `- "host:container":Z` which is invalid YAML).
    quote = ""
    if len(stripped) >= 2 and stripped[0] == stripped[-1] and stripped[0] in ("'", '"'):
        quote = stripped[0]
        stripped = stripped[1:-1]

    parts = stripped.split(":")
    if len(parts) < 2:
        return line
    host = parts[0]
    if not (host.startswith("/") or host.startswith("${")):
        return line
    if "docker.sock" in host or "podman.sock" in host:
        return line
    for prefix in ("/dev", "/sys", "/proc", "/run/udev"):
        if host == prefix or host.startswith(prefix + "/"):
            return line
    if parts[-1] in ("Z", "z", "ro", "rw"):
        return line

    prefix_str = line[: len(line) - len(trimmed)] + "- "
    if quote:
        return f"{prefix_str}{quote}{stripped}:Z{quote}"
    return line + ":Z"


def _clean_empty_section(content: str, section_key: str) -> str:
    """Remove a YAML section whose only child lines are blank/comments."""
    lines = content.split("\n")
    out: list[str] = []
    i = 0
    while i < len(lines):
        trimmed = lines[i].strip()
        if trimmed == section_key:
            indent = len(lines[i]) - len(lines[i].lstrip(" \t"))
            j = i + 1
            has_content = False
            while j < len(lines):
                child_trimmed = lines[j].strip()
                if child_trimmed == "" or child_trimmed.startswith("#"):
                    j += 1
                    continue
                child_indent = len(lines[j]) - len(lines[j].lstrip(" \t"))
                if child_indent > indent:
                    has_content = True
                break
            if not has_content:
                # Skip the section header. Children (if any blank/comment) get
                # carried through naturally because we just continue.
                i += 1
                continue
        out.append(lines[i])
        i += 1
    return "\n".join(out)


def default_env_for(app: dict, data_dir: Path, host_port: int) -> dict[str, str]:
    """Build the env-var map a fresh WebCasa install would inject.

    Mirrors the builtins set in plugins/appstore/renderer.go:RenderCompose:
    APP_PORT, APP_DATA_DIR, APP_DOMAIN, APP_PROTOCOL, plus a few extras
    that some Tipi seeds reference (LOCAL_DOMAIN). For form_fields we use
    each field's `default` so the harness doesn't need an interactive UI.
    """
    env = {
        "APP_PORT": str(host_port),
        "APP_ID": app.get("app_id", "test"),
        "APP_DATA_DIR": str(data_dir),
        "APP_DOMAIN": "localhost",
        "APP_PROTOCOL": "http",
        "LOCAL_DOMAIN": "local",
        "ROOT_FOLDER_HOST": str(data_dir),  # some seeds use this
        "STORAGE_PATH": str(data_dir),      # some seeds use this
    }
    # form_fields defaults — a real install would prompt the user; we use
    # whatever the catalogue authors marked as default so the smoke test
    # has a chance of bringing the app up. The seed stores form_fields
    # either as a list of dicts (newer entries) OR as a JSON-encoded
    # string (older Tipi imports). Decode the string form lazily so we
    # don't choke either way.
    fields = app.get("form_fields") or []
    if isinstance(fields, str):
        try:
            fields = json.loads(fields)
        except json.JSONDecodeError:
            fields = []
    if not isinstance(fields, list):
        fields = []
    for field in fields:
        if not isinstance(field, dict):
            continue
        key = field.get("env_variable")
        if not key or key in env:
            continue
        if "default" in field and field["default"] is not None:
            env[key] = str(field["default"])
        elif field.get("type") == "random":
            # crude — real renderer uses crypto random; smoke test can use
            # a fixed marker so failure logs are deterministic.
            env[key] = "smoketest_random_placeholder"
    return env


def run_one(app_id: str, app: dict, timeout_s: int, keep: bool, port: int) -> Result:
    """Bring an app up, verify at least one container entered Running,
    then bring it down. Returns a Result dataclass regardless of outcome."""
    start = time.monotonic()
    tool = compose_tool()

    compose_body = sanitize_compose(app.get("compose_file", ""))
    if not compose_body.strip():
        return Result(
            app_id=app_id, status="skipped",
            duration_s=0.0, error="empty compose after sanitize",
        )

    with tempfile.TemporaryDirectory(prefix=f"webcasa-batch-{app_id}-") as tmp:
        tmp_path = Path(tmp)
        compose_path = tmp_path / "docker-compose.yml"
        compose_path.write_text(compose_body, encoding="utf-8")

        # Per-app data dir for volume mounts. Real WebCasa install puts these
        # under /var/lib/webcasa/stacks/<app>/data; mirror the layout so
        # the relative paths in compose volumes resolve sanely.
        data_dir = tmp_path / "data"
        data_dir.mkdir(exist_ok=True)
        # SELinux relabel so the container's container_t domain can write
        # to this host path — same gotcha covered in docs/selinux.md
        # Scenario 1. Best-effort: fails silently on hosts without SELinux
        # tooling (e.g. CI inside a non-EL container). When SELinux is
        # actually enforcing this is what makes the bind mounts work.
        subprocess.run(
            ["chcon", "-R", "-t", "container_file_t", str(data_dir)],
            capture_output=True, timeout=10, check=False,
        )

        env_vars = default_env_for(app, data_dir, port)
        env_path = tmp_path / ".env"
        env_path.write_text(
            "\n".join(f"{k}={v}" for k, v in env_vars.items()) + "\n",
            encoding="utf-8",
        )

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

            # Wait for at least one container to reach Running state.
            # Single-shot 3s sleep + ps was racy: heavyweight images
            # (transmission-vpn, kasm-workspaces) sometimes pass through
            # a brief Created → starting transition that the snapshot
            # missed, producing a false FAIL on a container that was
            # actually fine. Poll until the per-app deadline — earlier
            # versions hardcoded 10s, which would record FAIL for slow
            # but eventually-healthy apps. Respect the budget the user
            # actually asked for, floor at 8s so short --timeout values
            # still get a meaningful readiness window.
            deadline = start + max(timeout_s, 8)
            running = False
            ps_stdout = ""
            ps_stderr = ""
            while time.monotonic() < deadline:
                time.sleep(1)
                ps = subprocess.run(
                    base + ["ps", "--format", "json"],
                    capture_output=True, text=True, timeout=30,
                )
                ps_stdout = ps.stdout
                ps_stderr = ps.stderr
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
                        running = "Up" in ps.stdout or "running" in ps.stdout.lower()
                if running:
                    break

            cleanup_err = cleanup()
            if not running:
                # Use the LAST ps output we saw (most likely state).
                last_out = ps_stdout[-800:] or ps_stderr[-800:]
                return Result(
                    app_id=app_id, status="fail",
                    duration_s=time.monotonic() - start,
                    error="no container reached running state",
                    stderr_tail=last_out,
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
        # Per-app port to avoid host-port collisions if podman-compose's
        # cleanup is incomplete between apps. Range starts above ephemeral.
        port = 19000 + i
        res = run_one(app_id, app, args.timeout, args.keep, port)
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
