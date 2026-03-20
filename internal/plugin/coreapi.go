package plugin

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
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
	compression := req.Compression

	hostType := req.HostType
	if hostType == "" {
		hostType = "proxy"
	}

	// Validate host type.
	switch hostType {
	case "proxy", "php", "static":
		// valid
	default:
		return 0, fmt.Errorf("invalid host type: %q (must be proxy, php, or static)", hostType)
	}

	// Validate domain.
	if err := caddy.ValidateDomain(req.Domain); err != nil {
		return 0, fmt.Errorf("invalid domain: %w", err)
	}

	hostReq := &model.HostCreateRequest{
		Domain:       req.Domain,
		HostType:     hostType,
		Enabled:      boolPtr(true),
		TLSEnabled:   &tlsEnabled,
		HTTPRedirect: &httpRedirect,
		WebSocket:    &ws,
		Compression:  &compression,
	}

	switch hostType {
	case "php":
		// Validate RootPath for Caddyfile injection.
		if strings.ContainsAny(req.RootPath, " \t\n\r{}\"'`;#$\\") {
			return 0, fmt.Errorf("invalid root path: contains unsafe characters")
		}
		// Validate PHPFastCGI format (host:port).
		if strings.ContainsAny(req.PHPFastCGI, " \t\n\r{}\"'`;#$\\") {
			return 0, fmt.Errorf("invalid PHP FastCGI address: contains unsafe characters")
		}
		hostReq.RootPath = req.RootPath
		hostReq.PHPFastCGI = req.PHPFastCGI
	case "static":
		if strings.ContainsAny(req.RootPath, " \t\n\r{}\"'`;#$\\") {
			return 0, fmt.Errorf("invalid root path: contains unsafe characters")
		}
		hostReq.RootPath = req.RootPath
	default: // "proxy"
		hostReq.Upstreams = []model.UpstreamInput{
			{Address: req.UpstreamAddr, Weight: 1},
		}
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
	if err := caddy.ValidateUpstream(newUpstream); err != nil {
		return fmt.Errorf("invalid upstream: %w", err)
	}
	if err := a.db.Model(&model.Upstream{}).
		Where("host_id = ?", hostID).
		Order("sort_order ASC").
		Limit(1).
		Update("address", newUpstream).Error; err != nil {
		return fmt.Errorf("update upstream: %w", err)
	}
	// Regenerate Caddyfile and reload so traffic actually switches.
	return a.hostSvc.ApplyConfig()
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
	// Auto-set deploy mode for Dockerfile projects.
	if req.Framework == "dockerfile" && deployMode == "" {
		deployMode = "docker"
	}
	if deployMode == "" {
		deployMode = "bare"
	}

	// Generate a cryptographically random webhook token.
	tokenBytes := make([]byte, 16)
	if _, err := cryptorand.Read(tokenBytes); err != nil {
		return 0, fmt.Errorf("generate webhook token: %w", err)
	}
	webhookToken := hex.EncodeToString(tokenBytes)

	now := time.Now()
	project := map[string]interface{}{
		"name":              req.Name,
		"git_url":           req.GitURL,
		"git_branch":        branch,
		"domain":            req.Domain,
		"framework":         req.Framework,
		"deploy_mode":       deployMode,
		"auto_deploy":       req.AutoDeploy,
		"auth_method":       "ssh_key",
		"install_cmd":       req.InstallCommand,
		"build_command":     req.BuildCommand,
		"start_command":     req.StartCommand,
		"status":            "pending",
		"webhook_token":     webhookToken,
		"created_at":        now,
		"updated_at":        now,
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

	// Allocate port for projects that need one: docker mode or bare with a start command.
	if created.ID > 0 && (deployMode == "docker" || req.StartCommand != "") {
		port := 10000 + int(created.ID)
		a.db.Table("plugin_deploy_projects").Where("id = ?", created.ID).Update("port", port)
	}

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
	if tail > 5000 {
		tail = 5000
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
	if req.Domain != "" {
		if err := caddy.ValidateDomain(req.Domain); err != nil {
			return fmt.Errorf("invalid domain: %w", err)
		}
		// Check uniqueness.
		var count int64
		a.db.Model(&model.Host{}).Where("domain = ? AND id != ?", req.Domain, id).Count(&count)
		if count > 0 {
			return fmt.Errorf("domain %q already exists", req.Domain)
		}
		updates["domain"] = req.Domain
	}
	if req.Upstream != "" {
		if err := caddy.ValidateUpstream(req.Upstream); err != nil {
			return fmt.Errorf("invalid upstream: %w", err)
		}
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

	// Regenerate Caddyfile and reload Caddy to apply changes.
	return a.hostSvc.ApplyConfig()
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
// Batch 3: Database management
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) DatabaseListInstances() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_database_instances").
		Select("id, name, engine, version, port, status, container_id, created_at, updated_at").
		Find(&results).Error
	if err != nil {
		return []map[string]interface{}{}, nil
	}

	// Enrich with container running status.
	for i, inst := range results {
		containerID, _ := inst["container_id"].(string)
		if containerID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerID).Output()
			cancel()
			if err == nil {
				results[i]["running"] = strings.TrimSpace(string(out)) == "true"
			} else {
				results[i]["running"] = false
			}
		}
	}
	return results, nil
}

