#!/usr/bin/env python3
"""
Static audit of WebCasa app-store compose files against podman-compose 1.5.x.

Why this exists:
    v0.12 swapped Docker for Podman as the default container runtime. The
    `docker` CLI is preserved via podman-docker, but `docker-compose` is
    replaced by podman-compose, which has known feature gaps. This script
    reads every app's compose YAML out of plugins/appstore/seed_apps.json.gz
    (the same corpus the panel ships with) and flags constructs that
    podman-compose either silently ignores or rejects, so we can decide
    whether to fix the manifest, mark the app unavailable on Podman, or
    document a workaround.

Output:
    - Human-readable summary on stdout
    - Machine-readable JSON report at --report (default: compose-audit.json)

Exit code:
    0 if no Critical findings (warnings allowed)
    1 if any Critical finding present (CI gate)
"""
from __future__ import annotations

import argparse
import gzip
import json
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml  # PyYAML — ships with most dev environments and CI runners

# ── Compatibility rules ────────────────────────────────────────────────────
# Each rule maps a YAML pattern to a severity and explanation.
#
# critical = podman-compose 1.5 will refuse the file or break the app at
#            runtime in a way that needs manifest changes
# warning  = silently ignored or partially supported; the app may still run
#            but the feature is degraded (e.g. GPU reservations)
# info     = works under Podman but worth noting for ops (e.g. needs root,
#            namespace quirks)


@dataclass
class Finding:
    severity: str  # "critical" | "warning" | "info"
    code: str      # short stable identifier for diff-friendly reports
    message: str   # human-readable explanation
    path: str = ""  # YAML path inside the compose, e.g. "services.app.deploy"


@dataclass
class AppReport:
    app_id: str
    findings: list[Finding] = field(default_factory=list)

    def critical(self) -> list[Finding]:
        return [f for f in self.findings if f.severity == "critical"]

    def warnings(self) -> list[Finding]:
        return [f for f in self.findings if f.severity == "warning"]


