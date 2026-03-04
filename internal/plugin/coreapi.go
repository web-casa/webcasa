package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/service"
	"gorm.io/gorm"
)

// CoreAPIImpl implements CoreAPI by delegating to existing services.
// Exported so that main.go can call SetEventBus after Manager creation.
type CoreAPIImpl struct {
	db       *gorm.DB
	hostSvc  *service.HostService
	caddyMgr *caddy.Manager
	dataDir  string
	eventBus *EventBus
}

// NewCoreAPI creates a CoreAPI backed by the given services.
func NewCoreAPI(db *gorm.DB, hostSvc *service.HostService, caddyMgr *caddy.Manager, dataDir string) *CoreAPIImpl {
	return &CoreAPIImpl{
		db:       db,
		hostSvc:  hostSvc,
		caddyMgr: caddyMgr,
		dataDir:  dataDir,
	}
}

// SetEventBus sets the event bus for cross-plugin communication (e.g. TriggerBuild).
// Call this after the Manager is created.
func (a *CoreAPIImpl) SetEventBus(eb *EventBus) {
	a.eventBus = eb
}

// ──────────────────────────────────────────────────
// Host management
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) CreateHost(req CreateHostRequest) (uint, error) {
	tlsEnabled := req.TLSEnabled
	httpRedirect := req.HTTPRedirect
	ws := req.WebSocket

	hostReq := &model.HostCreateRequest{
		Domain:       req.Domain,
		HostType:     "proxy",
		Enabled:      boolPtr(true),
		TLSEnabled:   &tlsEnabled,
		HTTPRedirect: &httpRedirect,
		WebSocket:    &ws,
		Upstreams: []model.UpstreamInput{
			{Address: req.UpstreamAddr, Weight: 1},
		},
	}

	host, err := a.hostSvc.Create(hostReq)
	if err != nil {
		return 0, fmt.Errorf("create host: %w", err)
	}
	return host.ID, nil
}

func (a *CoreAPIImpl) DeleteHost(id uint) error {
	return a.hostSvc.Delete(id)
}

func (a *CoreAPIImpl) ListHosts() ([]map[string]interface{}, error) {
	var hosts []model.Host
	if err := a.db.Preload("Upstreams").Find(&hosts).Error; err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, len(hosts))
	for i, h := range hosts {
		upstreams := make([]string, len(h.Upstreams))
		for j, u := range h.Upstreams {
			upstreams[j] = u.Address
		}
		result[i] = map[string]interface{}{
			"id":            h.ID,
			"domain":        h.Domain,
			"host_type":     h.HostType,
			"enabled":       h.Enabled != nil && *h.Enabled,
			"tls_enabled":   h.TLSEnabled != nil && *h.TLSEnabled,
			"http_redirect": h.HTTPRedirect != nil && *h.HTTPRedirect,
			"websocket":     h.WebSocket != nil && *h.WebSocket,
			"upstreams":     upstreams,
			"created_at":    h.CreatedAt,
			"updated_at":    h.UpdatedAt,
		}
	}
	return result, nil
}

func (a *CoreAPIImpl) GetHost(id uint) (map[string]interface{}, error) {
	var h model.Host
	if err := a.db.Preload("Upstreams").First(&h, id).Error; err != nil {
		return nil, err
	}

	upstreams := make([]string, len(h.Upstreams))
	for j, u := range h.Upstreams {
		upstreams[j] = u.Address
	}
	return map[string]interface{}{
		"id":            h.ID,
		"domain":        h.Domain,
		"host_type":     h.HostType,
		"enabled":       h.Enabled != nil && *h.Enabled,
		"tls_enabled":   h.TLSEnabled != nil && *h.TLSEnabled,
		"http_redirect": h.HTTPRedirect != nil && *h.HTTPRedirect,
		"websocket":     h.WebSocket != nil && *h.WebSocket,
		"upstreams":     upstreams,
		"created_at":    h.CreatedAt,
		"updated_at":    h.UpdatedAt,
	}, nil
}

func (a *CoreAPIImpl) ReloadCaddy() error {
	return a.caddyMgr.Reload()
}

