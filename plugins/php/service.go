package php

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeName(name string) string {
	return sanitizeRe.ReplaceAllString(strings.ToLower(name), "-")
}

// Service implements the business logic for PHP runtime and site management.
type Service struct {
	db      *gorm.DB
	dataDir string
	logger  *slog.Logger
	coreAPI plugin.CoreAPI
}

// NewService creates a PHP Service.
func NewService(db *gorm.DB, dataDir string, logger *slog.Logger, coreAPI plugin.CoreAPI) *Service {
	return &Service{db: db, dataDir: dataDir, logger: logger, coreAPI: coreAPI}
}

// ── Runtime CRUD ──

// ListRuntimes returns all runtimes with live status.
func (s *Service) ListRuntimes() ([]PHPRuntime, error) {
	var runtimes []PHPRuntime
	if err := s.db.Order("id ASC").Find(&runtimes).Error; err != nil {
		return nil, err
	}
	for i := range runtimes {
		runtimes[i].Status = s.resolveRuntimeStatus(&runtimes[i])
	}
	return runtimes, nil
}

// GetRuntime returns a single runtime with live status.
func (s *Service) GetRuntime(id uint) (*PHPRuntime, error) {
	var rt PHPRuntime
	if err := s.db.First(&rt, id).Error; err != nil {
		return nil, err
	}
	rt.Status = s.resolveRuntimeStatus(&rt)
	return &rt, nil
}

