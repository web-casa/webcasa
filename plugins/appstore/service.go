package appstore

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// Service implements the core business logic for the App Store.
type Service struct {
	db       *gorm.DB
	sources  *SourceManager
	coreAPI  pluginpkg.CoreAPI
	eventBus *pluginpkg.EventBus
	logger   *slog.Logger
	dataDir  string // data/plugins/appstore/
}

// NewService creates an App Store service.
func NewService(db *gorm.DB, sources *SourceManager, coreAPI pluginpkg.CoreAPI, eventBus *pluginpkg.EventBus, logger *slog.Logger, dataDir string) *Service {
	return &Service{
		db:       db,
		sources:  sources,
		coreAPI:  coreAPI,
		eventBus: eventBus,
		logger:   logger,
		dataDir:  dataDir,
	}
}

// ── App Catalog ──

// AppListResponse is the paginated response for app listing.
type AppListResponse struct {
	Apps       []AppDefinition `json:"apps"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	Categories []string        `json:"categories,omitempty"`
}

// ListApps returns available apps with filtering, search, and pagination.
func (s *Service) ListApps(category, search string, page, pageSize int) (*AppListResponse, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 24
	}

	query := s.db.Model(&AppDefinition{}).Where("available = ?", true)

	if search != "" {
		like := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(short_desc) LIKE ? OR LOWER(app_id) LIKE ?", like, like, like)
	}

	if category != "" {
		// Categories stored as JSON array string, use LIKE for SQLite
		query = query.Where("categories LIKE ?", "%\""+category+"\"%")
	}

	var total int64
	query.Count(&total)

	var apps []AppDefinition
	offset := (page - 1) * pageSize
	if err := query.Order("name ASC").Offset(offset).Limit(pageSize).Find(&apps).Error; err != nil {
		return nil, err
	}

	return &AppListResponse{
		Apps:     apps,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// GetApp returns a single app definition by ID (database PK).
func (s *Service) GetApp(id uint) (*AppDefinition, error) {
	var app AppDefinition
	if err := s.db.First(&app, id).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

// GetAppByAppID returns an app by its app_id string.
func (s *Service) GetAppByAppID(appID string) (*AppDefinition, error) {
	var app AppDefinition
	if err := s.db.Where("app_id = ? AND available = ?", appID, true).First(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

// GetCategories returns all unique categories across apps.
func (s *Service) GetCategories() []string {
	var apps []AppDefinition
	s.db.Select("categories").Where("available = ?", true).Find(&apps)

	seen := make(map[string]bool)
	var result []string
	for _, a := range apps {
		var cats []string
		if err := json.Unmarshal([]byte(a.Categories), &cats); err != nil {
			continue
		}
		for _, c := range cats {
			if !seen[c] {
				seen[c] = true
				result = append(result, c)
			}
		}
	}
	return result
}

// ── Installation ──

// InstallAppRequest is the input for installing an app.
type InstallAppRequest struct {
	AppID      string            `json:"app_id" binding:"required"`
	Name       string            `json:"name" binding:"required"`
	FormValues map[string]string `json:"form_values"`
	Domain     string            `json:"domain,omitempty"`
	AutoUpdate bool              `json:"auto_update"`
}

// InstallApp renders compose, creates Docker Stack, optionally creates host.
func (s *Service) InstallApp(req *InstallAppRequest) (*InstalledApp, error) {
	// 1. Find app definition
	app, err := s.GetAppByAppID(req.AppID)
	if err != nil {
		return nil, fmt.Errorf("app %q not found", req.AppID)
	}

	// 2. Parse form fields
	var fields []FormField
	if app.FormFields != "" {
		if err := json.Unmarshal([]byte(app.FormFields), &fields); err != nil {
			return nil, fmt.Errorf("parse form fields: %w", err)
		}
	}

	// 3. Prepare form values
	if req.FormValues == nil {
		req.FormValues = make(map[string]string)
	}
	FillDefaults(fields, req.FormValues)
	FillRandomFields(fields, req.FormValues)

	// 4. Validate
	if err := ValidateFormValues(fields, req.FormValues); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	// 5. Create InstalledApp record first to get ID
	stackName := sanitizeStackName(req.Name)
	// Extract url_suffix from config.json
	var appConfig AppConfig
	if app.ConfigJSON != "" {
		json.Unmarshal([]byte(app.ConfigJSON), &appConfig)
	}

	installed := &InstalledApp{
		AppID:      req.AppID,
		AppName:    app.Name,
		Name:       req.Name,
		StackName:  stackName,
		Domain:     req.Domain,
		Port:       app.Port,
		Version:    app.Version,
		Status:     "installing",
		AutoUpdate: req.AutoUpdate,
		UrlSuffix:  appConfig.UrlSuffix,
	}

	formJSON, _ := json.Marshal(req.FormValues)
	installed.FormValues = string(formJSON)

	if err := s.db.Create(installed).Error; err != nil {
		return nil, fmt.Errorf("create record: %w", err)
	}

	// 6. Prepare compose directory
	composeDir := filepath.Join(s.dataDir, "installed", fmt.Sprintf("%d", installed.ID))
	if err := os.MkdirAll(composeDir, 0755); err != nil {
		s.setStatus(installed.ID, "error")
		return nil, fmt.Errorf("create dir: %w", err)
	}
	installed.ComposeDir = composeDir
	s.db.Model(installed).Update("compose_dir", composeDir)

	// 7. Built-in variables (including Runtipi-compatible ones)
	protocol := "https"
	exposed := "false"
	if req.Domain != "" {
		exposed = "true"
	}
	// Ensure shared media directory exists for ROOT_FOLDER_HOST
	os.MkdirAll(filepath.Join(s.dataDir, "media"), 0755)

	builtins := map[string]string{
		"APP_ID":            req.AppID,
		"APP_PORT":          fmt.Sprintf("%d", app.Port),
		"APP_DATA_DIR":      filepath.Join(composeDir, "data"),
		"APP_DOMAIN":        req.Domain,
		"ROOT_FOLDER_HOST":  s.dataDir,
		"APP_EXPOSED":       exposed,
		"APP_PROTOCOL":      protocol,
		"APP_HOST":          req.Domain,
		"LOCAL_DOMAIN":      req.Domain,
		"TZ":                getSystemTimezone(),
		"NETWORK_INTERFACE": "127.0.0.1",
		"DNS_IP":            "1.1.1.1",
		"INTERNAL_IP":       getLocalIP(),
	}

	// 8. Render compose and env
	rendered := SanitizeCompose(RenderCompose(app.ComposeFile, req.FormValues, builtins))
	envContent := RenderEnvFile(req.FormValues, builtins)

	// Write files
	if err := os.WriteFile(filepath.Join(composeDir, "docker-compose.yml"), []byte(rendered), 0644); err != nil {
		s.setStatus(installed.ID, "error")
		return nil, fmt.Errorf("write compose: %w", err)
	}
	if err := os.WriteFile(filepath.Join(composeDir, ".env"), []byte(envContent), 0644); err != nil {
		s.setStatus(installed.ID, "error")
		return nil, fmt.Errorf("write env: %w", err)
	}

	// Create data dir
	os.MkdirAll(filepath.Join(composeDir, "data"), 0755)

	// 9. Also create a record in plugin_docker_stacks so Docker Overview shows it
	s.createDockerStackRecord(stackName, rendered, envContent, composeDir)

	// 10. docker compose up
	if err := s.runCompose(composeDir, stackName, "up", "-d", "--remove-orphans"); err != nil {
		s.setStatus(installed.ID, "error")
		return nil, fmt.Errorf("compose up: %w", err)
	}

	// 11. Optionally create reverse proxy
	if req.Domain != "" && app.Exposable && app.Port > 0 {
		hostID, err := s.coreAPI.CreateHost(pluginpkg.CreateHostRequest{
			Domain:       req.Domain,
			UpstreamAddr: fmt.Sprintf("localhost:%d", app.Port),
			TLSEnabled:   true,
			HTTPRedirect: true,
			WebSocket:    true,
		})
		if err != nil {
			s.logger.Error("create host failed", "domain", req.Domain, "err", err)
		} else {
			s.db.Model(installed).Update("host_id", hostID)
			installed.HostID = hostID
		}
	}

	// 12. Update status
	s.setStatus(installed.ID, "running")
	installed.Status = "running"

	// 13. Publish event
	if s.eventBus != nil {
		s.eventBus.Publish(pluginpkg.Event{
			Type:   "appstore.app.installed",
			Source: "appstore",
			Payload: map[string]interface{}{
				"app_id": req.AppID,
				"name":   req.Name,
			},
		})
	}

	return installed, nil
}

// UninstallApp stops and removes an installed app.
func (s *Service) UninstallApp(id uint, removeData bool) error {
	var installed InstalledApp
	if err := s.db.First(&installed, id).Error; err != nil {
		return err
	}

	// Stop and remove compose stack
	if installed.ComposeDir != "" {
		_ = s.runCompose(installed.ComposeDir, installed.StackName, "down", "--remove-orphans")
	}

	// Remove Docker Stack record
	s.deleteDockerStackRecord(installed.StackName)

	// Remove reverse proxy host
	if installed.HostID > 0 {
		if err := s.coreAPI.DeleteHost(installed.HostID); err != nil {
			s.logger.Error("delete host failed", "host_id", installed.HostID, "err", err)
		}
	}

	// Remove data if requested
	if removeData && installed.ComposeDir != "" {
		os.RemoveAll(installed.ComposeDir)
	}

	// Delete record
	if err := s.db.Delete(&InstalledApp{}, id).Error; err != nil {
		return err
	}

	if s.eventBus != nil {
		s.eventBus.Publish(pluginpkg.Event{
			Type:   "appstore.app.uninstalled",
			Source: "appstore",
			Payload: map[string]interface{}{
				"app_id": installed.AppID,
				"name":   installed.Name,
			},
		})
	}

	return nil
}

// ListInstalled returns all installed apps.
func (s *Service) ListInstalled() ([]InstalledApp, error) {
	var apps []InstalledApp
	if err := s.db.Order("id DESC").Find(&apps).Error; err != nil {
		return nil, err
	}

	// Refresh status from Docker
	for i := range apps {
		if apps[i].ComposeDir != "" {
			apps[i].Status = s.resolveAppStatus(apps[i].ComposeDir, apps[i].StackName)
		}
	}

	return apps, nil
}

// GetInstalled returns a single installed app.
func (s *Service) GetInstalled(id uint) (*InstalledApp, error) {
	var app InstalledApp
	if err := s.db.First(&app, id).Error; err != nil {
		return nil, err
	}
	if app.ComposeDir != "" {
		app.Status = s.resolveAppStatus(app.ComposeDir, app.StackName)
	}
	return &app, nil
}

// UpdateDomain changes the domain for an installed app, updating Caddy reverse proxy and .env.
func (s *Service) UpdateDomain(id uint, domain string) error {
	var installed InstalledApp
	if err := s.db.First(&installed, id).Error; err != nil {
		return err
	}

	// Remove old reverse proxy host
	if installed.HostID > 0 {
		if err := s.coreAPI.DeleteHost(installed.HostID); err != nil {
			s.logger.Error("delete old host failed", "host_id", installed.HostID, "err", err)
		}
		installed.HostID = 0
	}

	// Create new reverse proxy host if domain is provided
	if domain != "" && installed.Port > 0 {
		hostID, err := s.coreAPI.CreateHost(pluginpkg.CreateHostRequest{
			Domain:       domain,
			UpstreamAddr: fmt.Sprintf("localhost:%d", installed.Port),
			TLSEnabled:   true,
			HTTPRedirect: true,
			WebSocket:    true,
		})
		if err != nil {
			s.logger.Error("create host failed", "domain", domain, "err", err)
		} else {
			installed.HostID = hostID
		}
	}

	// Update database record
	installed.Domain = domain
	if err := s.db.Model(&installed).Updates(map[string]interface{}{
		"domain":  domain,
		"host_id": installed.HostID,
	}).Error; err != nil {
		return err
	}

	// Update .env file with new domain values
	if installed.ComposeDir != "" {
		envPath := filepath.Join(installed.ComposeDir, ".env")
		if data, err := os.ReadFile(envPath); err == nil {
			lines := strings.Split(string(data), "\n")
			domainVars := map[string]bool{"APP_DOMAIN": true, "APP_HOST": true, "LOCAL_DOMAIN": true}
			var out []string
			for _, line := range lines {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 && domainVars[parts[0]] {
					out = append(out, parts[0]+"="+domain)
				} else {
					out = append(out, line)
				}
			}
			os.WriteFile(envPath, []byte(strings.Join(out, "\n")), 0644)
		}

		// Restart compose to pick up new .env
		_ = s.runCompose(installed.ComposeDir, installed.StackName, "up", "-d", "--remove-orphans")
	}

	return nil
}

// StartApp starts an installed app.
func (s *Service) StartApp(id uint) error {
	var app InstalledApp
	if err := s.db.First(&app, id).Error; err != nil {
		return err
	}
	if err := s.runCompose(app.ComposeDir, app.StackName, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	return s.db.Model(&app).Update("status", "running").Error
}

// StopApp stops an installed app.
func (s *Service) StopApp(id uint) error {
	var app InstalledApp
	if err := s.db.First(&app, id).Error; err != nil {
		return err
	}
	if err := s.runCompose(app.ComposeDir, app.StackName, "down"); err != nil {
		return err
	}
	return s.db.Model(&app).Update("status", "stopped").Error
}

// ── Updates ──

// AppUpdate represents an available update.
type AppUpdate struct {
	InstalledID      uint   `json:"installed_id"`
	AppID            string `json:"app_id"`
	Name             string `json:"name"`
	CurrentVersion   string `json:"current_version"`
	AvailableVersion string `json:"available_version"`
}

// CheckUpdates compares installed versions with catalog versions.
func (s *Service) CheckUpdates() ([]AppUpdate, error) {
	var installed []InstalledApp
	if err := s.db.Find(&installed).Error; err != nil {
		return nil, err
	}

	var updates []AppUpdate
	for _, inst := range installed {
		var app AppDefinition
		if err := s.db.Where("app_id = ?", inst.AppID).First(&app).Error; err != nil {
			continue
		}
		if app.Version != "" && inst.Version != "" && app.Version != inst.Version {
			updates = append(updates, AppUpdate{
				InstalledID:      inst.ID,
				AppID:            inst.AppID,
				Name:             inst.Name,
				CurrentVersion:   inst.Version,
				AvailableVersion: app.Version,
			})
		}
	}

	return updates, nil
}

// UpdateApp updates an installed app to the latest version.
func (s *Service) UpdateApp(id uint) error {
	var installed InstalledApp
	if err := s.db.First(&installed, id).Error; err != nil {
		return err
	}

	var app AppDefinition
	if err := s.db.Where("app_id = ?", installed.AppID).First(&app).Error; err != nil {
		return fmt.Errorf("app definition not found: %w", err)
	}

	s.setStatus(id, "installing")

	// Parse stored form values
	var formValues map[string]string
	if installed.FormValues != "" {
		json.Unmarshal([]byte(installed.FormValues), &formValues)
	}
	if formValues == nil {
		formValues = make(map[string]string)
	}

	// Re-render with new compose
	exposed := "false"
	if installed.Domain != "" {
		exposed = "true"
	}
	builtins := map[string]string{
		"APP_ID":            installed.AppID,
		"APP_PORT":          fmt.Sprintf("%d", app.Port),
		"APP_DATA_DIR":      filepath.Join(installed.ComposeDir, "data"),
		"APP_DOMAIN":        installed.Domain,
		"ROOT_FOLDER_HOST":  s.dataDir,
		"APP_EXPOSED":       exposed,
		"APP_PROTOCOL":      "https",
		"APP_HOST":          installed.Domain,
		"LOCAL_DOMAIN":      installed.Domain,
		"TZ":                getSystemTimezone(),
		"NETWORK_INTERFACE": "127.0.0.1",
		"DNS_IP":            "1.1.1.1",
		"INTERNAL_IP":       getLocalIP(),
	}

	rendered := SanitizeCompose(RenderCompose(app.ComposeFile, formValues, builtins))
	envContent := RenderEnvFile(formValues, builtins)

	// Write updated files
	os.WriteFile(filepath.Join(installed.ComposeDir, "docker-compose.yml"), []byte(rendered), 0644)
	os.WriteFile(filepath.Join(installed.ComposeDir, ".env"), []byte(envContent), 0644)

	// Pull new images and recreate
	_ = s.runCompose(installed.ComposeDir, installed.StackName, "pull")
	if err := s.runCompose(installed.ComposeDir, installed.StackName, "up", "-d", "--remove-orphans"); err != nil {
		s.setStatus(id, "error")
		return fmt.Errorf("compose up: %w", err)
	}

	// Update version
	s.db.Model(&installed).Updates(map[string]interface{}{
		"version": app.Version,
		"status":  "running",
	})

	return nil
}

// ── Helpers ──

func (s *Service) setStatus(id uint, status string) {
	s.db.Model(&InstalledApp{}).Where("id = ?", id).Update("status", status)
}

// runCompose executes docker compose with the given args.
func (s *Service) runCompose(dir, projectName string, args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+projectName)

	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(output))
		s.logger.Error("docker compose failed",
			"dir", dir,
			"args", strings.Join(args, " "),
			"output", outStr,
			"err", err,
		)
		if outStr != "" {
			return fmt.Errorf("docker compose %s: %s", args[0], outStr)
		}
		return fmt.Errorf("docker compose %s: %v", args[0], err)
	}
	return nil
}

// resolveAppStatus checks if compose services are running.
func (s *Service) resolveAppStatus(dir, projectName string) string {
	cmd := exec.Command("docker", "compose", "ps", "--status", "running", "-q")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+projectName)

	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return "stopped"
	}
	return "running"
}

// createDockerStackRecord inserts a record into plugin_docker_stacks
// so the Docker Overview page shows this stack.
func (s *Service) createDockerStackRecord(name, composeFile, envFile, dataDir string) {
	type DockerStack struct {
		Name        string `gorm:"uniqueIndex;not null;size:128"`
		Description string `gorm:"size:512"`
		ComposeFile string `gorm:"type:text;not null"`
		EnvFile     string `gorm:"type:text"`
		Status      string `gorm:"size:16;default:stopped"`
		DataDir     string `gorm:"size:512"`
	}

	// Use raw table name to avoid importing docker package
	s.db.Table("plugin_docker_stacks").Create(map[string]interface{}{
		"name":         name,
		"description":  "[App Store] Managed by App Store plugin",
		"compose_file": composeFile,
		"env_file":     envFile,
		"status":       "running",
		"data_dir":     dataDir,
		"managed_by":   "appstore",
	})
}

// deleteDockerStackRecord removes the Docker Stack record.
func (s *Service) deleteDockerStackRecord(name string) {
	s.db.Table("plugin_docker_stacks").Where("name = ?", name).Delete(map[string]interface{}{})
}

// sanitizeStackName converts a name to a Docker Compose-safe project name.
func sanitizeStackName(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-_")
	if name == "" {
		name = "app"
	}
	return "appstore-" + name
}