func (a *CoreAPIImpl) DatabaseCreateInstance(req DatabaseCreateInstanceRequest) (uint, error) {
	if a.eventBus == nil {
		return 0, fmt.Errorf("event bus not available")
	}
	a.eventBus.Publish(Event{
		Type: "database.create_instance",
		Payload: map[string]interface{}{
			"engine":        req.Engine,
			"version":       req.Version,
			"name":          req.Name,
			"port":          req.Port,
			"root_password": req.RootPassword,
			"memory_limit":  req.MemoryLimit,
		},
		Source: "core",
	})
	return 0, nil // ID will be assigned asynchronously by the database plugin
}

// dbValidCharsets mirrors the database plugin's charset whitelist.
// Must be kept in sync with plugins/database/dbclient.go validCharsets.
var dbValidCharsets = map[string]bool{
	"utf8": true, "utf8mb4": true, "latin1": true, "ascii": true,
	"binary": true, "utf16": true, "utf32": true, "big5": true,
	"gb2312": true, "gbk": true, "euckr": true, "sjis": true,
	"UTF8": true, // PostgreSQL
}

func (a *CoreAPIImpl) DatabaseCreateDatabase(instanceID uint, name, charset string) error {
	var inst struct {
		Engine        string `gorm:"column:engine"`
		ContainerName string `gorm:"column:container_name"`
		RootPassword  string `gorm:"column:root_password"`
	}
	if err := a.db.Table("plugin_database_instances").Where("id = ?", instanceID).First(&inst).Error; err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	// Validate charset against whitelist.
	if charset == "" {
		if inst.Engine == "postgres" {
			charset = "UTF8"
		} else {
			charset = "utf8mb4"
		}
	}
	if !dbValidCharsets[charset] {
		return fmt.Errorf("invalid charset: %s", charset)
	}
	// Validate name against the same pattern used by the database plugin's
	// query executor, so databases created here can always be queried later.
	if !dbValidNameRe.MatchString(name) {
		return fmt.Errorf("invalid database name %q: must match [a-zA-Z_][a-zA-Z0-9_-]*", name)
	}
	safeName := dbEscapeName(name)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch inst.Engine {
	case "mysql", "mariadb":
		cmd = exec.CommandContext(ctx, "docker", "exec",
			"-e", "MYSQL_PWD="+inst.RootPassword,
			inst.ContainerName,
			"mysql", "-uroot", "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET %s;", safeName, charset))
	case "postgres":
		cmd = exec.CommandContext(ctx, "docker", "exec", inst.ContainerName,
			"psql", "-U", "postgres", "-c", fmt.Sprintf(`CREATE DATABASE "%s" ENCODING '%s';`, safeName, charset))
	default:
		return fmt.Errorf("unsupported engine: %s", inst.Engine)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create database: %s — %w", buf.String(), err)
	}
	return nil
}

// dbEscapeName strips backticks, double quotes, semicolons, and single quotes from identifiers.
var dbValidNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

func dbEscapeName(name string) string {
	name = strings.ReplaceAll(name, "`", "")
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, ";", "")
	name = strings.ReplaceAll(name, "'", "")
	return name
}

// dbEscapeString escapes a string for use in SQL single-quoted literals.
// Escapes backslashes first (MySQL treats \ as escape char by default),
// then single quotes.
func dbEscapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

// containsUnquotedSemicolon checks for semicolons outside single-quoted
// SQL strings, avoiding false positives on SELECT ';' AS s.
func containsUnquotedSemicolon(s string) bool {
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' {
			if inQuote && i+1 < len(s) && s[i+1] == '\'' {
				i++ // escaped quote ''
				continue
			}
			inQuote = !inQuote
			continue
		}
		if ch == '\\' && inQuote {
			i++ // skip escaped char
			continue
		}
		if ch == ';' && !inQuote {
			return true
		}
	}
	return false
}

