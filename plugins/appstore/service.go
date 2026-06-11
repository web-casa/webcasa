package appstore

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/web-casa/webcasa/internal/caddy"
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
// lang is the user's language code (e.g. "zh"); when non-empty and not "en",
// search also covers the i18n_json field for localized matches.
func (s *Service) ListApps(category, search, lang string, page, pageSize int) (*AppListResponse, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 24
	}

	query := s.db.Model(&AppDefinition{}).Where("available = ? AND deprecated = ?", true, false)

	if search != "" {
		like := "%" + strings.ToLower(search) + "%"
		if lang != "" && lang != "en" {
			query = query.Where("LOWER(name) LIKE ? OR LOWER(short_desc) LIKE ? OR LOWER(app_id) LIKE ? OR LOWER(i18n_json) LIKE ?", like, like, like, like)
		} else {
			query = query.Where("LOWER(name) LIKE ? OR LOWER(short_desc) LIKE ? OR LOWER(app_id) LIKE ?", like, like, like)
		}
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
// Deprecated apps are excluded to prevent new installations.
func (s *Service) GetAppByAppID(appID string) (*AppDefinition, error) {
	var app AppDefinition
	if err := s.db.Where("app_id = ? AND available = ? AND deprecated = ?", appID, true, false).First(&app).Error; err != nil {
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
//
// ── App Store supply-chain trust model ──
//
// Sources are UNSIGNED. App definitions (the docker-compose.yml and the
// container images it references) are pulled from admin-added Git repos with
// NO signature verification, NO commit pinning, and the upstream compose files
// are NOT digest-pinned. We therefore treat every source as untrusted and
// apply two backend-enforced controls at install time:
//
//  1. Explicit acknowledgement — installing from an unsigned source REQUIRES
//     InstallAppRequest.AcknowledgeUnsigned == true. Otherwise InstallApp
//     returns an *UnsignedSourceError listing exactly which images/compose
//     would run, so the risk is an audited, opt-in choice rather than silent.
//
//  2. Image digest pinning — before `compose up`, every service image is
//     pulled and resolved to an immutable name@sha256:<digest> reference, and
//     the rendered compose is rewritten to pin to that digest (already-pinned
//     refs are left as-is). If the registry is unreachable we FAIL the install
//     rather than run an unpinned/floating image. The resolved digests are
//     persisted on the InstalledApp so registry-side tag drift is detectable.
//
// A stronger model — signed app manifests (e.g. cosign/in-toto over the
// compose + pinned digests, verified before install) — is the better long-term
// design but is intentionally NOT implemented here. This is the minimal,
// operator-chosen model: digest pinning + unsigned acknowledgement, no signing
// system.

// InstallAppRequest is the input for installing an app.
type InstallAppRequest struct {
	AppID      string            `json:"app_id" binding:"required"`
	Name       string            `json:"name" binding:"required"`
	FormValues map[string]string `json:"form_values"`
	Domain     string            `json:"domain,omitempty"`
	AutoUpdate bool              `json:"auto_update"`
	// AcknowledgeUnsigned must be true to install from an unsigned (untrusted)
	// source. When false/absent, InstallApp returns *UnsignedSourceError so the
	// UI can surface the warning and require explicit confirmation.
	AcknowledgeUnsigned bool `json:"acknowledge_unsigned"`
}

// UnsignedSourceError is returned by InstallApp when the target app comes from
// an unsigned source and the caller has not acknowledged the risk. It carries
// the concrete images that would run so the UI can show the user what they are
// trusting before they confirm.
type UnsignedSourceError struct {
	AppID      string   `json:"app_id"`
	SourceID   uint     `json:"source_id"`
	SourceName string   `json:"source_name"`
	Images     []string `json:"images"`
}

func (e *UnsignedSourceError) Error() string {
	return fmt.Sprintf("app %q comes from an unsigned source %q (id %d) and was not acknowledged; "+
		"set acknowledge_unsigned=true to install. Images that would run: %s",
		e.AppID, e.SourceName, e.SourceID, strings.Join(e.Images, ", "))
}

// InstallApp renders compose, creates Docker Stack, optionally creates host.
func (s *Service) InstallApp(req *InstallAppRequest) (*InstalledApp, error) {
	// 1. Find app definition
	app, err := s.GetAppByAppID(req.AppID)
	if err != nil {
		return nil, fmt.Errorf("app %q not found", req.AppID)
	}

	// 1b. Unsigned-source gate. The source is untrusted (no signature/manifest
	// verification); require an explicit, audited acknowledgement before
	// running anything. Surfaced as a structured error listing the images so
	// the UI can show the user exactly what they are about to trust.
	if !req.AcknowledgeUnsigned && s.sourceIsUnsigned(app.SourceID) {
		var srcName string
		var src AppSource
		if s.db.First(&src, app.SourceID).Error == nil {
			srcName = src.Name
		}
		return nil, &UnsignedSourceError{
			AppID:      req.AppID,
			SourceID:   app.SourceID,
			SourceName: srcName,
			Images:     extractComposeImages(app.ComposeFile),
		}
	}

	// 2. Validate force_expose: app requires a domain
	if app.ForceExpose && req.Domain == "" {
		return nil, fmt.Errorf("this app requires a domain to function properly")
	}
	// Validate domain format to prevent .env injection and Caddy config injection.
	if req.Domain != "" {
		if err := caddy.ValidateDomain(req.Domain); err != nil {
			return nil, fmt.Errorf("invalid domain: %w", err)
		}
	}

	// 3. Parse form fields
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

	// Check stack name collision: different app names can sanitize to the same
	// compose project name, causing cross-interference.
	var stackCount int64
	s.db.Model(&InstalledApp{}).Where("stack_name = ?", stackName).Count(&stackCount)
	if stackCount > 0 {
		return nil, fmt.Errorf("stack name %q conflicts with an existing installation — choose a different name", req.Name)
	}

	// Extract url_suffix from config.json
	var appConfig AppConfig
	if app.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(app.ConfigJSON), &appConfig); err != nil {
			return nil, fmt.Errorf("parse app config: %w", err)
		}
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
		"APP_LOCAL_DOMAIN":  req.Domain,
		"ROOT_FOLDER_HOST":  s.dataDir,
		"APP_EXPOSED":       exposed,
		"APP_PROTOCOL":      protocol,
		"APP_HOST":          req.Domain,
		"LOCAL_DOMAIN":      req.Domain,
		"TZ":                getSystemTimezone(),
		"NETWORK_INTERFACE": "127.0.0.1",
		"DNS_IP":            "1.1.1.1",
		"INTERNAL_IP":       getLocalIP(),
		"TIPI_UID":          "1000",
		"TIPI_GID":          "1000",
	}

	// Generate VAPID keys if required by the app
	if appConfig.GenerateVapidKeys {
		pubKey, privKey, err := GenerateVapidKeys()
		if err != nil {
			s.logger.Error("generate VAPID keys failed", "err", err)
		} else {
			builtins["VAPID_PUBLIC_KEY"] = pubKey
			builtins["VAPID_PRIVATE_KEY"] = privKey
		}
	}

	// 8. Render compose and env
	rendered := SanitizeCompose(RenderCompose(app.ComposeFile, req.FormValues, builtins))
	envContent := RenderEnvFile(req.FormValues, builtins)

	// Write files
	if err := os.WriteFile(filepath.Join(composeDir, "docker-compose.yml"), []byte(rendered), 0600); err != nil {
		s.setStatus(installed.ID, "error")
		return nil, fmt.Errorf("write compose: %w", err)
	}
	if err := os.WriteFile(filepath.Join(composeDir, ".env"), []byte(envContent), 0600); err != nil {
		s.setStatus(installed.ID, "error")
		return nil, fmt.Errorf("write env: %w", err)
	}

	// Create data dir
	os.MkdirAll(filepath.Join(composeDir, "data"), 0755)

	// 9. Also create a record in plugin_docker_stacks so Docker Overview shows it
	if err := s.createDockerStackRecord(stackName, rendered, envContent, composeDir); err != nil {
		s.logger.Warn("failed to create docker stack record", "err", err)
	}

	// 10. Pin images to immutable digests, then up. We pull first (failing the
	// install if the registry is unreachable rather than running an unpinned
	// image), resolve each service image to name@sha256:<digest>, rewrite the
	// compose to pin it, and persist the resolved digests for drift detection.
	pinned, digests, err := s.pinComposeImages(composeDir, stackName, rendered)
	if err != nil {
		// Roll back fully: an unpinned compose file + a plugin_docker_stacks
		// record already exist, and Docker Overview could later `up` them and
		// run the floating image — defeating the fail-closed pin. Remove the
		// stack record, the install record, and the on-disk compose.
		s.deleteDockerStackRecord(stackName)
		s.db.Delete(&InstalledApp{}, installed.ID)
		os.RemoveAll(composeDir)
		return nil, fmt.Errorf("pin images: %w", err)
	}
	if pinned != rendered {
		rendered = pinned
		if err := os.WriteFile(filepath.Join(composeDir, "docker-compose.yml"), []byte(rendered), 0600); err != nil {
			s.setStatus(installed.ID, "error")
			return nil, fmt.Errorf("write pinned compose: %w", err)
		}
		// Keep the Docker Overview record in sync with the pinned compose.
		s.db.Table("plugin_docker_stacks").Where("name = ?", stackName).
			Update("compose_file", rendered)
	}
	if len(digests) > 0 {
		if b, err := json.Marshal(digests); err == nil {
			installed.ImageDigests = string(b)
			s.db.Model(installed).Update("image_digests", installed.ImageDigests)
		}
	}

	// docker compose up (images already pulled+pinned above)
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
	// Validate domain format to prevent .env injection and Caddy config injection
	if domain != "" {
		if err := caddy.ValidateDomain(domain); err != nil {
			return fmt.Errorf("invalid domain: %w", err)
		}
	}

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
			return fmt.Errorf("create reverse proxy failed: %w", err)
		}
		installed.HostID = hostID
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
			domainVars := map[string]bool{"APP_DOMAIN": true, "APP_HOST": true, "LOCAL_DOMAIN": true, "APP_LOCAL_DOMAIN": true}
			var out []string
			for _, line := range lines {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 && domainVars[parts[0]] {
					out = append(out, parts[0]+"="+domain)
				} else {
					out = append(out, line)
				}
			}
			os.WriteFile(envPath, []byte(strings.Join(out, "\n")), 0600)
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
		if err := json.Unmarshal([]byte(installed.FormValues), &formValues); err != nil {
			return fmt.Errorf("parse stored form values: %w", err)
		}
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
		"APP_LOCAL_DOMAIN":  installed.Domain,
		"ROOT_FOLDER_HOST":  s.dataDir,
		"APP_EXPOSED":       exposed,
		"APP_PROTOCOL":      "https",
		"APP_HOST":          installed.Domain,
		"LOCAL_DOMAIN":      installed.Domain,
		"TZ":                getSystemTimezone(),
		"NETWORK_INTERFACE": "127.0.0.1",
		"DNS_IP":            "1.1.1.1",
		"INTERNAL_IP":       getLocalIP(),
		"TIPI_UID":          "1000",
		"TIPI_GID":          "1000",
	}

	// Preserve VAPID keys from existing env if present
	if installed.ComposeDir != "" {
		if envData, err := os.ReadFile(filepath.Join(installed.ComposeDir, ".env")); err == nil {
			for _, line := range strings.Split(string(envData), "\n") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 && (parts[0] == "VAPID_PUBLIC_KEY" || parts[0] == "VAPID_PRIVATE_KEY") && parts[1] != "" {
					builtins[parts[0]] = parts[1]
				}
			}
		}
	}

	rendered := SanitizeCompose(RenderCompose(app.ComposeFile, formValues, builtins))
	envContent := RenderEnvFile(formValues, builtins)

	// Capture the current (already-pinned) compose AND .env so we can restore
	// both if the new images can't be pinned — otherwise a failed update would
	// leave an unpinned compose / changed env on disk that the existing stack
	// would later start with.
	composePath := filepath.Join(installed.ComposeDir, "docker-compose.yml")
	envPath := filepath.Join(installed.ComposeDir, ".env")
	prevCompose, _ := os.ReadFile(composePath)
	prevEnv, _ := os.ReadFile(envPath)

	// Write updated files
	if err := os.WriteFile(composePath, []byte(rendered), 0600); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	// Pull new images, pin them to immutable digests, then recreate. Same
	// trust model as install: never run a floating/unpinned image, and record
	// the resolved digests for drift detection.
	pinned, digests, err := s.pinComposeImages(installed.ComposeDir, installed.StackName, rendered)
	if err != nil {
		// Restore the previous pinned compose AND .env so the existing stack
		// stays startable with its pinned image and original environment rather
		// than the new floating image / changed env.
		if len(prevCompose) > 0 {
			os.WriteFile(composePath, prevCompose, 0600)
		}
		if len(prevEnv) > 0 {
			os.WriteFile(envPath, prevEnv, 0600)
		}
		s.setStatus(id, "error")
		return fmt.Errorf("pin images: %w", err)
	}
	if pinned != rendered {
		rendered = pinned
		if err := os.WriteFile(filepath.Join(installed.ComposeDir, "docker-compose.yml"), []byte(rendered), 0600); err != nil {
			s.setStatus(id, "error")
			return fmt.Errorf("write pinned compose file: %w", err)
		}
		s.db.Table("plugin_docker_stacks").Where("name = ?", installed.StackName).
			Update("compose_file", rendered)
	}
	if len(digests) > 0 {
		if b, err := json.Marshal(digests); err == nil {
			s.db.Model(&installed).Update("image_digests", string(b))
		}
	}
	if err := s.runCompose(installed.ComposeDir, installed.StackName, "up", "-d", "--remove-orphans"); err != nil {
		s.setStatus(id, "error")
		return fmt.Errorf("compose up: %w", err)
	}

	// Update version
	if err := s.db.Model(&installed).Updates(map[string]interface{}{
		"version": app.Version,
		"status":  "running",
	}).Error; err != nil {
		return fmt.Errorf("update app status: %w", err)
	}

	return nil
}

