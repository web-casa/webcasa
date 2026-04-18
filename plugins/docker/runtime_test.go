package docker

import (
	"testing"
)

// resetRuntimeForTesting is a thin wrapper around ResetRuntimeCache so tests
// keep the previous local-helper name. The production reset is safe to call
// concurrently; tests still reset before launching goroutines for clarity.
func resetRuntimeForTesting() {
	ResetRuntimeCache()
}

func TestRuntime_String(t *testing.T) {
	cases := map[Runtime]string{
		RuntimeUnknown: "unknown",
		RuntimeDocker:  "docker",
		RuntimePodman:  "podman",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", r, got, want)
		}
	}
}

// TestRuntime_SystemdUnit pins the mapping between a detected runtime and
// the systemd unit we restart for daemon.json-equivalent changes. The CI
// install-test relies on `systemctl restart podman.socket` being the target
// under Podman; a regression here would route daemon restarts at the wrong
// unit name and silently no-op.
func TestRuntime_SystemdUnit(t *testing.T) {
	cases := map[Runtime]string{
		RuntimeUnknown: "",
		RuntimeDocker:  "docker",
		RuntimePodman:  "podman.socket",
	}
	for r, want := range cases {
		if got := r.SystemdUnit(); got != want {
			t.Errorf("%d.SystemdUnit() = %q, want %q", r, got, want)
		}
	}
}

// TestDetectRuntime_CacheIsStable verifies the sync.Once caching behaviour.
// Second call must return the same value as the first even if the host
// binaries change — operators swapping runtimes require a service restart.
func TestDetectRuntime_CacheIsStable(t *testing.T) {
	resetRuntimeForTesting()
	first := DetectRuntime()
	second := DetectRuntime()
	if first != second {
		t.Errorf("DetectRuntime not stable across calls: first=%v second=%v", first, second)
	}
}
