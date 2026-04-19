package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const daemonConfigPath = "/etc/docker/daemon.json"

// DaemonConfig represents the managed subset of Docker daemon.json settings.
type DaemonConfig struct {
	RegistryMirrors    []string          `json:"registry-mirrors"`
	InsecureRegistries []string          `json:"insecure-registries"`
	LogDriver          string            `json:"log-driver"`
	LogOpts            map[string]string `json:"log-opts"`
	StorageDriver      string            `json:"storage-driver"`
	LiveRestore        *bool             `json:"live-restore"`
}

// ReadDaemonConfig reads /etc/docker/daemon.json and returns both a typed
// DaemonConfig (our managed fields) and the raw map (to preserve unmanaged fields).
func ReadDaemonConfig() (*DaemonConfig, map[string]interface{}, error) {
	raw := make(map[string]interface{})
	cfg := &DaemonConfig{}

	data, err := os.ReadFile(daemonConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, raw, nil
		}
		return nil, nil, fmt.Errorf("read daemon.json: %w", err)
	}

	if len(data) == 0 {
		return cfg, raw, nil
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse daemon.json: %w", err)
	}

	// Re-unmarshal into typed struct to extract managed fields.
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, nil, fmt.Errorf("parse daemon config fields: %w", err)
	}

	return cfg, raw, nil
}

// WriteDaemonConfig merges the managed DaemonConfig fields into the raw map
// (preserving any unmanaged fields) and writes back to /etc/docker/daemon.json.
func WriteDaemonConfig(cfg *DaemonConfig, raw map[string]interface{}) error {
	if raw == nil {
		raw = make(map[string]interface{})
	}

	// Merge managed fields into raw map.
	// For arrays: set if non-empty, delete if empty.
	if len(cfg.RegistryMirrors) > 0 {
		raw["registry-mirrors"] = cfg.RegistryMirrors
	} else {
		delete(raw, "registry-mirrors")
	}

	if len(cfg.InsecureRegistries) > 0 {
		raw["insecure-registries"] = cfg.InsecureRegistries
	} else {
		delete(raw, "insecure-registries")
	}

	if cfg.LogDriver != "" {
		raw["log-driver"] = cfg.LogDriver
	} else {
		delete(raw, "log-driver")
	}

	if len(cfg.LogOpts) > 0 {
		raw["log-opts"] = cfg.LogOpts
	} else {
		delete(raw, "log-opts")
	}

	if cfg.StorageDriver != "" {
		raw["storage-driver"] = cfg.StorageDriver
	} else {
		delete(raw, "storage-driver")
	}

	if cfg.LiveRestore != nil {
		raw["live-restore"] = *cfg.LiveRestore
	} else {
		delete(raw, "live-restore")
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon.json: %w", err)
	}
	data = append(data, '\n')

	// Ensure /etc/docker/ directory exists.
	if err := os.MkdirAll(filepath.Dir(daemonConfigPath), 0755); err != nil {
		return fmt.Errorf("create docker config dir: %w", err)
	}

	// Atomic write: write to .tmp then rename.
	tmpPath := daemonConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write daemon.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, daemonConfigPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename daemon.json.tmp: %w", err)
	}

	return nil
}

// WriteDaemonConfigRaw writes raw bytes to the daemon.json file atomically.
// Used for rollback when a restart with new config fails.
func WriteDaemonConfigRaw(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(daemonConfigPath), 0755); err != nil {
		return fmt.Errorf("create docker config dir: %w", err)
	}
	tmpPath := daemonConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write daemon.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, daemonConfigPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename daemon.json.tmp: %w", err)
	}
	return nil
}

// ErrDaemonConfigNotSupportedOnPodman is returned when the UI attempts to
// apply /etc/docker/daemon.json changes on a host where Podman is the
// active runtime. Podman reads its configuration from
// /etc/containers/containers.conf and /etc/containers/registries.conf;
// /etc/docker/daemon.json is silently ignored. Rather than pretend a
// restart applied the config, surface this to the UI so the admin
// isn't misled.
var ErrDaemonConfigNotSupportedOnPodman = fmt.Errorf("daemon.json is Docker-specific and has no effect under Podman — edit /etc/containers/containers.conf and /etc/containers/registries.conf instead; a dedicated Podman config UI is planned for a future phase")

// RestartDockerDaemon restarts the container runtime so daemon.json changes
// take effect. Docker hosts: standard `systemctl restart docker`. Podman
// hosts: refuse with ErrDaemonConfigNotSupportedOnPodman — the daemon.json
// UI has no effect on Podman, and pretending otherwise makes the panel
// lie to the admin about the applied config. Callers should check for the
// sentinel error and render a Podman-specific explanation.
//
// When the runtime is Unknown (neither binary present), returns a generic
// error so the admin can install a runtime via the install flow.
func RestartDockerDaemon() error {
	switch DetectRuntime() {
	case RuntimeDocker:
		// Legacy path: only reachable on hosts where Podman is not installed
		// but the `docker` CLI is. v0.12+ users always hit the RuntimePodman
		// branch below. See docs/08-podman-docker-shim-future.md for the
		// long-term plan.
		return exec.Command("systemctl", "restart", "docker").Run()
	case RuntimePodman:
		return ErrDaemonConfigNotSupportedOnPodman
	default:
		return fmt.Errorf("no container runtime detected; cannot restart daemon")
	}
}