// ── Helpers ──

func (s *Service) setStatus(id uint, status string) {
	s.db.Model(&InstalledApp{}).Where("id = ?", id).Update("status", status)
}

// sourceIsUnsigned reports whether the given source is untrusted. Today every
// source is unsigned; the column makes this explicit and future-proofs a
// signed-manifest model. A missing/unknown source is treated as unsigned
// (fail-closed).
func (s *Service) sourceIsUnsigned(sourceID uint) bool {
	var src AppSource
	if err := s.db.First(&src, sourceID).Error; err != nil {
		return true
	}
	return src.Unsigned
}

// extractComposeImages returns the image references declared in a compose
// document (in declaration order, deduplicated). It is a line-based scan that
// mirrors the rest of this plugin's compose handling; it is best-effort and
// used only to inform the user which images would run.
func extractComposeImages(compose string) []string {
	var images []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(compose, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "image:") {
			continue
		}
		ref := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
		// Strip inline comment and surrounding quotes.
		if i := strings.Index(ref, " #"); i >= 0 {
			ref = strings.TrimSpace(ref[:i])
		}
		ref = strings.Trim(ref, "\"'")
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		images = append(images, ref)
	}
	return images
}

// pinComposeImages pulls the compose's images and rewrites every `image:` line
// to an immutable name@sha256:<digest> reference. Already-digest-pinned refs
// and refs containing an unresolved ${VAR} are left untouched. It returns the
// (possibly rewritten) compose and a list of the resolved name@digest refs.
//
// The pull happens via `docker compose pull` so registry auth / .env are
// honored; if it fails (e.g. registry unreachable) we return an error and the
// caller aborts the install rather than running an unpinned image.
func (s *Service) pinComposeImages(dir, projectName, compose string) (string, []string, error) {
	images := extractComposeImages(compose)
	if len(images) == 0 {
		return compose, nil, nil
	}

	// Pull first so the digest we resolve is the one that will actually run.
	if err := s.runCompose(dir, projectName, "pull"); err != nil {
		return "", nil, fmt.Errorf("pull images (registry unreachable?): %w", err)
	}

	// Resolve tag → digest once per distinct image ref.
	resolved := make(map[string]string, len(images))
	var digests []string
	for _, ref := range images {
		if strings.Contains(ref, "${") {
			// Unresolved variable in the image ref: `docker compose up` would
			// still interpolate it from .env and run a floating tag, defeating
			// the digest-pinning guarantee the trust model promises. Fail closed
			// rather than run an unpinned image.
			return "", nil, fmt.Errorf("image %q uses an unresolved variable and cannot be digest-pinned; this app is not supported under digest pinning", ref)
		}
		if strings.Contains(ref, "@sha256:") {
			// Already immutable.
			resolved[ref] = ref
			digests = append(digests, ref)
			continue
		}
		pinned, err := s.resolveImageDigest(ref)
		if err != nil {
			return "", nil, fmt.Errorf("resolve digest for %q: %w", ref, err)
		}
		resolved[ref] = pinned
		digests = append(digests, pinned)
	}

	// Rewrite the `image:` lines, preserving indentation and quoting style.
	var out []string
	for _, line := range strings.Split(compose, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "image:") {
			ref := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "image:")), "\"'")
			if pinned, ok := resolved[ref]; ok && pinned != ref {
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				out = append(out, indent+"image: "+pinned)
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), digests, nil
}

