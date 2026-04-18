package docker

// Integration tests that require a live Podman runtime on the host.
//
// These tests are guarded by two skip conditions:
//   1. `WEBCASA_RUN_PODMAN_TESTS=1` must be set. Without the env var the tests
//      are skipped even on hosts that have Podman installed, so `go test ./...`
//      in a developer loop stays fast and doesn't touch the daemon.
//   2. Individual tests then probe for the resource they need (socket file,
//      binary on PATH) and skip cleanly if absent. CI opts in by setting the
//      env var on the AlmaLinux 9/10 Podman matrix job (see .github/workflows/ci.yml).
//
// Why this file exists: the v0.12 migration swapped Docker for Podman via the
// podman-docker shim. The Go SDK client, the `docker` CLI, and the
// /var/run/docker.sock symlink are all expected to work transparently against
// Podman's compat API. These tests pin that contract so a Podman or SDK
// upgrade that breaks compatibility lights up in CI rather than in production.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	runPodmanTestsEnv = "WEBCASA_RUN_PODMAN_TESTS"
	podmanSocketPath  = "/run/podman/podman.sock"
	dockerSocketPath  = "/var/run/docker.sock"
)

// requirePodmanIntegration skips the test unless the opt-in env var is set.
// Also fails fast if Podman is expected (env set) but obviously unusable,
// turning a silent skip into a clear CI failure.
func requirePodmanIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv(runPodmanTestsEnv) != "1" {
		t.Skipf("set %s=1 to run Podman integration tests", runPodmanTestsEnv)
	}
	if _, err := os.Stat(podmanSocketPath); err != nil {
		t.Fatalf("Podman integration requested but %s missing: %v "+
			"(start podman.socket or unset %s)",
			podmanSocketPath, err, runPodmanTestsEnv)
	}
}

// TestPodmanSocketCompat verifies that the WebCasa Go SDK client can reach
// the Podman rootful socket and that the Docker API version negotiation
// lands on a version Podman serves (v1.41 / v1.43 as of Podman 5.6).
//
// This is the single most important contract check: every plugin route that
// hits plugins/docker/client.go goes through this socket, and a Docker SDK
// upgrade that stops negotiating down to Podman's API window would break
// every container/image/volume/network operation in the panel.
func TestPodmanSocketCompat(t *testing.T) {
	requirePodmanIntegration(t)

	client, err := NewClient(dockerSocketPath)
	if err != nil {
		t.Fatalf("NewClient(%q): %v", dockerSocketPath, err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ping is the cheapest round-trip that proves the socket is reachable
	// AND the API version was negotiated successfully.
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed — socket reachable but API negotiation broken: %v", err)
	}

	// ListContainers exercises a real JSON-shape contract; if Podman's
	// response diverges from what the Docker SDK expects (missing required
	// fields, renamed keys) the decode fails here.
	if _, err := client.ListContainers(ctx, true); err != nil {
		t.Fatalf("ListContainers: %v (Podman compat API v1.41+ should answer this)", err)
	}

	// ListImages likewise — a different Podman handler, and app-store
	// flows all hit both.
	if _, err := client.ListImages(ctx); err != nil {
		t.Fatalf("ListImages: %v", err)
	}
}

// TestDockerShimTransparency verifies that `docker version` and
// `docker compose version` (the two commands WebCasa shells out to when
// checking runtime health in plugin.go) actually reach Podman via the
// podman-docker shim. The output must be parseable by the existing plugin
// checks — specifically, it must include a "Server" section and return
// exit 0.
//
// This catches a class of regressions where either:
//   - podman-docker is uninstalled but `podman` is still present (the plugin
//     currently tolerates this via DetectRuntime, but the shim is still the
//     documented path)
//   - Podman upgrade changes the docker-shim output format enough to break
//     the existing regex in plugin.go checkDockerAlreadyReady
func TestDockerShimTransparency(t *testing.T) {
	requirePodmanIntegration(t)

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker CLI (expected to be podman-docker shim) not on PATH: %v", err)
	}

	// `docker version` must include a Server section. Podman's docker shim
	// answers with its own version line which plugin.go matches.
	out, err := exec.Command("docker", "version").CombinedOutput()
	if err != nil {
		t.Fatalf("`docker version` via shim failed: %v\noutput: %s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "Server:") && !strings.Contains(got, "Server ") {
		t.Errorf("`docker version` output missing Server section — shim may be misconfigured:\n%s", got)
	}

	// `docker compose version` should also work when podman-compose is
	// installed and wired via the shim. WebCasa's plugin init uses this
	// to decide whether compose support is live.
	out, err = exec.Command("docker", "compose", "version").CombinedOutput()
	if err != nil {
		// Not a hard failure: if podman-compose is missing this command
		// will exit non-zero, which is a deployment config issue rather
		// than a SDK regression. Surface it as a skip with context so
		// the CI log points at the right fix.
		t.Skipf("`docker compose version` failed — install podman-compose: %v\noutput: %s",
			err, out)
	}
	got = string(out)
	if !strings.Contains(strings.ToLower(got), "compose") {
		t.Errorf("`docker compose version` output doesn't mention compose:\n%s", got)
	}
}

// TestSocketSymlinkIntegrity verifies the /var/run/docker.sock → Podman socket
// symlink that install.sh creates for app-store compatibility. 8+ popular
// apps (portainer, dockge, dozzle, uptime-kuma, crowdsec, cup, beszel-agent,
// homarr-1) mount this path into containers and expect to be able to talk to
// "Docker" through it. The symlink target must resolve to the actual Podman
// socket for those to work.
func TestSocketSymlinkIntegrity(t *testing.T) {
	requirePodmanIntegration(t)

	fi, err := os.Lstat(dockerSocketPath)
	if err != nil {
		t.Fatalf("Lstat(%q): %v — install.sh should have created this symlink",
			dockerSocketPath, err)
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		// Not a symlink — could be a real socket, which is fine only if
		// it's actually the Podman compat endpoint (e.g. some admins
		// bind-mount instead of symlink). We don't try to tell those
		// apart here; just verify it's a usable socket.
		if fi.Mode()&os.ModeSocket == 0 {
			t.Fatalf("%s is neither a symlink nor a socket (mode=%v)",
				dockerSocketPath, fi.Mode())
		}
		t.Logf("note: %s is a real socket, not a symlink — skipping target resolution check",
			dockerSocketPath)
		return
	}

	target, err := os.Readlink(dockerSocketPath)
	if err != nil {
		t.Fatalf("Readlink(%q): %v", dockerSocketPath, err)
	}
	// Resolve relative targets against the symlink's directory.
	resolved := target
	if !filepath.IsAbs(target) {
		resolved = filepath.Join(filepath.Dir(dockerSocketPath), target)
	}

	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		t.Fatalf("resolve symlink target: %v", err)
	}

	// Expect the target to be the Podman rootful socket. Be permissive
	// about /run/podman vs /var/run/podman (EL9 uses /run, /var/run is a
	// bind-mount fallback).
	expectedSuffix := "podman/podman.sock"
	if !strings.HasSuffix(absResolved, expectedSuffix) {
		t.Errorf("%s symlink points at %q, expected path ending in %q",
			dockerSocketPath, absResolved, expectedSuffix)
	}

	// And the resolved target must exist as a real socket.
	targetInfo, err := os.Stat(absResolved)
	if err != nil {
		t.Fatalf("Stat(%q) — symlink target missing: %v", absResolved, err)
	}
	if targetInfo.Mode()&os.ModeSocket == 0 {
		t.Errorf("%s exists but is not a socket (mode=%v)", absResolved, targetInfo.Mode())
	}
}