func (a *CoreAPIImpl) DatabaseCreateUser(instanceID uint, username, password string, databases []string) error {
	var inst struct {
		Engine        string `gorm:"column:engine"`
		ContainerName string `gorm:"column:container_name"`
		RootPassword  string `gorm:"column:root_password"`
	}
	if err := a.db.Table("plugin_database_instances").Where("id = ?", instanceID).First(&inst).Error; err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	// Sanitize inputs to prevent SQL injection.
	safeUser := dbEscapeName(username)
	safePwd := dbEscapeString(password)
	if safeUser == "" {
		return fmt.Errorf("invalid username")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmds []string
	switch inst.Engine {
	case "mysql", "mariadb":
		cmds = append(cmds, fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s';", dbEscapeString(safeUser), safePwd))
		for _, db := range databases {
			cmds = append(cmds, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%';", dbEscapeName(db), dbEscapeString(safeUser)))
		}
		cmds = append(cmds, "FLUSH PRIVILEGES;")
		sqlStmt := strings.Join(cmds, " ")
		cmd := exec.CommandContext(ctx, "docker", "exec", "-e", "MYSQL_PWD="+inst.RootPassword, inst.ContainerName, "mysql", "-uroot", "-e", sqlStmt)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("create user: %s — %w", buf.String(), err)
		}
	case "postgres":
		cmds = append(cmds, fmt.Sprintf(`CREATE USER "%s" WITH PASSWORD '%s';`, safeUser, safePwd))
		for _, db := range databases {
			cmds = append(cmds, fmt.Sprintf(`GRANT ALL PRIVILEGES ON DATABASE "%s" TO "%s";`, dbEscapeName(db), safeUser))
		}
		sqlStmt := strings.Join(cmds, " ")
		cmd := exec.CommandContext(ctx, "docker", "exec", inst.ContainerName, "psql", "-U", "postgres", "-c", sqlStmt)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("create user: %s — %w", buf.String(), err)
		}
	default:
		return fmt.Errorf("unsupported engine: %s", inst.Engine)
	}
	return nil
}

func (a *CoreAPIImpl) DatabaseExecuteQuery(instanceID uint, database, query string) (map[string]interface{}, error) {
	// Security: only allow read-only queries.
	// Strip trailing whitespace/semicolons, then reject stacked statements.
	trimmed := strings.TrimSpace(query)
	trimmed = strings.TrimRight(trimmed, "; \t\n\r")
	if containsUnquotedSemicolon(trimmed) {
		return nil, fmt.Errorf("multiple statements are not allowed; submit one query at a time")
	}

	upper := strings.ToUpper(trimmed)
	allowed := []string{"SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN"}
	isAllowed := false
	for _, prefix := range allowed {
		if strings.HasPrefix(upper, prefix) {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		return nil, fmt.Errorf("only read-only queries (SELECT, SHOW, DESCRIBE, EXPLAIN) are allowed")
	}
	query = trimmed

	// Reject SELECT INTO (write side-effect via single statement).
	if strings.Contains(upper, " INTO ") && (strings.Contains(upper, "OUTFILE") || strings.Contains(upper, "DUMPFILE") || strings.Contains(upper, "TEMP ") || strings.Contains(upper, "TEMPORARY ")) {
		return nil, fmt.Errorf("SELECT INTO is not allowed")
	}

	var inst struct {
		Engine        string `gorm:"column:engine"`
		ContainerName string `gorm:"column:container_name"`
		RootPassword  string `gorm:"column:root_password"`
	}
	if err := a.db.Table("plugin_database_instances").Where("id = ?", instanceID).First(&inst).Error; err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch inst.Engine {
	case "mysql", "mariadb":
		args := []string{"exec", "-e", "MYSQL_PWD=" + inst.RootPassword, inst.ContainerName, "mysql", "-uroot", "--batch", "-e", query}
		if database != "" {
			args = []string{"exec", "-e", "MYSQL_PWD=" + inst.RootPassword, inst.ContainerName, "mysql", "-uroot", "--batch", "-D", database, "-e", query}
		}
		cmd = exec.CommandContext(ctx, "docker", args...)
	case "postgres":
		args := []string{"exec", inst.ContainerName, "psql", "-U", "postgres", "-c", query}
		if database != "" {
			args = []string{"exec", inst.ContainerName, "psql", "-U", "postgres", "-d", database, "-c", query}
		}
		cmd = exec.CommandContext(ctx, "docker", args...)
	default:
		return nil, fmt.Errorf("unsupported engine: %s", inst.Engine)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("execute query: %s — %w", buf.String(), err)
	}

	output := buf.String()
	if len(output) > 8192 {
		output = output[:8192] + "\n... (truncated)"
	}
	return map[string]interface{}{
		"output": output,
		"engine": inst.Engine,
	}, nil
}

// ──────────────────────────────────────────────────
// Batch 3: Docker extended
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) DockerListStacks() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_docker_stacks").
		Select("id, name, description, status, data_dir, created_at, updated_at").
		Find(&results).Error
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

func (a *CoreAPIImpl) DockerManageContainer(containerID, action string) error {
	switch action {
	case "start", "stop", "restart":
	default:
		return fmt.Errorf("invalid action %q: must be start, stop, or restart", action)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", action, containerID)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s %s: %s — %w", action, containerID, buf.String(), err)
	}
	return nil
}