// CreateRuntimeStream creates a PHP-FPM runtime with progress streaming.
func (s *Service) CreateRuntimeStream(req *CreateRuntimeRequest, progressCb func(string)) (*PHPRuntime, error) {
	vInfo := FindVersion(req.Version, req.Type)
	if vInfo == nil {
		return nil, fmt.Errorf("unsupported PHP version %s (%s)", req.Version, req.Type)
	}

	// Only FPM runtimes are shared. FrankenPHP runtimes are per-site.
	if req.Type != RuntimeFPM {
		return nil, fmt.Errorf("only FPM runtimes can be created directly; FrankenPHP is created per-site")
	}

	// Validate extensions.
	if err := ValidateExtensionNames(req.Extensions); err != nil {
		return nil, err
	}

	// Validate memory limit.
	if err := ValidateMemoryLimit(req.MemoryLimit); err != nil {
		return nil, err
	}

	// Check uniqueness.
	var count int64
	if err := s.db.Model(&PHPRuntime{}).Where("version = ? AND type = ?", req.Version, req.Type).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("check uniqueness: %w", err)
	}
	if count > 0 {
		return nil, fmt.Errorf("PHP %s (%s) runtime already exists", req.Version, req.Type)
	}

	// Allocate port — FPM starts at 9080.
	port, err := s.allocatePort(9080, 20)
	if err != nil {
		return nil, fmt.Errorf("allocate port: %w", err)
	}

	memLimit := req.MemoryLimit
	if memLimit == "" {
		memLimit = "256m"
	}

	containerName := fmt.Sprintf("webcasa-php-fpm-%s", strings.ReplaceAll(req.Version, ".", ""))
	rtDir := filepath.Join(s.dataDir, "runtimes", containerName)

	// Serialize extensions.
	extJSON := "[]"
	if len(req.Extensions) > 0 {
		if data, err := json.Marshal(req.Extensions); err == nil {
			extJSON = string(data)
		}
	}

	// Default configs.
	phpCfg := DefaultPHPIniConfig()
	phpCfg.DateTimezone = getSystemTimezone()
	phpCfgJSON, _ := json.Marshal(phpCfg)
	fpmCfg := DefaultFPMPoolConfig()
	fpmCfgJSON, _ := json.Marshal(fpmCfg)

	customImage := ""
	if len(req.Extensions) > 0 {
		customImage = fmt.Sprintf("webcasa-php-fpm-%s:custom", strings.ReplaceAll(req.Version, ".", ""))
	}

	rt := &PHPRuntime{
		Version:       req.Version,
		Type:          RuntimeFPM,
		Status:        "stopped",
		Port:          port,
		ContainerName: containerName,
		DataDir:       rtDir,
		Extensions:    extJSON,
		MemoryLimit:   memLimit,
		PHPConfig:     string(phpCfgJSON),
		FPMConfig:     string(fpmCfgJSON),
		CustomImage:   customImage,
	}

	progressCb("Preparing runtime directory...")
	if err := os.MkdirAll(filepath.Join(rtDir, "conf.d"), 0755); err != nil {
		return nil, fmt.Errorf("create runtime dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(rtDir, "php-fpm.d"), 0755); err != nil {
		return nil, fmt.Errorf("create fpm.d dir: %w", err)
	}

	// Write config files.
	progressCb("Writing configuration files...")
	iniContent, err := GeneratePHPIni(phpCfg)
	if err != nil {
		return nil, fmt.Errorf("generate php.ini: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rtDir, "conf.d", "99-webcasa.ini"), []byte(iniContent), 0644); err != nil {
		return nil, fmt.Errorf("write php.ini: %w", err)
	}
	fpmContent, err := GenerateFPMPoolConf(fpmCfg)
	if err != nil {
		return nil, fmt.Errorf("generate fpm.conf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rtDir, "php-fpm.d", "zz-webcasa.conf"), []byte(fpmContent), 0644); err != nil {
		return nil, fmt.Errorf("write fpm pool conf: %w", err)
	}

	// Generate Dockerfile if extensions requested.
	if len(req.Extensions) > 0 {
		progressCb("Generating Dockerfile with extensions...")
		dockerfile, err := GenerateFPMDockerfile(req.Version, req.Extensions)
		if err != nil {
			return nil, fmt.Errorf("generate Dockerfile: %w", err)
		}
		if err := os.WriteFile(filepath.Join(rtDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
			return nil, fmt.Errorf("write Dockerfile: %w", err)
		}
	}

	// Generate compose file.
	tz := getSystemTimezone()
	composeContent := GenerateFPMCompose(rt, tz)
	if err := os.WriteFile(filepath.Join(rtDir, "docker-compose.yml"), []byte(composeContent), 0644); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}

	progressCb("Saving runtime record...")
	if err := s.db.Create(rt).Error; err != nil {
		return nil, fmt.Errorf("create runtime record: %w", err)
	}

	if req.AutoStart {
		progressCb("Starting PHP-FPM (pulling image if needed)...")
		if len(req.Extensions) > 0 {
			progressCb("Building custom image with extensions...")
			if err := s.runComposeStream(rtDir, progressCb, "build", "--no-cache"); err != nil {
				// Rollback: remove DB record and data dir.
				s.db.Delete(&PHPRuntime{}, rt.ID)
				os.RemoveAll(rtDir)
				return nil, fmt.Errorf("build failed: %w", err)
			}
		}
		if err := s.runComposeStream(rtDir, progressCb, "up", "-d", "--remove-orphans"); err != nil {
			// Rollback: remove DB record and data dir.
			s.db.Delete(&PHPRuntime{}, rt.ID)
			os.RemoveAll(rtDir)
			return nil, fmt.Errorf("auto-start failed: %w", err)
		}
	}

	return s.GetRuntime(rt.ID)
}

// DeleteRuntime stops and removes a PHP runtime.
func (s *Service) DeleteRuntime(id uint) error {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}

	// Check if any FPM sites depend on this runtime.
	var siteCount int64
	if err := s.db.Model(&PHPSite{}).Where("runtime_id = ?", id).Count(&siteCount).Error; err != nil {
		return fmt.Errorf("check site dependencies: %w", err)
	}
	if siteCount > 0 {
		return fmt.Errorf("cannot delete: %d site(s) still use this runtime", siteCount)
	}

	if err := s.runCompose(rt.DataDir, "down", "--volumes", "--remove-orphans"); err != nil {
		s.logger.Error("compose down failed for runtime", "runtime", rt.ContainerName, "err", err)
		return fmt.Errorf("failed to stop runtime: %w (runtime kept for manual cleanup)", err)
	}
	os.RemoveAll(rt.DataDir)
	return s.db.Delete(&PHPRuntime{}, id).Error
}

// StartRuntime starts the runtime containers.
func (s *Service) StartRuntime(id uint) error {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}
	return s.runCompose(rt.DataDir, "up", "-d", "--remove-orphans")
}

