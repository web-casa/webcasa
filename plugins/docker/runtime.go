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
	runtimeOnce   sync.Once
	runtimeCache  Runtime
)

// DetectRuntime inspects the host once and caches the result. Detection
// order: (1) podman binary present = Podman (v0.12 default via podman-docker
// shim); (2) docker binary present = Docker; (3) neither = Unknown.
//
// This is a process-local cache — it is safe to call freely from HTTP
// handlers without burning subprocess fork cost per request. If an operator
// swaps runtimes without restarting WebCasa, they need a service restart to
// pick up the new value (acceptable: runtime swap is a rare admin action).
func DetectRuntime() Runtime {
	runtimeOnce.Do(func() {
		if _, err := exec.LookPath("podman"); err == nil {
			runtimeCache = RuntimePodman
			return
		}
		if _, err := exec.LookPath("docker"); err == nil {
			runtimeCache = RuntimeDocker
			return
		}
		runtimeCache = RuntimeUnknown
	})
	return runtimeCache
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
		if out, err := exec.Command("docker", "--version").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}