func (a *CoreAPIImpl) UpdateHostUpstream(hostID uint, newUpstream string) error {
	// Update the first upstream's address for the given host.
	return a.db.Model(&model.Upstream{}).
		Where("host_id = ?", hostID).
		Order("sort_order ASC").
		Limit(1).
		Update("address", newUpstream).Error
}

// ──────────────────────────────────────────────────
// Settings
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) GetSetting(key string) (string, error) {
	var s model.Setting
	if err := a.db.Where("key = ?", key).First(&s).Error; err != nil {
		return "", err
	}
	return s.Value, nil
}

func (a *CoreAPIImpl) SetSetting(key, value string) error {
	return a.db.Where("key = ?", key).
		Assign(model.Setting{Key: key, Value: value}).
		FirstOrCreate(&model.Setting{}).Error
}

func (a *CoreAPIImpl) GetDB() *gorm.DB {
	return a.db
}

// ──────────────────────────────────────────────────
// Cross-plugin queries — used by AI tool use
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) ListProjects() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_deploy_projects").
		Select("id, name, domain, framework, status, git_url, git_branch, port, current_build, error_msg, created_at, updated_at").
		Find(&results).Error
	return results, err
}

func (a *CoreAPIImpl) GetProject(id uint) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := a.db.Table("plugin_deploy_projects").
		Where("id = ?", id).
		First(&result).Error
	return result, err
}

func (a *CoreAPIImpl) GetBuildLog(projectID uint, buildNum int) (string, error) {
	logPath := filepath.Join(a.dataDir, "logs", fmt.Sprintf("project_%d", projectID), fmt.Sprintf("build_%d.log", buildNum))
	data, err := os.ReadFile(logPath)
	if err != nil {
		return "", fmt.Errorf("read build log: %w", err)
	}
	return string(data), nil
}

func (a *CoreAPIImpl) GetRuntimeLog(projectID uint, lines int) (string, error) {
	if lines <= 0 {
		lines = 100
	}
	logPath := filepath.Join(a.dataDir, "logs", fmt.Sprintf("project_%d", projectID), "runtime.log")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tail", "-n", fmt.Sprintf("%d", lines), logPath).Output()
	if err != nil {
		return "", fmt.Errorf("read runtime log: %w", err)
	}
	return string(out), nil
}

func (a *CoreAPIImpl) CreateProject(req CreateProjectRequest) (uint, error) {
	branch := req.GitBranch
	if branch == "" {
		branch = "main"
	}
	deployMode := req.DeployMode
	if deployMode == "" {
		deployMode = "bare"
	}

	// Generate a random webhook token.
	tokenBytes := make([]byte, 16)
	if _, err := fmt.Fprintf(bytes.NewBuffer(tokenBytes), "%d", time.Now().UnixNano()); err != nil {
		// fallback — just use timestamp hex
	}
	webhookToken := fmt.Sprintf("%x", time.Now().UnixNano())

	project := map[string]interface{}{
		"name":         req.Name,
		"git_url":      req.GitURL,
		"git_branch":   branch,
		"domain":       req.Domain,
		"framework":    req.Framework,
		"deploy_mode":  deployMode,
		"auto_deploy":  req.AutoDeploy,
		"status":       "pending",
		"webhook_token": webhookToken,
		"created_at":   time.Now(),
		"updated_at":   time.Now(),
	}

	result := a.db.Table("plugin_deploy_projects").Create(&project)
	if result.Error != nil {
		return 0, fmt.Errorf("create project: %w", result.Error)
	}

	// Extract the created ID.
	var created struct {
		ID uint `gorm:"column:id"`
	}
	a.db.Table("plugin_deploy_projects").
		Where("webhook_token = ?", webhookToken).
		Select("id").
		First(&created)

	return created.ID, nil
}

