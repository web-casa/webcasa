package caddy

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caddypanel/caddypanel/internal/config"
)

// Manager handles Caddy process lifecycle and configuration reloading
type Manager struct {
	cfg  *config.Config
	mu   sync.Mutex
	proc *os.Process // tracked only when we start Caddy ourselves
}

// NewManager creates a new Caddy manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{cfg: cfg}
}

// WriteCaddyfile atomically writes a Caddyfile:
//  1. Write to temp file
//  2. Validate with `caddy validate`
//  3. Backup current file
//  4. Rename temp → final
func (m *Manager) WriteCaddyfile(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	targetPath := m.cfg.CaddyfilePath
	tmpPath := targetPath + ".tmp"
	backupDir := filepath.Join(m.cfg.DataDir, "backups")

	// 1. Write temp file
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write temp Caddyfile: %w", err)
	}

	// 2. Validate (skip if caddy binary is not available)
	if _, lookErr := exec.LookPath(m.cfg.CaddyBin); lookErr == nil {
		cmd := exec.Command(m.cfg.CaddyBin, "validate", "--config", tmpPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("Caddyfile validation failed: %s\n%s", err, string(output))
		}
	} else {
		log.Printf("⚠️  Caddy binary not found (%s), skipping validation", m.cfg.CaddyBin)
	}

	// 3. Backup current file (if exists)
	if _, err := os.Stat(targetPath); err == nil {
		backupName := fmt.Sprintf("Caddyfile.%s.bak", time.Now().Format("20060102-150405"))
		backupPath := filepath.Join(backupDir, backupName)
		data, _ := os.ReadFile(targetPath)
		os.WriteFile(backupPath, data, 0644)

		// Keep only last 10 backups
		m.cleanupBackups(backupDir, 10)
	}

	// 4. Atomic rename
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("failed to rename Caddyfile: %w", err)
	}

	log.Printf("Caddyfile written successfully to %s", targetPath)
	return nil
}

// Reload tells Caddy to reload its configuration
func (m *Manager) Reload() error {
	cmd := exec.Command(m.cfg.CaddyBin, "reload", "--config", m.cfg.CaddyfilePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("caddy reload failed: %s\n%s", err, string(output))
	}
	log.Println("Caddy reloaded successfully")
	return nil
}

// Start starts the Caddy process
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.IsRunning() {
		return fmt.Errorf("caddy is already running")
	}

	cmd := exec.Command(m.cfg.CaddyBin, "start", "--config", m.cfg.CaddyfilePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("caddy start failed: %s\n%s", err, string(output))
	}
	log.Println("Caddy started successfully")
	return nil
}

// Stop stops the Caddy process
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := exec.Command(m.cfg.CaddyBin, "stop")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("caddy stop failed: %s\n%s", err, string(output))
	}
	log.Println("Caddy stopped successfully")
	return nil
}

// IsRunning checks if a Caddy process is currently running
func (m *Manager) IsRunning() bool {
	// Try to hit the admin API
	cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", m.cfg.AdminAPI+"/config/")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "200"
}

// Status returns the current Caddy status
func (m *Manager) Status() map[string]interface{} {
	running := m.IsRunning()
	status := map[string]interface{}{
		"running":        running,
		"caddy_bin":      m.cfg.CaddyBin,
		"caddyfile_path": m.cfg.CaddyfilePath,
	}

	if running {
		// Get Caddy version
		cmd := exec.Command(m.cfg.CaddyBin, "version")
		output, err := cmd.Output()
		if err == nil {
			status["version"] = strings.TrimSpace(string(output))
		}
	}

	return status
}

// GetCaddyfileContent returns the current Caddyfile content
func (m *Manager) GetCaddyfileContent() (string, error) {
	data, err := os.ReadFile(m.cfg.CaddyfilePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *Manager) cleanupBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var backups []os.DirEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "Caddyfile.") && strings.HasSuffix(e.Name(), ".bak") {
			backups = append(backups, e)
		}
	}

	if len(backups) <= keep {
		return
	}

	// Sort by name (which includes timestamp), oldest first
	for i := 0; i < len(backups)-keep; i++ {
		os.Remove(filepath.Join(dir, backups[i].Name()))
	}
}
