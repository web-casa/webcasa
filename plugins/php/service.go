package php

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

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

	// Check uniqueness.
	var count int64
	s.db.Model(&PHPRuntime{}).Where("version = ? AND type = ?", req.Version, req.Type).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("PHP %s (%s) runtime already exists", req.Version, req.Type)
	}

	// Allocate port — FPM starts at 9080.
	port := s.allocateFPMPort()

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
	if err := os.WriteFile(filepath.Join(rtDir, "conf.d", "99-webcasa.ini"), []byte(GeneratePHPIni(phpCfg)), 0644); err != nil {
		return nil, fmt.Errorf("write php.ini: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rtDir, "php-fpm.d", "zz-webcasa.conf"), []byte(GenerateFPMPoolConf(fpmCfg)), 0644); err != nil {
		return nil, fmt.Errorf("write fpm pool conf: %w", err)
	}

	// Generate Dockerfile if extensions requested.
	if len(req.Extensions) > 0 {
		progressCb("Generating Dockerfile with extensions...")
		dockerfile := GenerateFPMDockerfile(req.Version, req.Extensions)
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
				progressCb("Warning: build failed: " + err.Error())
			}
		}
		if err := s.runComposeStream(rtDir, progressCb, "up", "-d", "--remove-orphans"); err != nil {
			s.logger.Error("auto-start failed", "runtime", rt.ContainerName, "err", err)
			progressCb("Warning: auto-start failed: " + err.Error())
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
	s.db.Model(&PHPSite{}).Where("runtime_id = ?", id).Count(&siteCount)
	if siteCount > 0 {
		return fmt.Errorf("cannot delete: %d site(s) still use this runtime", siteCount)
	}

	_ = s.runCompose(rt.DataDir, "down", "--volumes", "--remove-orphans")
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
	out, err := exec.Command("docker", "logs", "--tail", fmt.Sprintf("%d", lines), rt.ContainerName).CombinedOutput()
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

	if req.PHPConfig != nil {
		data, _ := json.Marshal(req.PHPConfig)
		rt.PHPConfig = string(data)
		// Regenerate php.ini file.
		iniPath := filepath.Join(rt.DataDir, "conf.d", "99-webcasa.ini")
		if err := os.WriteFile(iniPath, []byte(GeneratePHPIni(*req.PHPConfig)), 0644); err != nil {
			return fmt.Errorf("write php.ini: %w", err)
		}
	}

	if req.FPMConfig != nil {
		data, _ := json.Marshal(req.FPMConfig)
		rt.FPMConfig = string(data)
		// Regenerate FPM pool conf.
		confPath := filepath.Join(rt.DataDir, "php-fpm.d", "zz-webcasa.conf")
		if err := os.WriteFile(confPath, []byte(GenerateFPMPoolConf(*req.FPMConfig)), 0644); err != nil {
			return fmt.Errorf("write fpm.conf: %w", err)
		}
	}

	if err := s.db.Save(rt).Error; err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Restart to apply.
	return s.runCompose(rt.DataDir, "restart")
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
	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}

	// Merge extensions.
	existing := parseExtensions(rt.Extensions)
	merged := mergeStringSlices(existing, extensions)
	extJSON, _ := json.Marshal(merged)

	// Regenerate Dockerfile.
	progressCb("Generating Dockerfile with updated extensions...")
	var dockerfile string
	if rt.Type == RuntimeFPM {
		dockerfile = GenerateFPMDockerfile(rt.Version, merged)
	} else {
		dockerfile = GenerateFrankenDockerfile(rt.Version, merged)
	}
	if err := os.WriteFile(filepath.Join(rt.DataDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	// Update custom image name.
	customImage := fmt.Sprintf("webcasa-php-%s-%s:custom", rt.Type, strings.ReplaceAll(rt.Version, ".", ""))
	rt.CustomImage = customImage
	rt.Extensions = string(extJSON)

	// Regenerate compose with custom image.
	tz := getSystemTimezone()
	composeContent := GenerateFPMCompose(rt, tz)
	if err := os.WriteFile(filepath.Join(rt.DataDir, "docker-compose.yml"), []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("write compose: %w", err)
	}

	if err := s.db.Save(rt).Error; err != nil {
		return fmt.Errorf("save extensions: %w", err)
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

// RemoveExtension removes an extension, rebuilds the image, and restarts.
func (s *Service) RemoveExtension(id uint, extName string, progressCb func(string)) error {
	rt, err := s.GetRuntime(id)
	if err != nil {
		return err
	}

	existing := parseExtensions(rt.Extensions)
	var filtered []string
	for _, e := range existing {
		if e != extName {
			filtered = append(filtered, e)
		}
	}

	extJSON, _ := json.Marshal(filtered)
	rt.Extensions = string(extJSON)

	// Regenerate Dockerfile and rebuild.
	return s.InstallExtensions(id, nil, progressCb)
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
	// Validate uniqueness.
	var count int64
	s.db.Model(&PHPSite{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("site name %q already exists", req.Name)
	}

	rootPath := req.RootPath
	if rootPath == "" {
		rootPath = filepath.Join("/var/www", req.Domain)
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

	site.RuntimeID = rt.ID
	site.Port = rt.Port

	// Create site directory with default index.php.
	progressCb("Creating site directory...")
	if err := os.MkdirAll(site.RootPath, 0755); err != nil {
		return nil, fmt.Errorf("create site dir: %w", err)
	}
	indexPath := filepath.Join(site.RootPath, "index.php")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		defaultIndex := "<?php\nphpinfo();\n"
		os.WriteFile(indexPath, []byte(defaultIndex), 0644)
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
	site.Port = s.allocateFrankenPort()
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
		defaultIndex := "<?php\nphpinfo();\n"
		os.WriteFile(indexPath, []byte(defaultIndex), 0644)
	}

	// Write default php.ini.
	phpCfg := DefaultPHPIniConfig()
	phpCfg.DateTimezone = getSystemTimezone()
	if err := os.WriteFile(filepath.Join(site.DataDir, "conf.d", "99-webcasa.ini"), []byte(GeneratePHPIni(phpCfg)), 0644); err != nil {
		return nil, fmt.Errorf("write php.ini: %w", err)
	}

	// Write Caddyfile for FrankenPHP.
	caddyfile := GenerateFrankenCaddyfile(site)
	if err := os.WriteFile(filepath.Join(site.DataDir, "Caddyfile"), []byte(caddyfile), 0644); err != nil {
		return nil, fmt.Errorf("write Caddyfile: %w", err)
	}

	// Dockerfile + extensions.
	customImage := ""
	if len(req.Extensions) > 0 {
		progressCb("Generating Dockerfile with extensions...")
		dockerfile := GenerateFrankenDockerfile(req.PHPVersion, req.Extensions)
		if err := os.WriteFile(filepath.Join(site.DataDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
			return nil, fmt.Errorf("write Dockerfile: %w", err)
		}
		customImage = fmt.Sprintf("webcasa-fp-%s:custom", sanitizeName(req.Name))
		extJSON, _ := json.Marshal(req.Extensions)
		site.Extensions = string(extJSON)
	}

	// Write docker-compose.yml.
	tz := getSystemTimezone()
	composeContent := GenerateFrankenCompose(site, tz, customImage)
	if err := os.WriteFile(filepath.Join(site.DataDir, "docker-compose.yml"), []byte(composeContent), 0644); err != nil {
		return nil, fmt.Errorf("write compose: %w", err)
	}

	// Build and start.
	if customImage != "" {
		progressCb("Building FrankenPHP image with extensions...")
		if err := s.runComposeStream(site.DataDir, progressCb, "build", "--no-cache"); err != nil {
			progressCb("Warning: build failed: " + err.Error())
		}
	}
	progressCb("Starting FrankenPHP container...")
	if err := s.runComposeStream(site.DataDir, progressCb, "up", "-d", "--remove-orphans"); err != nil {
		progressCb("Warning: start failed: " + err.Error())
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
		return nil, fmt.Errorf("create Caddy host: %w", err)
	}
	site.HostID = hostID

	progressCb("Saving site record...")
	if err := s.db.Create(site).Error; err != nil {
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

	// Delete Caddy host.
	if site.HostID > 0 {
		_ = s.coreAPI.DeleteHost(site.HostID)
	}

	// FrankenPHP: stop and remove container.
	if site.RuntimeType == string(RuntimeFranken) && site.DataDir != "" {
		_ = s.runCompose(site.DataDir, "down", "--volumes", "--remove-orphans")
		os.RemoveAll(site.DataDir)
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

	if req.Domain != "" && req.Domain != site.Domain {
		site.Domain = req.Domain
	}
	if req.WorkerMode != nil {
		site.WorkerMode = *req.WorkerMode
	}
	if req.WorkerScript != "" {
		site.WorkerScript = req.WorkerScript
	}

	if err := s.db.Save(site).Error; err != nil {
		return err
	}

	// Regenerate FrankenPHP Caddyfile if needed.
	if site.RuntimeType == string(RuntimeFranken) && site.DataDir != "" {
		caddyfile := GenerateFrankenCaddyfile(site)
		os.WriteFile(filepath.Join(site.DataDir, "Caddyfile"), []byte(caddyfile), 0644)
		_ = s.runCompose(site.DataDir, "restart")
	}

	return nil
}

// ── Docker helpers ──

func (s *Service) runCompose(dir string, args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %s: %s — %w", args[0], string(out), err)
	}
	return nil
}

func (s *Service) runComposeStream(dir string, cb func(string), args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
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
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", containerName).Output()
	if err != nil {
		return "stopped"
	}
	if strings.TrimSpace(string(out)) == "true" {
		return "running"
	}
	return "stopped"
}

func (s *Service) allocateFPMPort() int {
	basePort := 9080
	var runtimes []PHPRuntime
	s.db.Select("port").Find(&runtimes)
	usedPorts := make(map[int]bool)
	for _, r := range runtimes {
		usedPorts[r.Port] = true
	}
	for port := basePort; port < basePort+100; port++ {
		if !usedPorts[port] {
			return port
		}
	}
	return basePort + len(runtimes)
}

func (s *Service) allocateFrankenPort() int {
	basePort := 9100
	var sites []PHPSite
	s.db.Where("runtime_type = ? AND port > 0", string(RuntimeFranken)).Select("port").Find(&sites)
	usedPorts := make(map[int]bool)
	for _, site := range sites {
		usedPorts[site.Port] = true
	}
	for port := basePort; port < basePort+1000; port++ {
		if !usedPorts[port] {
			return port
		}
	}
	return basePort + len(sites)
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
