package docker

import (
	"os/exec"
	"strings"
	"sync"
)

// Runtime identifies the container engine reachable via the Docker CLI/socket.
// WebCasa v0.12 targets Podman exclusively, but the `docker` CLI shim can
// either be real Docker (pre-v0.12 systems) or podman-docker (v0.12+).
// Detection drives which systemd unit to restart (podman.socket vs docker)
// and lets install-docker style flows short-circuit cleanly.
type Runtime int

const (
	RuntimeUnknown Runtime = iota
	RuntimeDocker
	RuntimePodman
)

// String renders the runtime enum for logs and user-facing messages.
func (r Runtime) String() string {
	switch r {
	case RuntimeDocker:
		return "docker"
	case RuntimePodman:
		return "podman"
	default:
		return "unknown"
	}
}

// SystemdUnit names the systemd unit that controls this runtime's socket.
// Empty when the runtime is unknown.
func (r Runtime) SystemdUnit() string {
	switch r {
	case RuntimeDocker:
		return "docker"
	case RuntimePodman:
		return "podman.socket"
	default:
		return ""
	}
}

var (
	runtimeOnce  sync.Once
	runtimeCache Runtime
	runtimeMu    sync.RWMutex // guards runtimeCache + runtimeOnce on reset
)

// DetectRuntime inspects the host once and caches the result. Detection
// order: (1) podman binary present = Podman (v0.12 default via podman-docker
// shim); (2) docker binary present = Docker; (3) neither = Unknown.
//
// This is a process-local cache — it is safe to call freely from HTTP
// handlers without burning subprocess fork cost per request. The cache can
// be invalidated with ResetRuntimeCache after the install flow provisions a
// runtime, so callers don't see stale "unknown" state until process restart.
func DetectRuntime() Runtime {
	runtimeMu.RLock()
	once := &runtimeOnce
	runtimeMu.RUnlock()
	once.Do(func() {
		r := detectRuntimeUncached()
		runtimeMu.Lock()
		runtimeCache = r
		runtimeMu.Unlock()
	})
	runtimeMu.RLock()
	r := runtimeCache
	runtimeMu.RUnlock()
	return r
}

// detectRuntimeUncached performs a PATH lookup without consulting the cache.
func detectRuntimeUncached() Runtime {
	if _, err := exec.LookPath("podman"); err == nil {
		return RuntimePodman
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return RuntimeDocker
	}
	return RuntimeUnknown
}

// ResetRuntimeCache clears the DetectRuntime cache. Call after an install or
// uninstall flow so subsequent /status + /daemon-config lookups re-probe the
// host. Safe to call concurrently. Cheap: the next DetectRuntime call does
// two exec.LookPath probes (PATH-resolved, no fork).
func ResetRuntimeCache() {
	runtimeMu.Lock()
	runtimeOnce = sync.Once{}
	runtimeCache = RuntimeUnknown
	runtimeMu.Unlock()
}

// RuntimeVersion returns a one-line human-readable version string for the
// detected runtime, e.g. "podman version 5.6.0" or "Docker version 24.0.7".
// Used in dockerStatus responses and install-flow messaging.
func RuntimeVersion() string {
	switch DetectRuntime() {
	case RuntimePodman:
		if out, err := exec.Command("podman", "--version").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	case RuntimeDocker:
		// Legacy path for hosts that still have Docker (pre-v0.12 installs);
		// v0.12+ always reaches the RuntimePodman branch. See
		// docs/08-podman-docker-shim-future.md.
		if out, err := exec.Command("docker", "--version").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}