def audit_service(svc_name: str, svc: dict[str, Any]) -> list[Finding]:
    """Audit one services.<name> block. Returns findings rooted at that path."""
    out: list[Finding] = []
    base = f"services.{svc_name}"

    # GPU reservations: podman-compose 1.5 ignores deploy.resources.reservations
    # entirely. Apps relying on it (Ollama-NVIDIA, Stable Diffusion, etc.) will
    # start but fall back to CPU.
    deploy = svc.get("deploy") or {}
    reservations = (deploy.get("resources") or {}).get("reservations") or {}
    if reservations.get("devices"):
        out.append(Finding(
            severity="warning",
            code="gpu-reservation-ignored",
            message="deploy.resources.reservations.devices is ignored by "
                    "podman-compose; GPU/accelerator passthrough requires "
                    "Podman CDI (`--device nvidia.com/gpu=all`) instead.",
            path=f"{base}.deploy.resources.reservations.devices",
        ))

    # `extends` has only partial support pre-podman-compose 1.6 and is a
    # frequent source of silent failure.
    if "extends" in svc:
        out.append(Finding(
            severity="critical",
            code="extends-unsupported",
            message="`extends` is not reliably supported by podman-compose "
                    "1.5; inline the parent service or split into separate "
                    "stacks.",
            path=f"{base}.extends",
        ))

    # build: with cache_from / target / network is fragile under buildah.
    build = svc.get("build")
    if isinstance(build, dict):
        for risky in ("cache_from", "cache_to", "secrets", "ssh"):
            if risky in build:
                out.append(Finding(
                    severity="warning",
                    code=f"build-{risky}-fragile",
                    message=f"build.{risky} works inconsistently with the "
                            f"buildah backend in podman-compose 1.5.",
                    path=f"{base}.build.{risky}",
                ))

    # privileged / cap_add: SYS_ADMIN: needs rootful Podman (which v0.12 ships
    # by default, so info-level only).
    if svc.get("privileged"):
        out.append(Finding(
            severity="info",
            code="needs-rootful",
            message="`privileged: true` requires the rootful Podman socket; "
                    "v0.12 ships rootful by default so this is supported.",
            path=f"{base}.privileged",
        ))
    cap_add = svc.get("cap_add") or []
    if isinstance(cap_add, list) and any(
        cap in ("SYS_ADMIN", "NET_ADMIN", "ALL") for cap in cap_add
    ):
        out.append(Finding(
            severity="info",
            code="elevated-caps",
            message="Elevated capabilities (SYS_ADMIN/NET_ADMIN/ALL) require "
                    "rootful Podman; supported in v0.12 by default.",
            path=f"{base}.cap_add",
        ))

    # network_mode: host on rootful Podman binds the host network namespace —
    # works, but app may collide with WebCasa's own ports. Worth flagging.
    if svc.get("network_mode") == "host":
        out.append(Finding(
            severity="warning",
            code="network-host",
            message="network_mode: host shares the host namespace; verify the "
                    "app's ports do not collide with WebCasa (39921), Caddy "
                    "(80/443), or other installed services.",
            path=f"{base}.network_mode",
        ))

    # userns_mode: keep-id is a Podman-specific flag; some apps explicitly set
    # it, but the value `host` is also valid. Flag only unsupported strings.
    userns = svc.get("userns_mode")
    if userns and userns not in ("host", "keep-id", "auto"):
        out.append(Finding(
            severity="warning",
            code="userns-uncommon",
            message=f"userns_mode: {userns!r} is not in the well-tested set; "
                    f"verify behaviour under rootful Podman.",
            path=f"{base}.userns_mode",
        ))

    # depends_on with conditions (service_healthy / service_started) is
    # supported but condition: service_completed_successfully has historical
    # gaps. Flag at info level.
    dep = svc.get("depends_on") or {}
    if isinstance(dep, dict):
        for k, v in dep.items():
            if isinstance(v, dict) and v.get("condition") == "service_completed_successfully":
                out.append(Finding(
                    severity="info",
                    code="dep-completed-condition",
                    message="depends_on condition: service_completed_successfully "
                            "may behave subtly differently under podman-compose; "
                            "verify init-container patterns work as expected.",
                    path=f"{base}.depends_on.{k}",
                ))

    # Healthcheck with `start_interval` is a Compose Spec 2024 addition that
    # podman-compose 1.5 does not yet recognise — silently ignored.
    hc = svc.get("healthcheck") or {}
    if "start_interval" in hc:
        out.append(Finding(
            severity="warning",
            code="hc-start-interval-ignored",
            message="healthcheck.start_interval (Compose 2024) is ignored by "
                    "podman-compose 1.5; falls back to interval timing.",
            path=f"{base}.healthcheck.start_interval",
        ))

    # docker.sock bind mounts: works under v0.12 because install.sh symlinks
    # /var/run/docker.sock → /run/podman/podman.sock, but the API exposed is
    # Podman's compat layer, not real Docker. Apps doing low-level Docker
    # inspection (Portainer custom labels, dind-style builds) need verification.
    volumes = svc.get("volumes") or []
    if isinstance(volumes, list):
        for v in volumes:
            spec = v if isinstance(v, str) else (v.get("source") if isinstance(v, dict) else "")
            if spec and ("docker.sock" in spec or "podman.sock" in spec):
                out.append(Finding(
                    severity="info",
                    code="docker-sock-mount",
                    message="Mounts the container runtime socket. v0.12 ships "
                            "/var/run/docker.sock as a symlink to the Podman "
                            "rootful socket; verify Docker-API consumers work "
                            "against Podman's compat endpoint.",
                    path=f"{base}.volumes",
                ))
                break

    # Device passthrough: rootful Podman supports `devices:` but specific
    # nodes (/dev/ttyUSB*, /dev/net/tun, /dev/dri) need to exist on the host
    # and (for serial) udev rules to grant the podman group access.
    devices = svc.get("devices") or []
    if isinstance(devices, list) and devices:
        out.append(Finding(
            severity="info",
            code="device-passthrough",
            message="Maps host device(s) into the container. Verify the device "
                    "node exists on the target host and that the `webcasa`/"
                    "`podman` group has read/write access (udev rules for "
                    "/dev/ttyUSB*, /dev/net/tun, etc.).",
            path=f"{base}.devices",
        ))

    return out