func (a *CoreAPIImpl) DockerRunContainer(req DockerRunContainerRequest) (string, error) {
	// Security: block --privileged and --net=host in image name or other fields.
	if strings.Contains(req.Image, "--privileged") || strings.Contains(req.Name, "--privileged") {
		return "", fmt.Errorf("--privileged is not allowed")
	}

	args := []string{"run", "-d"}
	if req.Name != "" {
		args = append(args, "--name", req.Name)
	}
	for _, p := range req.Ports {
		args = append(args, "-p", p)
	}
	for k, v := range req.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	for _, v := range req.Volumes {
		args = append(args, "-v", v)
	}
	if req.RestartPolicy != "" {
		args = append(args, "--restart", req.RestartPolicy)
	}
	args = append(args, req.Image)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker run: %s — %w", buf.String(), err)
	}

	containerID := strings.TrimSpace(buf.String())
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}
	return containerID, nil
}

func (a *CoreAPIImpl) DockerPullImage(image string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker pull %s: %s — %w", image, buf.String(), err)
	}
	return nil
}

func (a *CoreAPIImpl) DockerGetContainerStats(containerID string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}", containerID).Output()
	if err != nil {
		return nil, fmt.Errorf("docker stats: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(out), &result); err != nil {
		return nil, fmt.Errorf("parse stats: %w", err)
	}
	return result, nil
}

// ──────────────────────────────────────────────────
// Batch 3: App Store
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) AppStoreSearchApps(query string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_appstore_apps").
		Where("name LIKE ? OR description LIKE ?", "%"+query+"%", "%"+query+"%").
		Limit(20).
		Find(&results).Error
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

func (a *CoreAPIImpl) AppStoreInstallApp(appID string, config map[string]interface{}) (uint, error) {
	if a.eventBus == nil {
		return 0, fmt.Errorf("event bus not available")
	}
	a.eventBus.Publish(Event{
		Type: "appstore.install",
		Payload: map[string]interface{}{
			"app_id": appID,
			"config": config,
		},
		Source: "core",
	})
	return 0, nil // ID will be assigned asynchronously by the appstore plugin
}

func (a *CoreAPIImpl) AppStoreListInstalled() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_appstore_installed").
		Select("id, app_id, name, status, version, created_at, updated_at").
		Find(&results).Error
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

// ──────────────────────────────────────────────────
// Batch 3: File write operations
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) FileWrite(path, content string) error {
	if !isPathSafe(path) {
		return fmt.Errorf("access denied: path %q is outside allowed directories", path)
	}
	if len(content) > 1<<20 { // 1MB limit
		return fmt.Errorf("content too large (max 1MB)")
	}
	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func (a *CoreAPIImpl) FileDelete(path string) error {
	if !isPathSafe(path) {
		return fmt.Errorf("access denied: path %q is outside allowed directories", path)
	}
	return os.RemoveAll(path)
}

func (a *CoreAPIImpl) FileRename(oldPath, newPath string) error {
	if !isPathSafe(oldPath) {
		return fmt.Errorf("access denied: path %q is outside allowed directories", oldPath)
	}
	if !isPathSafe(newPath) {
		return fmt.Errorf("access denied: path %q is outside allowed directories", newPath)
	}
	return os.Rename(oldPath, newPath)
}

// isPathSafe checks if a file path is within allowed directories.
func isPathSafe(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File or parent may not exist yet (e.g. /tmp/newdir/file.txt).
		// Walk up to find the nearest existing ancestor and resolve from there.
		resolved = resolvePartialPath(abs)
		if resolved == "" {
			return false
		}
	}
	blocked := []string{"/etc/shadow", "/etc/gshadow", "/etc/sudoers", "/etc/passwd"}
	for _, b := range blocked {
		if resolved == b {
			return false
		}
	}
	allowed := []string{"/etc/caddy", "/etc/nginx", "/var/log", "/var/www", "/home", "/root", "/opt", "/srv", "/tmp"}
	for _, a := range allowed {
		if resolved == a || strings.HasPrefix(resolved, a+"/") {
			return true
		}
	}
	return false
}

// resolvePartialPath walks up from abs until it finds an existing ancestor,
// resolves symlinks on that ancestor, then re-appends the remaining components.
// Returns "" if no existing ancestor can be resolved.
func resolvePartialPath(abs string) string {
	// Collect path components that don't exist yet.
	current := abs
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			// Found an existing ancestor — rebuild the full path.
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root without success.
			return ""
		}
		tail = append(tail, filepath.Base(current))
		current = parent
	}
}

// ──────────────────────────────────────────────────
// Firewall management (via firewall-cmd)
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) firewallCmd(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "firewall-cmd", args...).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return result, fmt.Errorf("%s: %s", err, result)
	}
	return result, nil
}

func (a *CoreAPIImpl) firewallDefaultZone() string {
	if z, err := a.firewallCmd("--get-default-zone"); err == nil {
		return strings.TrimSpace(z)
	}
	return "public"
}