// resolveImageDigest returns the immutable name@sha256:<digest> form of a
// freshly pulled image by reading its RepoDigests from the local image store.
// The repo-digest entry already carries the registry path, so we use it
// directly; if multiple are present we prefer the one matching ref's repo.
func (s *Service) resolveImageDigest(ref string) (string, error) {
	cmd := exec.Command("docker", "image", "inspect", "--format", "{{range .RepoDigests}}{{println .}}{{end}}", ref)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("image inspect: %s", strings.TrimSpace(string(output)))
	}
	repo := ref
	if i := strings.LastIndex(ref, ":"); i >= 0 && !strings.Contains(ref[i:], "/") {
		repo = ref[:i] // strip tag, keep registry/repo
	}
	var fallback string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		rd := strings.TrimSpace(line)
		if rd == "" || !strings.Contains(rd, "@sha256:") {
			continue
		}
		if fallback == "" {
			fallback = rd
		}
		if strings.HasPrefix(rd, repo+"@") {
			return rd, nil
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("no RepoDigest found for %q (image may be locally built or registry stripped digests)", ref)
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
func (s *Service) createDockerStackRecord(name, composeFile, envFile, dataDir string) error {
	// Use raw table name to avoid importing docker package
	if err := s.db.Table("plugin_docker_stacks").Create(map[string]interface{}{
		"name":         name,
		"description":  "[App Store] Managed by App Store plugin",
		"compose_file": composeFile,
		"env_file":     envFile,
		"status":       "running",
		"data_dir":     dataDir,
		"managed_by":   "appstore",
	}).Error; err != nil {
		return fmt.Errorf("create docker stack record: %w", err)
	}
	return nil
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