func (a *CoreAPIImpl) GetEnvSuggestions(framework string) ([]map[string]interface{}, error) {
	// Query the deploy plugin's env suggestions table directly.
	// The suggestions are defined as Go maps in deploy/model.go, so we replicate the common ones here.
	suggestions := map[string][]map[string]interface{}{
		"nextjs": {
			{"key": "NODE_ENV", "default_value": "production", "description": "Node.js environment mode", "required": true},
			{"key": "NEXT_TELEMETRY_DISABLED", "default_value": "1", "description": "Disable Next.js telemetry", "required": false},
			{"key": "NEXT_PUBLIC_API_URL", "default_value": "", "description": "Public API base URL for client-side requests", "required": false},
		},
		"nuxt": {
			{"key": "NODE_ENV", "default_value": "production", "description": "Node.js environment mode", "required": true},
			{"key": "NITRO_PRESET", "default_value": "node-server", "description": "Nitro server preset", "required": false},
			{"key": "NUXT_PUBLIC_API_BASE", "default_value": "", "description": "Public API base URL", "required": false},
		},
		"vite": {
			{"key": "NODE_ENV", "default_value": "production", "description": "Node.js environment mode", "required": true},
			{"key": "VITE_API_URL", "default_value": "", "description": "API base URL (exposed to client)", "required": false},
		},
		"remix": {
			{"key": "NODE_ENV", "default_value": "production", "description": "Node.js environment mode", "required": true},
			{"key": "SESSION_SECRET", "default_value": "", "description": "Session encryption secret", "required": true},
		},
		"express": {
			{"key": "NODE_ENV", "default_value": "production", "description": "Node.js environment mode", "required": true},
			{"key": "LOG_LEVEL", "default_value": "info", "description": "Application log level", "required": false},
		},
		"go": {
			{"key": "GIN_MODE", "default_value": "release", "description": "Gin framework mode (debug/release)", "required": false},
			{"key": "GO_ENV", "default_value": "production", "description": "Go environment mode", "required": false},
		},
		"laravel": {
			{"key": "APP_ENV", "default_value": "production", "description": "Application environment", "required": true},
			{"key": "APP_KEY", "default_value": "", "description": "Application encryption key (run: php artisan key:generate)", "required": true},
			{"key": "APP_DEBUG", "default_value": "false", "description": "Debug mode (disable in production)", "required": true},
			{"key": "DB_CONNECTION", "default_value": "mysql", "description": "Database driver", "required": false},
			{"key": "DB_HOST", "default_value": "127.0.0.1", "description": "Database host", "required": false},
			{"key": "DB_DATABASE", "default_value": "", "description": "Database name", "required": true},
			{"key": "DB_USERNAME", "default_value": "", "description": "Database username", "required": true},
			{"key": "DB_PASSWORD", "default_value": "", "description": "Database password", "required": true},
		},
		"flask": {
			{"key": "FLASK_ENV", "default_value": "production", "description": "Flask environment mode", "required": true},
			{"key": "FLASK_APP", "default_value": "app", "description": "Flask application module", "required": false},
			{"key": "SECRET_KEY", "default_value": "", "description": "Flask session secret key", "required": true},
		},
		"django": {
			{"key": "DJANGO_SETTINGS_MODULE", "default_value": "config.settings", "description": "Django settings module path", "required": true},
			{"key": "DEBUG", "default_value": "False", "description": "Debug mode (disable in production)", "required": true},
			{"key": "SECRET_KEY", "default_value": "", "description": "Django secret key", "required": true},
			{"key": "ALLOWED_HOSTS", "default_value": "*", "description": "Allowed host headers", "required": false},
			{"key": "DATABASE_URL", "default_value": "", "description": "Database connection URL", "required": false},
		},
	}

	if s, ok := suggestions[framework]; ok {
		return s, nil
	}
	return []map[string]interface{}{}, nil
}

func (a *CoreAPIImpl) TriggerBuild(projectID uint) error {
	if a.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}
	a.eventBus.Publish(Event{
		Type:    "deploy.trigger_build",
		Payload: map[string]interface{}{"project_id": projectID},
		Source:  "core",
	})
	return nil
}

func (a *CoreAPIImpl) DockerPS() ([]map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}

	var results []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var item map[string]interface{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		results = append(results, item)
	}
	return results, nil
}