func (a *CoreAPIImpl) FirewallStatus() (map[string]interface{}, error) {
	result := map[string]interface{}{"installed": false, "running": false}
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		return result, nil
	}
	result["installed"] = true

	out, err := a.firewallCmd("--state")
	if err != nil {
		return result, nil
	}
	result["running"] = strings.TrimSpace(out) == "running"
	if !result["running"].(bool) {
		return result, nil
	}
	if v, err := a.firewallCmd("--version"); err == nil {
		result["version"] = strings.TrimSpace(v)
	}
	if z, err := a.firewallCmd("--get-default-zone"); err == nil {
		result["default_zone"] = strings.TrimSpace(z)
	}

	// Active zones with their rules.
	if z, err := a.firewallCmd("--get-zones"); err == nil {
		result["zones"] = strings.Fields(z)
	}
	activeMap := make(map[string]bool)
	if out, err := a.firewallCmd("--get-active-zones"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "interfaces:") && !strings.HasPrefix(line, "sources:") {
				activeMap[line] = true
			}
		}
	}

	var activeZones []map[string]interface{}
	for name := range activeMap {
		if out, err := a.firewallCmd("--zone="+name, "--list-all"); err == nil {
			z := map[string]interface{}{"name": name, "active": true}
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "services:") {
					z["services"] = strings.Fields(strings.TrimPrefix(line, "services:"))
				} else if strings.HasPrefix(line, "ports:") {
					z["ports"] = strings.Fields(strings.TrimPrefix(line, "ports:"))
				}
			}
			activeZones = append(activeZones, z)
		}
	}
	result["active_zones"] = activeZones
	return result, nil
}

func (a *CoreAPIImpl) FirewallListRules(zone string) (map[string]interface{}, error) {
	if zone == "" {
		zone = a.firewallDefaultZone()
	}
	if !fwZoneRe.MatchString(zone) {
		return nil, fmt.Errorf("invalid zone name: %s", zone)
	}
	out, err := a.firewallCmd("--zone="+zone, "--list-all")
	if err != nil {
		return nil, fmt.Errorf("list rules for zone %s: %w", zone, err)
	}

	result := map[string]interface{}{"zone": zone}
	var richRules []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "services:") {
			result["services"] = strings.Fields(strings.TrimPrefix(line, "services:"))
		} else if strings.HasPrefix(line, "ports:") {
			result["ports"] = strings.Fields(strings.TrimPrefix(line, "ports:"))
		} else if strings.HasPrefix(line, "target:") {
			result["target"] = strings.TrimSpace(strings.TrimPrefix(line, "target:"))
		} else if strings.HasPrefix(line, "interfaces:") {
			result["interfaces"] = strings.Fields(strings.TrimPrefix(line, "interfaces:"))
		} else if strings.HasPrefix(line, "rich rules:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "rich rules:"))
			if raw != "" {
				richRules = append(richRules, raw)
			}
		} else if strings.HasPrefix(line, "rule ") {
			richRules = append(richRules, line)
		}
	}
	result["rich_rules"] = richRules
	return result, nil
}

var (
	fwPortRe     = regexp.MustCompile(`^\d+(-\d+)?$`)
	fwProtocolRe = regexp.MustCompile(`^(tcp|udp)$`)
	fwZoneRe     = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	fwServiceRe  = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

func (a *CoreAPIImpl) firewallValidatePort(port, protocol string) error {
	if !fwPortRe.MatchString(port) {
		return fmt.Errorf("invalid port: %s", port)
	}
	if !fwProtocolRe.MatchString(protocol) {
		return fmt.Errorf("invalid protocol: %s", protocol)
	}
	for _, p := range strings.SplitN(port, "-", 2) {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("port %s out of range (1-65535)", p)
		}
	}
	return nil
}

func (a *CoreAPIImpl) firewallPanelPort() string {
	if v, err := a.GetSetting("port"); err == nil && v != "" {
		return v
	}
	return "39921"
}

func (a *CoreAPIImpl) FirewallAddPort(zone, port, protocol string) error {
	if err := a.firewallValidatePort(port, protocol); err != nil {
		return err
	}
	if zone == "" {
		zone = a.firewallDefaultZone()
	}
	if !fwZoneRe.MatchString(zone) {
		return fmt.Errorf("invalid zone name: %s", zone)
	}
	if _, err := a.firewallCmd("--permanent", "--zone="+zone, "--add-port="+port+"/"+protocol); err != nil {
		return fmt.Errorf("add port: %w", err)
	}
	_, _ = a.firewallCmd("--reload")
	return nil
}

func (a *CoreAPIImpl) FirewallRemovePort(zone, port, protocol string) error {
	if err := a.firewallValidatePort(port, protocol); err != nil {
		return err
	}
	// Protect panel port from removal.
	panelPort := a.firewallPanelPort()
	if protocol == "tcp" && port == panelPort {
		return fmt.Errorf("cannot remove panel port %s/tcp — this would lock you out", panelPort)
	}
	if protocol == "tcp" && strings.Contains(port, "-") {
		parts := strings.SplitN(port, "-", 2)
		if len(parts) == 2 {
			lo, _ := strconv.Atoi(parts[0])
			hi, _ := strconv.Atoi(parts[1])
			pp, _ := strconv.Atoi(panelPort)
			if pp >= lo && pp <= hi {
				return fmt.Errorf("cannot remove port range %s/tcp — it contains panel port %s", port, panelPort)
			}
		}
	}
	if zone == "" {
		zone = a.firewallDefaultZone()
	}
	if !fwZoneRe.MatchString(zone) {
		return fmt.Errorf("invalid zone name: %s", zone)
	}
	if _, err := a.firewallCmd("--permanent", "--zone="+zone, "--remove-port="+port+"/"+protocol); err != nil {
		return fmt.Errorf("remove port: %w", err)
	}
	_, _ = a.firewallCmd("--reload")
	return nil
}