// StopRuntime stops the runtime containers.
func (s *Service) StopRuntime(id uint) error {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}
	return s.runCompose(rt.DataDir, "down")
}

// RestartRuntime restarts the runtime containers.
func (s *Service) RestartRuntime(id uint) error {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}
	return s.runCompose(rt.DataDir, "restart")
}

// GetRuntimeLogs returns the last N lines of container logs.
func (s *Service) GetRuntimeLogs(id uint, lines int) (string, error) {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return "", err
	}
	if lines <= 0 {
		lines = 100
	}
	if lines > 1000 {
		lines = 1000
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "logs", "--tail", fmt.Sprintf("%d", lines), rt.ContainerName).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("get logs: %w", err)
	}
	return string(out), nil
}

// ── Config Management ──

// GetConfig returns the runtime's PHP and FPM config.
func (s *Service) GetConfig(id uint) (*PHPIniConfig, *FPMPoolConfig, error) {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return nil, nil, err
	}

	var phpCfg PHPIniConfig
	if rt.PHPConfig != "" {
		if err := json.Unmarshal([]byte(rt.PHPConfig), &phpCfg); err != nil {
			phpCfg = DefaultPHPIniConfig()
		}
	} else {
		phpCfg = DefaultPHPIniConfig()
	}

	var fpmCfg FPMPoolConfig
	if rt.FPMConfig != "" {
		if err := json.Unmarshal([]byte(rt.FPMConfig), &fpmCfg); err != nil {
			fpmCfg = DefaultFPMPoolConfig()
		}
	} else {
		fpmCfg = DefaultFPMPoolConfig()
	}

	return &phpCfg, &fpmCfg, nil
}

// UpdateConfig updates the runtime config, regenerates files, and restarts.
func (s *Service) UpdateConfig(id uint, req *UpdateConfigRequest) error {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}

	// Save old config for rollback.
	oldPHPConfig := rt.PHPConfig
	oldFPMConfig := rt.FPMConfig

	// Back up old config files.
	var oldIniContent, oldFpmContent []byte
	iniPath := filepath.Join(rt.DataDir, "conf.d", "99-webcasa.ini")
	confPath := filepath.Join(rt.DataDir, "php-fpm.d", "zz-webcasa.conf")

	if req.PHPConfig != nil {
		oldIniContent, _ = os.ReadFile(iniPath)
		iniContent, err := GeneratePHPIni(*req.PHPConfig)
		if err != nil {
			return fmt.Errorf("validate php.ini: %w", err)
		}
		data, _ := json.Marshal(req.PHPConfig)
		rt.PHPConfig = string(data)
		if err := os.WriteFile(iniPath, []byte(iniContent), 0644); err != nil {
			return fmt.Errorf("write php.ini: %w", err)
		}
	}

	if req.FPMConfig != nil {
		oldFpmContent, _ = os.ReadFile(confPath)
		confContent, err := GenerateFPMPoolConf(*req.FPMConfig)
		if err != nil {
			return fmt.Errorf("validate fpm.conf: %w", err)
		}
		data, _ := json.Marshal(req.FPMConfig)
		rt.FPMConfig = string(data)
		if err := os.WriteFile(confPath, []byte(confContent), 0644); err != nil {
			return fmt.Errorf("write fpm.conf: %w", err)
		}
	}

	if err := s.db.Save(rt).Error; err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Restart to apply. If restart fails, rollback DB and config files.
	if err := s.runCompose(rt.DataDir, "restart"); err != nil {
		s.logger.Error("restart failed after config update, rolling back", "runtime", rt.ContainerName, "err", err)
		rt.PHPConfig = oldPHPConfig
		rt.FPMConfig = oldFPMConfig
		s.db.Save(rt)
		if oldIniContent != nil {
			os.WriteFile(iniPath, oldIniContent, 0644)
		}
		if oldFpmContent != nil {
			os.WriteFile(confPath, oldFpmContent, 0644)
		}
		// Try restarting with old config.
		_ = s.runCompose(rt.DataDir, "restart")
		return fmt.Errorf("restart failed with new config (rolled back): %w", err)
	}
	return nil
}

