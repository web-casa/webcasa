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
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write daemon.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, daemonConfigPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename daemon.json.tmp: %w", err)
	}

	return nil
}

// RestartDockerDaemon restarts the Docker daemon via systemctl.
func RestartDockerDaemon() error {
	return exec.Command("systemctl", "restart", "docker").Run()
}