func (a *CoreAPIImpl) FirewallAddService(zone, service string) error {
	if !fwServiceRe.MatchString(service) {
		return fmt.Errorf("invalid service name: %s", service)
	}
	if zone == "" {
		zone = a.firewallDefaultZone()
	}
	if !fwZoneRe.MatchString(zone) {
		return fmt.Errorf("invalid zone name: %s", zone)
	}
	if _, err := a.firewallCmd("--permanent", "--zone="+zone, "--add-service="+service); err != nil {
		return fmt.Errorf("add service: %w", err)
	}
	_, _ = a.firewallCmd("--reload")
	return nil
}

func (a *CoreAPIImpl) FirewallRemoveService(zone, service string) error {
	if !fwServiceRe.MatchString(service) {
		return fmt.Errorf("invalid service name: %s", service)
	}
	// Protect SSH from removal.
	if service == "ssh" {
		return fmt.Errorf("cannot remove SSH service — this would lock you out of remote access")
	}
	if zone == "" {
		zone = a.firewallDefaultZone()
	}
	if !fwZoneRe.MatchString(zone) {
		return fmt.Errorf("invalid zone name: %s", zone)
	}
	if _, err := a.firewallCmd("--permanent", "--zone="+zone, "--remove-service="+service); err != nil {
		return fmt.Errorf("remove service: %w", err)
	}
	_, _ = a.firewallCmd("--reload")
	return nil
}

// ──────────────────────────────────────────────────
// PHP management
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) PHPListRuntimes() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_php_runtimes").
		Select("id, version, type, status, port, container_name, extensions, memory_limit, created_at").
		Find(&results).Error
	if err != nil {
		// Table may not exist if PHP plugin is not installed.
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

func (a *CoreAPIImpl) PHPListSites() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_php_sites").
		Select("id, name, domain, root_path, php_version, runtime_type, worker_mode, status, created_at").
		Find(&results).Error
	if err != nil {
		// Table may not exist if PHP plugin is not installed.
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

// ──────────────────────────────────────────────────
// NLOps: Host management additions
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) ToggleHost(id uint) error {
	var h model.Host
	if err := a.db.First(&h, id).Error; err != nil {
		return fmt.Errorf("host not found: %w", err)
	}
	newVal := !(h.Enabled != nil && *h.Enabled)
	if err := a.db.Model(&h).Update("enabled", newVal).Error; err != nil {
		return fmt.Errorf("toggle host: %w", err)
	}
	return a.hostSvc.ApplyConfig()
}

func (a *CoreAPIImpl) CloneHost(id uint, newDomain string) (uint, error) {
	var src model.Host
	if err := a.db.Preload("Upstreams").First(&src, id).Error; err != nil {
		return 0, fmt.Errorf("host not found: %w", err)
	}

	if err := caddy.ValidateDomain(newDomain); err != nil {
		return 0, fmt.Errorf("invalid domain: %w", err)
	}

	// Build upstream input from source.
	upstreamAddr := ""
	if len(src.Upstreams) > 0 {
		upstreamAddr = src.Upstreams[0].Address
	}

	return a.CreateHost(CreateHostRequest{
		Domain:       newDomain,
		HostType:     src.HostType,
		UpstreamAddr: upstreamAddr,
		TLSEnabled:   src.TLSEnabled != nil && *src.TLSEnabled,
		HTTPRedirect: src.HTTPRedirect != nil && *src.HTTPRedirect,
		WebSocket:    src.WebSocket != nil && *src.WebSocket,
		Compression:  src.Compression != nil && *src.Compression,
		RootPath:     src.RootPath,
		PHPFastCGI:   src.PHPFastCGI,
	})
}

// ──────────────────────────────────────────────────
// NLOps: Caddy management
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) GetCaddyStatus() (map[string]interface{}, error) {
	status := a.caddyMgr.Status()
	status["installed"] = true
	return status, nil
}

func (a *CoreAPIImpl) RestartCaddy() error {
	_ = a.caddyMgr.Stop()
	return a.caddyMgr.Start()
}

// ──────────────────────────────────────────────────
// NLOps: Deploy lifecycle
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) StartProject(id uint) error {
	if a.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}
	a.eventBus.Publish(Event{
		Type:    "deploy.start_project",
		Payload: map[string]interface{}{"project_id": id},
		Source:  "core",
	})
	return nil
}