// Optimize auto-calculates optimal FPM settings.
func (s *Service) Optimize(id uint) (*FPMPoolConfig, error) {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return nil, err
	}
	if rt.Type != RuntimeFPM {
		return nil, fmt.Errorf("optimization only applies to FPM runtimes")
	}

	cfg := AutoOptimize()

	// Apply and save.
	req := &UpdateConfigRequest{FPMConfig: &cfg}
	if err := s.UpdateConfig(id, req); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ── Extension Management ──

// GetExtensions returns the runtime's installed extensions.
func (s *Service) GetExtensions(id uint) ([]string, error) {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return nil, err
	}
	return parseExtensions(rt.Extensions), nil
}

// InstallExtensions adds extensions, rebuilds the image, and restarts.
func (s *Service) InstallExtensions(id uint, extensions []string, progressCb func(string)) error {
	// Validate extension names against allowlist.
	if err := ValidateExtensionNames(extensions); err != nil {
		return err
	}

	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}

	// Merge extensions.
	existing := parseExtensions(rt.Extensions)
	merged := mergeStringSlices(existing, extensions)
	extJSON, _ := json.Marshal(merged)
	rt.Extensions = string(extJSON)

	// Update custom image name.
	customImage := fmt.Sprintf("webcasa-php-%s-%s:custom", rt.Type, strings.ReplaceAll(rt.Version, ".", ""))
	rt.CustomImage = customImage

	// Save old state for rollback.
	oldExtensions := rt.Extensions
	oldCustomImage := rt.CustomImage

	// Save to DB first (before rebuild).
	if err := s.db.Save(rt).Error; err != nil {
		return fmt.Errorf("save extensions: %w", err)
	}

	if err := s.rebuildRuntimeImage(rt, merged, progressCb); err != nil {
		// Rollback DB to old extensions.
		rt.Extensions = oldExtensions
		rt.CustomImage = oldCustomImage
		s.db.Save(rt)
		return err
	}
	return nil
}

// RemoveExtension removes an extension, rebuilds the image, and restarts.
func (s *Service) RemoveExtension(id uint, extName string, progressCb func(string)) error {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}

	existing := parseExtensions(rt.Extensions)
	found := false
	var filtered []string
	for _, e := range existing {
		if e != extName {
			filtered = append(filtered, e)
		} else {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("extension %q is not installed", extName)
	}

	// Save old state for rollback.
	oldExtensions := rt.Extensions
	oldCustomImage := rt.CustomImage

	extJSON, _ := json.Marshal(filtered)
	rt.Extensions = string(extJSON)

	// Save to DB first (before rebuild).
	if err := s.db.Save(rt).Error; err != nil {
		return fmt.Errorf("save extensions: %w", err)
	}

	rollback := func() {
		rt.Extensions = oldExtensions
		rt.CustomImage = oldCustomImage
		s.db.Save(rt)
	}

	if len(filtered) == 0 {
		// No extensions left: remove Dockerfile, reset custom image.
		os.Remove(filepath.Join(rt.DataDir, "Dockerfile"))
		rt.CustomImage = ""
		if err := s.db.Save(rt).Error; err != nil {
			return fmt.Errorf("save runtime: %w", err)
		}
		// Regenerate compose without custom image.
		tz := getSystemTimezone()
		composeContent := GenerateFPMCompose(rt, tz)
		if err := os.WriteFile(filepath.Join(rt.DataDir, "docker-compose.yml"), []byte(composeContent), 0644); err != nil {
			return fmt.Errorf("write compose: %w", err)
		}
		progressCb("Recreating container with base image...")
		if err := s.runComposeStream(rt.DataDir, progressCb, "up", "-d", "--force-recreate"); err != nil {
			rollback()
			return fmt.Errorf("recreate container failed (rolled back): %w", err)
		}
		return nil
	}

	if err := s.rebuildRuntimeImage(rt, filtered, progressCb); err != nil {
		rollback()
		return err
	}
	return nil
}