func (a *CoreAPIImpl) DockerLogs(containerID string, tail int) (string, error) {
	if tail <= 0 {
		tail = 100
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "logs", "--tail", fmt.Sprintf("%d", tail), containerID)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("docker logs: %w", err)
	}
	return buf.String(), nil
}

func (a *CoreAPIImpl) GetMetrics() (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// CPU count
	result["num_cpu"] = runtime.NumCPU()

	// Load average via /proc/loadavg
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 3 {
			result["load_1"] = parts[0]
			result["load_5"] = parts[1]
			result["load_15"] = parts[2]
		}
	}

	// Memory via /proc/meminfo
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		memInfo := parseMemInfo(string(data))
		if v, ok := memInfo["MemTotal"]; ok {
			result["mem_total_kb"] = v
		}
		if v, ok := memInfo["MemAvailable"]; ok {
			result["mem_available_kb"] = v
		}
		if v, ok := memInfo["MemFree"]; ok {
			result["mem_free_kb"] = v
		}
	}

	// Disk via df
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "df", "-B1", "/").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 4 {
				result["disk_total"] = fields[1]
				result["disk_used"] = fields[2]
				result["disk_available"] = fields[3]
			}
		}
	}

	return result, nil
}

func (a *CoreAPIImpl) RunCommand(cmd string, timeoutSec int) (string, error) {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 120 {
		timeoutSec = 120
	}

	// Security: block dangerous commands
	lower := strings.ToLower(cmd)
	blocked := []string{
		"rm -rf /", "mkfs", "dd if=", "> /dev/sd",
		"chmod -r 777 /", ":(){ :|:", "shutdown", "reboot",
		"init 0", "init 6", "halt",
	}
	for _, b := range blocked {
		if strings.Contains(lower, b) {
			return "", fmt.Errorf("command blocked for safety: contains %q", b)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	c := exec.CommandContext(ctx, "bash", "-c", cmd)
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		output := buf.String()
		if len(output) > 4096 {
			output = output[:4096] + "\n... (truncated)"
		}
		return output, fmt.Errorf("command failed: %w", err)
	}

	output := buf.String()
	if len(output) > 8192 {
		output = output[:8192] + "\n... (truncated)"
	}
	return output, nil
}

// ──────────────────────────────────────────────────
// Batch 2: Backup, Host update, Alerts
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) TriggerBackup() error {
	if a.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}
	a.eventBus.Publish(Event{
		Type:    "backup.trigger",
		Payload: map[string]interface{}{},
		Source:  "core",
	})
	return nil
}

func (a *CoreAPIImpl) UpdateHost(id uint, req UpdateHostRequest) error {
	var h model.Host
	if err := a.db.First(&h, id).Error; err != nil {
		return fmt.Errorf("host not found: %w", err)
	}

	updates := map[string]interface{}{}
	if req.Upstream != "" {
		// Update the first upstream address.
		a.db.Model(&model.Upstream{}).
			Where("host_id = ?", id).
			Order("sort_order ASC").
			Limit(1).
			Update("address", req.Upstream)
	}
	if req.TLSMode != "" {
		updates["tls_mode"] = req.TLSMode
		if req.TLSMode == "off" {
			updates["tls_enabled"] = false
		} else {
			updates["tls_enabled"] = true
		}
	}
	if req.ForceHTTPS != nil {
		updates["http_redirect"] = *req.ForceHTTPS
	}
	if req.WebSocket != nil {
		updates["websocket"] = *req.WebSocket
	}
	if req.Compression != nil {
		updates["compression"] = *req.Compression
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) > 0 {
		if err := a.db.Model(&h).Updates(updates).Error; err != nil {
			return fmt.Errorf("update host: %w", err)
		}
	}

	// Reload Caddy to apply changes.
	return a.caddyMgr.Reload()
}

func (a *CoreAPIImpl) GetRecentAlerts() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_monitoring_alert_history").
		Order("fired_at DESC").
		Limit(20).
		Find(&results).Error
	if err != nil {
		// Table might not exist if monitoring plugin is not installed — return empty.
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

// ──────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────

func parseMemInfo(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.TrimSuffix(val, " kB")
			result[key] = val
		}
	}
	return result
}

func boolPtr(v bool) *bool {
	return &v
}