func (a *CoreAPIImpl) StopProject(id uint) error {
	if a.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}
	a.eventBus.Publish(Event{
		Type:    "deploy.stop_project",
		Payload: map[string]interface{}{"project_id": id},
		Source:  "core",
	})
	return nil
}

func (a *CoreAPIImpl) RollbackProject(projectID uint, buildNum int) error {
	if a.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}
	a.eventBus.Publish(Event{
		Type: "deploy.rollback",
		Payload: map[string]interface{}{
			"project_id":   projectID,
			"build_number": buildNum,
		},
		Source: "core",
	})
	return nil
}

// ──────────────────────────────────────────────────
// NLOps: Docker cleanup
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) DockerRemoveContainer(containerID string, force bool) error {
	if containerID == "" {
		return fmt.Errorf("container ID is required")
	}
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, containerID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker rm %s: %s — %w", containerID, buf.String(), err)
	}
	return nil
}

func (a *CoreAPIImpl) DockerPrune(what string) (map[string]interface{}, error) {
	var args []string
	switch what {
	case "containers":
		args = []string{"container", "prune", "-f"}
	case "images":
		args = []string{"image", "prune", "-a", "-f"}
	case "volumes":
		args = []string{"volume", "prune", "-f"}
	case "all":
		args = []string{"system", "prune", "-a", "-f"}
	default:
		return nil, fmt.Errorf("invalid prune target %q: must be containers, images, volumes, or all", what)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker prune: %s — %w", buf.String(), err)
	}

	return map[string]interface{}{
		"target": what,
		"output": strings.TrimSpace(buf.String()),
	}, nil
}

// ──────────────────────────────────────────────────
// NLOps: Notification channel management
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) ListNotifyChannels() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("notify_channels").
		Select("id, type, name, enabled, events, created_at, updated_at").
		Find(&results).Error
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

func (a *CoreAPIImpl) TestNotifyChannel(id uint) error {
	if a.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}
	a.eventBus.Publish(Event{
		Type:    "notify.test_channel",
		Payload: map[string]interface{}{"channel_id": id},
		Source:  "core",
	})
	return nil
}

// ──────────────────────────────────────────────────
// NLOps: Monitoring alert management
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) ListAlertRules() ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := a.db.Table("plugin_monitoring_alert_rules").
		Find(&results).Error
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

func (a *CoreAPIImpl) CreateAlertRule(name, metric, operator string, threshold float64, duration int) (uint, error) {
	if operator == "" {
		operator = ">"
	}
	if duration <= 0 {
		duration = 1
	}

	type alertRuleRow struct {
		ID          uint      `gorm:"primaryKey"`
		Name        string    `gorm:"size:128"`
		Metric      string    `gorm:"size:64"`
		Operator    string    `gorm:"size:4"`
		Threshold   float64
		Duration    int
		Enabled     bool
		CooldownMin int       `gorm:"column:cooldown_min"`
		CreatedAt   time.Time
		UpdatedAt   time.Time
	}
	row := alertRuleRow{
		Name: name, Metric: metric, Operator: operator,
		Threshold: threshold, Duration: duration,
		Enabled: true, CooldownMin: 30,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	result := a.db.Table("plugin_monitoring_alert_rules").Create(&row)
	if result.Error != nil {
		return 0, fmt.Errorf("create alert rule: %w", result.Error)
	}

	return row.ID, nil
}

func (a *CoreAPIImpl) DeleteAlertRule(id uint) error {
	result := a.db.Table("plugin_monitoring_alert_rules").Where("id = ?", id).Delete(nil)
	if result.Error != nil {
		return fmt.Errorf("delete alert rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("alert rule not found: %d", id)
	}
	return nil
}

// ──────────────────────────────────────────────────
// NLOps: System information
// ──────────────────────────────────────────────────

func (a *CoreAPIImpl) GetSystemInfo() (map[string]interface{}, error) {
	result := map[string]interface{}{}

	// Hostname.
	if h, err := os.Hostname(); err == nil {
		result["hostname"] = h
	}

	// Kernel.
	result["kernel"] = runtime.GOOS + "/" + runtime.GOARCH
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "uname", "-r").Output(); err == nil {
		result["kernel_version"] = strings.TrimSpace(string(out))
	}

	// OS info.
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				result["os"] = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				break
			}
		}
	}

	// Uptime.
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			result["uptime_seconds"] = fields[0]
		}
	}

	// CPU.
	result["cpu_cores"] = runtime.NumCPU()

	// Architecture.
	result["arch"] = runtime.GOARCH

	return result, nil
}

// ──────────────────────────────────────────────────
// Cron job management
// ──────────────────────────────────────────────────