// rebuildRuntimeImage rebuilds the Docker image with the given extensions and restarts.
func (s *Service) rebuildRuntimeImage(rt *PHPRuntime, extensions []string, progressCb func(string)) error {
	progressCb("Generating Dockerfile with updated extensions...")
	var dockerfile string
	var err error
	if rt.Type == RuntimeFPM {
		dockerfile, err = GenerateFPMDockerfile(rt.Version, extensions)
	} else {
		dockerfile, err = GenerateFrankenDockerfile(rt.Version, extensions)
	}
	if err != nil {
		return fmt.Errorf("generate Dockerfile: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rt.DataDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	// Regenerate compose with custom image.
	tz := getSystemTimezone()
	composeContent := GenerateFPMCompose(rt, tz)
	if err := os.WriteFile(filepath.Join(rt.DataDir, "docker-compose.yml"), []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("write compose: %w", err)
	}

	// Rebuild image.
	progressCb("Building custom image...")
	if err := s.runComposeStream(rt.DataDir, progressCb, "build", "--no-cache"); err != nil {
		return fmt.Errorf("build image: %w", err)
	}

	// Recreate container.
	progressCb("Recreating container...")
	if err := s.runComposeStream(rt.DataDir, progressCb, "up", "-d", "--force-recreate"); err != nil {
		return fmt.Errorf("recreate container: %w", err)
	}

	return nil
}

// ── Site Management ──

// ListSites returns all PHP sites.
func (s *Service) ListSites() ([]PHPSite, error) {
	var sites []PHPSite
	if err := s.db.Order("id ASC").Find(&sites).Error; err != nil {
		return nil, err
	}
	// Resolve FrankenPHP site status.
	for i := range sites {
		if sites[i].RuntimeType == string(RuntimeFranken) && sites[i].ContainerName != "" {
			sites[i].Status = s.resolveContainerStatus(sites[i].ContainerName)
		}
	}
	return sites, nil
}

// GetSite returns a single site.
func (s *Service) GetSite(id uint) (*PHPSite, error) {
	var site PHPSite
	if err := s.db.First(&site, id).Error; err != nil {
		return nil, err
	}
	if site.RuntimeType == string(RuntimeFranken) && site.ContainerName != "" {
		site.Status = s.resolveContainerStatus(site.ContainerName)
	}
	return &site, nil
}

// CreateSite creates a new PHP site.
func (s *Service) CreateSite(req *CreateSiteRequest, progressCb func(string)) (*PHPSite, error) {
	// Validate domain.
	if err := caddy.ValidateDomain(req.Domain); err != nil {
		return nil, fmt.Errorf("invalid domain: %w", err)
	}

	// Validate extensions.
	if err := ValidateExtensionNames(req.Extensions); err != nil {
		return nil, err
	}

	// Validate worker script.
	if err := ValidateWorkerScript(req.WorkerScript); err != nil {
		return nil, err
	}

	// Validate site name format (alphanumeric, hyphens, underscores).
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,126}$`).MatchString(req.Name) {
		return nil, fmt.Errorf("invalid site name: only letters, numbers, hyphens and underscores allowed (1-127 chars)")
	}

	// Validate uniqueness.
	var count int64
	if err := s.db.Model(&PHPSite{}).Where("name = ?", req.Name).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("check uniqueness: %w", err)
	}
	if count > 0 {
		return nil, fmt.Errorf("site name %q already exists", req.Name)
	}

	rootPath := req.RootPath
	if rootPath == "" {
		rootPath = filepath.Join("/var/www", req.Domain)
	}

	// Validate root path.
	if err := ValidateRootPath(rootPath); err != nil {
		return nil, err
	}

	site := &PHPSite{
		Name:        req.Name,
		Domain:      req.Domain,
		RootPath:    rootPath,
		PHPVersion:  req.PHPVersion,
		RuntimeType: req.RuntimeType,
		Status:      "active",
	}

	switch RuntimeType(req.RuntimeType) {
	case RuntimeFPM:
		return s.createFPMSite(site, req, progressCb)
	case RuntimeFranken:
		return s.createFrankenSite(site, req, progressCb)
	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", req.RuntimeType)
	}
}

func (s *Service) createFPMSite(site *PHPSite, req *CreateSiteRequest, progressCb func(string)) (*PHPSite, error) {
	// Verify FPM runtime exists and is running.
	rt, err := s.GetRuntime(req.RuntimeID)
	if err != nil {
		return nil, fmt.Errorf("FPM runtime not found: %w", err)
	}
	if rt.Type != RuntimeFPM {
		return nil, fmt.Errorf("runtime %d is not a FPM runtime", req.RuntimeID)
	}
	if rt.Status != "running" {
		return nil, fmt.Errorf("FPM runtime %q is not running (status: %s) — start it first", rt.ContainerName, rt.Status)
	}

	site.RuntimeID = rt.ID
	site.Port = rt.Port

	// Create site directory with default index.php.
	progressCb("Creating site directory...")
	if err := os.MkdirAll(site.RootPath, 0755); err != nil {
		return nil, fmt.Errorf("create site dir: %w", err)
	}
	indexPath := filepath.Join(site.RootPath, "index.php")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		if err := os.WriteFile(indexPath, []byte("<?php\nphpinfo();\n"), 0644); err != nil {
			s.logger.Warn("failed to write default index.php", "err", err)
		}
	}

	// Create Caddy host (php type).
	progressCb("Creating Caddy host...")
	hostID, err := s.coreAPI.CreateHost(plugin.CreateHostRequest{
		Domain:       req.Domain,
		HostType:     "php",
		RootPath:     site.RootPath,
		PHPFastCGI:   fmt.Sprintf("localhost:%d", rt.Port),
		TLSEnabled:   req.TLSEnabled,
		HTTPRedirect: req.HTTPRedirect,
		Compression:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("create Caddy host: %w", err)
	}
	site.HostID = hostID

	progressCb("Saving site record...")
	if err := s.db.Create(site).Error; err != nil {
		// Rollback: delete Caddy host.
		_ = s.coreAPI.DeleteHost(hostID)
		return nil, fmt.Errorf("create site record: %w", err)
	}

	return s.GetSite(site.ID)
}

func (s *Service) createFrankenSite(site *PHPSite, req *CreateSiteRequest, progressCb func(string)) (*PHPSite, error) {
	// Validate FrankenPHP version.
	vInfo := FindVersion(req.PHPVersion, RuntimeFranken)
	if vInfo == nil {
		return nil, fmt.Errorf("FrankenPHP does not support PHP %s", req.PHPVersion)
	}

	// Allocate port for this site (9100+).
	port, err := s.allocatePort(9100, 1000)
	if err != nil {
		return nil, fmt.Errorf("allocate port: %w", err)
	}
	site.Port = port
	site.ContainerName = fmt.Sprintf("webcasa-fp-%s", sanitizeName(req.Name))
	site.DataDir = filepath.Join(s.dataDir, "sites", site.ContainerName)
	site.WorkerMode = req.WorkerMode
	site.WorkerScript = req.WorkerScript

	// Create directories.
	progressCb("Creating site directory...")
	if err := os.MkdirAll(site.RootPath, 0755); err != nil {
		return nil, fmt.Errorf("create site dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(site.DataDir, "conf.d"), 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// Default index.php.
	indexPath := filepath.Join(site.RootPath, "index.php")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		if err := os.WriteFile(indexPath, []byte("<?php\nphpinfo();\n"), 0644); err != nil {
			s.logger.Warn("failed to write default index.php", "err", err)
		}
	}

	// Write default php.ini.
	phpCfg := DefaultPHPIniConfig()
	phpCfg.DateTimezone = getSystemTimezone()
	iniContent, err := GeneratePHPIni(phpCfg)
	if err != nil {
		return nil, fmt.Errorf("generate php.ini: %w", err)
	}
	if err := os.WriteFile(filepath.Join(site.DataDir, "conf.d", "99-webcasa.ini"), []byte(iniContent), 0644); err != nil {
		return nil, fmt.Errorf("write php.ini: %w", err)
	}

	// Write Caddyfile for FrankenPHP.
	caddyfile := GenerateFrankenCaddyfile(site)
	if err := os.WriteFile(filepath.Join(site.DataDir, "Caddyfile"), []byte(caddyfile), 0644); err != nil {
		return nil, fmt.Errorf("write Caddyfile: %w", err)
	}

	// Dockerfile + extensions.
	customImage := ""
	memoryLimit := "256m"
	if len(req.Extensions) > 0 {
		progressCb("Generating Dockerfile with extensions...")
		dockerfile, err := GenerateFrankenDockerfile(req.PHPVersion, req.Extensions)
		if err != nil {
			return nil, fmt.Errorf("generate Dockerfile: %w", err)
		}
		if err := os.WriteFile(filepath.Join(site.DataDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
			return nil, fmt.Errorf("write Dockerfile: %w", err)
		}
		customImage = fmt.Sprintf("webcasa-fp-%s:custom", sanitizeName(req.Name))
		extJSON, _ := json.Marshal(req.Extensions)
		site.Extensions = string(extJSON)
	}

	// Write docker-compose.yml.
	tz := getSystemTimezone()
	composeContent := GenerateFrankenCompose(site, tz, customImage, memoryLimit)
	if err := os.WriteFile(filepath.Join(site.DataDir, "docker-compose.yml"), []byte(composeContent), 0644); err != nil {
		return nil, fmt.Errorf("write compose: %w", err)
	}

	// Build and start.
	if customImage != "" {
		progressCb("Building FrankenPHP image with extensions...")
		if err := s.runComposeStream(site.DataDir, progressCb, "build", "--no-cache"); err != nil {
			os.RemoveAll(site.DataDir)
			return nil, fmt.Errorf("build failed: %w", err)
		}
	}
	progressCb("Starting FrankenPHP container...")
	if err := s.runComposeStream(site.DataDir, progressCb, "up", "-d", "--remove-orphans"); err != nil {
		os.RemoveAll(site.DataDir)
		return nil, fmt.Errorf("start failed: %w", err)
	}

	// Create Caddy reverse proxy host.
	progressCb("Creating Caddy reverse proxy...")
	hostID, err := s.coreAPI.CreateHost(plugin.CreateHostRequest{
		Domain:       req.Domain,
		HostType:     "proxy",
		UpstreamAddr: fmt.Sprintf("localhost:%d", site.Port),
		TLSEnabled:   req.TLSEnabled,
		HTTPRedirect: req.HTTPRedirect,
		Compression:  true,
	})
	if err != nil {
		// Rollback: stop container.
		_ = s.runCompose(site.DataDir, "down", "--volumes", "--remove-orphans")
		return nil, fmt.Errorf("create Caddy host: %w", err)
	}
	site.HostID = hostID

	progressCb("Saving site record...")
	if err := s.db.Create(site).Error; err != nil {
		// Rollback: delete Caddy host + stop container.
		_ = s.coreAPI.DeleteHost(hostID)
		_ = s.runCompose(site.DataDir, "down", "--volumes", "--remove-orphans")
		return nil, fmt.Errorf("create site record: %w", err)
	}

	return s.GetSite(site.ID)
}

// DeleteSite removes a PHP site and optionally its files.
func (s *Service) DeleteSite(id uint, deleteFiles bool) error {
	site, err := s.GetSite(id)
	if err != nil {
		return err
	}

	// FrankenPHP: stop and remove container first — if this fails, keep the site.
	if site.RuntimeType == string(RuntimeFranken) && site.DataDir != "" {
		if err := s.runCompose(site.DataDir, "down", "--volumes", "--remove-orphans"); err != nil {
			s.logger.Error("compose down failed for site", "site", site.Name, "err", err)
			return fmt.Errorf("failed to stop site container: %w (site kept for manual cleanup)", err)
		}
		os.RemoveAll(site.DataDir)
	}

	// Delete Caddy host.
	if site.HostID > 0 {
		if err := s.coreAPI.DeleteHost(site.HostID); err != nil {
			s.logger.Warn("failed to delete Caddy host", "host_id", site.HostID, "err", err)
		}
	}

	// Optionally delete site files.
	if deleteFiles && site.RootPath != "" {
		os.RemoveAll(site.RootPath)
	}

	return s.db.Delete(&PHPSite{}, id).Error
}

// UpdateSite updates site properties.
func (s *Service) UpdateSite(id uint, req *UpdateSiteRequest) error {
	site, err := s.GetSite(id)
	if err != nil {
		return err
	}

	domainChanged := false
	if req.Domain != "" && req.Domain != site.Domain {
		if err := caddy.ValidateDomain(req.Domain); err != nil {
			return fmt.Errorf("invalid domain: %w", err)
		}
		site.Domain = req.Domain
		domainChanged = true
	}
	if req.WorkerMode != nil {
		site.WorkerMode = *req.WorkerMode
	}
	if req.WorkerScript != "" {
		if err := ValidateWorkerScript(req.WorkerScript); err != nil {
			return err
		}
		site.WorkerScript = req.WorkerScript
	}

	if err := s.db.Save(site).Error; err != nil {
		return err
	}

	// Regenerate FrankenPHP Caddyfile if needed.
	if site.RuntimeType == string(RuntimeFranken) && site.DataDir != "" {
		caddyfile := GenerateFrankenCaddyfile(site)
		if err := os.WriteFile(filepath.Join(site.DataDir, "Caddyfile"), []byte(caddyfile), 0644); err != nil {
			return fmt.Errorf("write Caddyfile: %w", err)
		}
		_ = s.runCompose(site.DataDir, "restart")
	}

	// Sync domain change to Caddy host.
	if domainChanged && site.HostID > 0 {
		if err := s.coreAPI.UpdateHost(site.HostID, plugin.UpdateHostRequest{
			Domain: site.Domain,
		}); err != nil {
			return fmt.Errorf("update Caddy host domain: %w", err)
		}
	}

	return nil
}

// ── Docker helpers ──

func (s *Service) runCompose(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %s: %s — %w", args[0], string(out), err)
	}
	return nil
}

func (s *Service) runComposeStream(dir string, cb func(string), args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("docker compose %s: %w", args[0], err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		cb(scanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("docker compose %s failed: %w", args[0], err)
	}
	return nil
}

func (s *Service) resolveRuntimeStatus(rt *PHPRuntime) string {
	if rt.ContainerName == "" {
		return "stopped"
	}
	return s.resolveContainerStatus(rt.ContainerName)
}

func (s *Service) resolveContainerStatus(containerName string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerName).Output()
	if err != nil {
		return "stopped"
	}
	if strings.TrimSpace(string(out)) == "true" {
		return "running"
	}
	return "stopped"
}

// allocatePort finds the next available port in the given range.
// It checks both PHPRuntime and PHPSite tables to avoid collisions.
func (s *Service) allocatePort(basePort, maxRange int) (int, error) {
	usedPorts := make(map[int]bool)

	var runtimes []PHPRuntime
	s.db.Select("port").Find(&runtimes)
	for _, r := range runtimes {
		usedPorts[r.Port] = true
	}

	var sites []PHPSite
	s.db.Where("port > 0").Select("port").Find(&sites)
	for _, site := range sites {
		usedPorts[site.Port] = true
	}

	for port := basePort; port < basePort+maxRange; port++ {
		if !usedPorts[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", basePort, basePort+maxRange-1)
}

func mergeStringSlices(existing, additions []string) []string {
	set := make(map[string]bool)
	for _, e := range existing {
		set[e] = true
	}
	for _, a := range additions {
		set[a] = true
	}
	var result []string
	for k := range set {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

// getSystemTimezone reads the system timezone.
func getSystemTimezone() string {
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(data)); tz != "" {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if idx := strings.Index(target, "zoneinfo/"); idx >= 0 {
			return target[idx+len("zoneinfo/"):]
		}
	}
	return "UTC"
}