def audit_compose(text: str) -> list[Finding]:
    """Parse a single compose YAML string and emit findings."""
    out: list[Finding] = []

    # Substitute env tokens with placeholders so PyYAML doesn't choke on
    # ${VAR:-default} patterns that look fine to docker-compose but parse OK
    # here too — keeping for safety in case any app uses unquoted shell-isms.
    sanitized = re.sub(r"\$\{[^}]+\}", "PLACEHOLDER", text)

    try:
        doc = yaml.safe_load(sanitized) or {}
    except yaml.YAMLError as e:
        out.append(Finding(
            severity="critical",
            code="yaml-parse-error",
            message=f"YAML parse failed: {e}",
        ))
        return out

    if not isinstance(doc, dict):
        out.append(Finding(
            severity="critical",
            code="not-a-mapping",
            message="Top-level compose document is not a mapping.",
        ))
        return out

    # Top-level `secrets`/`configs` with `external: true` need pre-created
    # objects in podman-secrets and are a common gotcha.
    for top in ("secrets", "configs"):
        section = doc.get(top) or {}
        if isinstance(section, dict):
            for name, cfg in section.items():
                if isinstance(cfg, dict) and cfg.get("external"):
                    out.append(Finding(
                        severity="warning",
                        code=f"external-{top}",
                        message=f"{top}.{name} is declared external; under "
                                f"Podman this object must be pre-created via "
                                f"`podman secret create` / equivalent.",
                        path=f"{top}.{name}",
                    ))

    services = doc.get("services") or {}
    if isinstance(services, dict):
        for name, svc in services.items():
            if isinstance(svc, dict):
                out.extend(audit_service(name, svc))

    return out


def load_apps(seed_path: Path) -> list[dict[str, Any]]:
    with gzip.open(seed_path, "rt", encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, list):
        raise ValueError(f"{seed_path} is not a list of apps")
    return data


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument(
        "--seed",
        type=Path,
        default=Path("plugins/appstore/seed_apps.json.gz"),
        help="Path to seed_apps.json.gz",
    )
    ap.add_argument(
        "--report",
        type=Path,
        default=Path("compose-audit.json"),
        help="Where to write the JSON report",
    )
    ap.add_argument(
        "--strict",
        action="store_true",
        help="Treat warnings as failure too (exit 1 if any warning present)",
    )
    args = ap.parse_args()

    if not args.seed.exists():
        print(f"error: seed file not found: {args.seed}", file=sys.stderr)
        return 2

    apps = load_apps(args.seed)
    reports: list[AppReport] = []
    for app in apps:
        compose = app.get("compose_file") or ""
        rep = AppReport(app_id=app.get("app_id", "<unknown>"))
        if compose:
            rep.findings = audit_compose(compose)
        reports.append(rep)

    # ── Aggregate ─────────────────────────────────────────────────────────
    total = len(reports)
    crit_apps = [r for r in reports if r.critical()]
    warn_apps = [r for r in reports if r.warnings() and not r.critical()]
    info_only = [r for r in reports if r.findings and not r.critical() and not r.warnings()]
    clean = [r for r in reports if not r.findings]

    # Code-level histogram for quick triage
    histogram: dict[str, int] = {}
    for r in reports:
        for f in r.findings:
            histogram[f.code] = histogram.get(f.code, 0) + 1

    # ── Write report ──────────────────────────────────────────────────────
    out_obj = {
        "summary": {
            "total": total,
            "critical": len(crit_apps),
            "warning_only": len(warn_apps),
            "info_only": len(info_only),
            "clean": len(clean),
            "histogram": histogram,
        },
        "apps": [
            {
                "app_id": r.app_id,
                "findings": [
                    {"severity": f.severity, "code": f.code,
                     "path": f.path, "message": f.message}
                    for f in r.findings
                ],
            }
            for r in reports
            if r.findings
        ],
    }
    args.report.write_text(json.dumps(out_obj, indent=2, ensure_ascii=False))

    # ── Stdout summary ────────────────────────────────────────────────────
    print(f"Audited {total} apps from {args.seed}")
    print(f"  critical:     {len(crit_apps):>4}")
    print(f"  warning only: {len(warn_apps):>4}")
    print(f"  info only:    {len(info_only):>4}")
    print(f"  clean:        {len(clean):>4}")
    if histogram:
        print("  findings by code:")
        for code, n in sorted(histogram.items(), key=lambda kv: -kv[1]):
            print(f"    {code:<32} {n:>4}")
    if crit_apps:
        print("\nCRITICAL apps (must fix before v0.12):")
        for r in crit_apps:
            for f in r.critical():
                print(f"  {r.app_id:<24} {f.code:<28} {f.path}")
    print(f"\nReport written to {args.report}")

    if crit_apps:
        return 1
    if args.strict and warn_apps:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