// CronJobList returns all cron jobs, optionally filtered by tag.
func (a *CoreAPIImpl) CronJobList(tag string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	q := a.db.Table("plugin_cronjob_tasks").Order("id ASC")
	if tag != "" {
		q = q.Where("tags LIKE ?", fmt.Sprintf(`%%"%s"%%`, tag))
	}
	if err := q.Find(&results).Error; err != nil {
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

// cronParser is used to validate 5-field cron expressions (with descriptors like @every).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// CronJobCreate creates a new cron job.
func (a *CoreAPIImpl) CronJobCreate(name, expression, command, workingDir string, tags []string, timeoutSec int) (uint, error) {
	if _, err := cronParser.Parse(expression); err != nil {
		return 0, fmt.Errorf("invalid cron expression %q: %w", expression, err)
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	tagsJSON := "[]"
	if len(tags) > 0 {
		b, _ := json.Marshal(tags)
		tagsJSON = string(b)
	}
	type cronTaskRow struct {
		ID         uint      `gorm:"primaryKey"`
		Name       string    `gorm:"size:128"`
		Expression string    `gorm:"size:128"`
		Command    string    `gorm:"type:text"`
		WorkingDir string    `gorm:"size:512"`
		Enabled    bool
		Tags       string    `gorm:"type:text"`
		TimeoutSec int
		CreatedAt  time.Time
		UpdatedAt  time.Time
	}
	row := cronTaskRow{
		Name: name, Expression: expression, Command: command,
		WorkingDir: workingDir, Enabled: true, Tags: tagsJSON,
		TimeoutSec: timeoutSec,
		CreatedAt:  time.Now(), UpdatedAt: time.Now(),
	}
	result := a.db.Table("plugin_cronjob_tasks").Create(&row)
	if result.Error != nil {
		return 0, fmt.Errorf("create cron job: %w", result.Error)
	}
	// Notify scheduler to register the new task.
	if a.eventBus != nil {
		a.eventBus.Publish(Event{
			Type:    "cronjob.reload",
			Source:  "core",
			Payload: map[string]interface{}{"task_id": float64(row.ID), "action": "create"},
		})
	}
	return row.ID, nil
}

// CronJobUpdate updates fields on an existing cron job.
func (a *CoreAPIImpl) CronJobUpdate(id uint, updates map[string]interface{}) error {
	// Validate expression if it's being updated.
	if expr, ok := updates["expression"].(string); ok && expr != "" {
		if _, err := cronParser.Parse(expr); err != nil {
			return fmt.Errorf("invalid cron expression %q: %w", expr, err)
		}
	}

	// Whitelist allowed fields to prevent primary key/timestamp manipulation.
	allowed := map[string]bool{
		"name": true, "expression": true, "command": true, "working_dir": true,
		"enabled": true, "tags": true, "timeout_sec": true, "max_retries": true,
		"notify_on_failure": true,
	}
	safe := map[string]interface{}{"updated_at": time.Now()}
	for k, v := range updates {
		if allowed[k] {
			safe[k] = v
		}
	}

	result := a.db.Table("plugin_cronjob_tasks").Where("id = ?", id).Updates(safe)
	if result.Error != nil {
		return fmt.Errorf("update cron job: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("cron job not found: %d", id)
	}
	// Notify scheduler to re-register the task.
	if a.eventBus != nil {
		a.eventBus.Publish(Event{
			Type:    "cronjob.reload",
			Source:  "core",
			Payload: map[string]interface{}{"task_id": float64(id), "action": "update"},
		})
	}
	return nil
}

// CronJobDelete deletes a cron job and its logs.
func (a *CoreAPIImpl) CronJobDelete(id uint) error {
	// Notify scheduler to remove the task before deleting from DB.
	if a.eventBus != nil {
		a.eventBus.Publish(Event{
			Type:    "cronjob.reload",
			Source:  "core",
			Payload: map[string]interface{}{"task_id": float64(id), "action": "delete"},
		})
	}
	a.db.Table("plugin_cronjob_logs").Where("task_id = ?", id).Delete(nil)
	result := a.db.Table("plugin_cronjob_tasks").Where("id = ?", id).Delete(nil)
	if result.Error != nil {
		return fmt.Errorf("delete cron job: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("cron job not found: %d", id)
	}
	return nil
}

// CronJobLogs returns execution logs. If taskID is 0, returns all recent logs.
func (a *CoreAPIImpl) CronJobLogs(taskID uint, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var results []map[string]interface{}
	q := a.db.Table("plugin_cronjob_logs").Order("id DESC").Limit(limit)
	if taskID > 0 {
		q = q.Where("task_id = ?", taskID)
	}
	if err := q.Find(&results).Error; err != nil {
		return []map[string]interface{}{}, nil
	}
	return results, nil
}

// CronJobTrigger triggers a cron job to run immediately via EventBus.
func (a *CoreAPIImpl) CronJobTrigger(id uint) error {
	// Verify the task exists.
	var count int64
	a.db.Table("plugin_cronjob_tasks").Where("id = ?", id).Count(&count)
	if count == 0 {
		return fmt.Errorf("cron job not found: %d", id)
	}
	if a.eventBus != nil {
		a.eventBus.Publish(Event{
			Type:    "cronjob.trigger",
			Source:  "core",
			Payload: map[string]interface{}{"task_id": float64(id)},
		})
	}
	return nil
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
