package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/web-casa/webcasa/internal/crypto"
	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// Service is the main deploy service that coordinates Git, Builder, and ProcessManager.
type Service struct {
	db       *gorm.DB
	git      *GitClient
	builder  *Builder
	proc     *ProcessManager
	docker   *DockerRunner
	health   *HealthChecker
	ports    *PortAllocator
	coreAPI  pluginpkg.CoreAPI
	eventBus *pluginpkg.EventBus
	logger   *slog.Logger
	dataDir  string
	jwtSecret string // for encrypting deploy keys and GitHub App private keys

	// Active log writers for in-progress builds (keyed by project ID)
	mu         sync.RWMutex
	activeLogs map[uint]*LogWriter

	// Build semaphores per project: channel of capacity 1 for queued builds
	buildMu   sync.Mutex
	buildSems map[uint]chan struct{}

	// GitHub App auth helper
	ghApp *GitHubAppAuth

	// GitHub OAuth service (App installation flow)
	ghOAuth     *GitHubOAuthService
	configStore *pluginpkg.ConfigStore

	// Cron scheduler
	cron *CronScheduler
}

// NewService creates a new deploy service.
func NewService(db *gorm.DB, coreAPI pluginpkg.CoreAPI, eventBus *pluginpkg.EventBus, logger *slog.Logger, dataDir string, jwtSecret string, configStore *pluginpkg.ConfigStore) *Service {
	srcDir := fmt.Sprintf("%s/sources", dataDir)
	os.MkdirAll(srcDir, 0755)

	logDir := fmt.Sprintf("%s/logs", dataDir)
	os.MkdirAll(logDir, 0755)

	git := NewGitClient(srcDir)
	svc := &Service{
		db:         db,
		git:        git,
		builder:    NewBuilder(git, dataDir),
		proc:       NewProcessManager(logDir),
		docker:     NewDockerRunner(),
		health:     NewHealthChecker(),
		ports:      NewPortAllocator(10000),
		coreAPI:    coreAPI,
		eventBus:   eventBus,
		logger:     logger,
		dataDir:    dataDir,
		jwtSecret:  jwtSecret,
		activeLogs: make(map[uint]*LogWriter),
		buildSems:  make(map[uint]chan struct{}),
		ghApp:       &GitHubAppAuth{},
		configStore: configStore,
		cron:        NewCronScheduler(db, logger, dataDir),
	}

	// Initialize GitHub OAuth service.
	svc.ghOAuth = NewGitHubOAuthService(configStore, db, jwtSecret, logger)

	// Migrate plaintext deploy keys to encrypted
	svc.migrateDeployKeys()

	return svc
}

// ListProjects returns all projects.
func (s *Service) ListProjects() ([]Project, error) {
	var projects []Project
	if err := s.db.Order("created_at desc").Find(&projects).Error; err != nil {
		return nil, err
	}
	// Resolve live status from systemd or Docker
	for i := range projects {
		if projects[i].Status == "running" {
			if projects[i].DeployMode == "docker" {
				if !s.docker.IsRunning(s.docker.ContainerName(projects[i].ID)) {
					projects[i].Status = "stopped"
				}
			} else {
				if !s.proc.IsRunning(projects[i].ID) {
					projects[i].Status = "stopped"
				}
			}
		}
	}
	return projects, nil
}

// GetProject returns a project by ID.
func (s *Service) GetProject(id uint) (*Project, error) {
	var project Project
	if err := s.db.First(&project, id).Error; err != nil {
		return nil, err
	}
	// Decode env vars
	if project.EnvVars != "" {
		json.Unmarshal([]byte(project.EnvVars), &project.EnvVarList)
	}
	// Check live status
	if project.Status == "running" {
		if project.DeployMode == "docker" {
			if !s.docker.IsRunning(s.docker.ContainerName(project.ID)) {
				project.Status = "stopped"
			}
		} else {
			if !s.proc.IsRunning(project.ID) {
				project.Status = "stopped"
			}
		}
	}
	// Populate transient fields
	project.HasDeployKey = project.DeployKey != ""
	project.HasGitHubKey = project.GitHubPrivateKey != ""
	return &project, nil
}

// CreateProject creates a new project.
func (s *Service) CreateProject(project *Project) error {
	// Generate webhook token
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return fmt.Errorf("generate webhook token: %w", err)
	}
	project.WebhookToken = hex.EncodeToString(token)

	// Encode env vars
	if len(project.EnvVarList) > 0 {
		data, _ := json.Marshal(project.EnvVarList)
		project.EnvVars = string(data)
	}

	// Encrypt deploy key before saving
	if project.DeployKey != "" {
		enc, err := crypto.Encrypt(project.DeployKey, s.jwtSecret)
		if err != nil {
			return fmt.Errorf("encrypt deploy key: %w", err)
		}
		project.DeployKey = enc
	}

	// Encrypt GitHub App private key before saving
	if project.GitHubPrivateKey != "" {
		enc, err := crypto.Encrypt(project.GitHubPrivateKey, s.jwtSecret)
		if err != nil {
			return fmt.Errorf("encrypt github private key: %w", err)
		}
		project.GitHubPrivateKey = enc
	}

	// Default auth method
	if project.AuthMethod == "" {
		project.AuthMethod = "ssh_key"
	}

	// Auto-set deploy mode for Dockerfile projects
	if project.Framework == "dockerfile" && project.DeployMode == "" {
		project.DeployMode = "docker"
	}
	if project.DeployMode == "" {
		project.DeployMode = "bare"
	}

	// Assign port if not set (both bare with start command and docker need a port)
	if project.Port == 0 && (project.StartCommand != "" || project.DeployMode == "docker") {
		// Will be assigned after DB insert (need ID)
	}

	if err := s.db.Create(project).Error; err != nil {
		return err
	}

	// Assign port based on project ID
	if project.Port == 0 && (project.StartCommand != "" || project.DeployMode == "docker") {
		project.Port = s.ports.AllocatePort(project.ID)
		s.db.Model(project).Update("port", project.Port)
	}

	return nil
}

// UpdateProject updates a project.
func (s *Service) UpdateProject(id uint, updates map[string]interface{}) error {
	// Handle env vars encoding
	if envVars, ok := updates["env_vars"]; ok {
		if list, ok := envVars.([]EnvVar); ok {
			data, _ := json.Marshal(list)
			updates["env_vars"] = string(data)
		}
	}

	// Encrypt deploy key if being updated
	if dk, ok := updates["deploy_key"]; ok {
		if keyStr, ok := dk.(string); ok && keyStr != "" {
			enc, err := crypto.Encrypt(keyStr, s.jwtSecret)
			if err != nil {
				return fmt.Errorf("encrypt deploy key: %w", err)
			}
			updates["deploy_key"] = enc
		}
	}

	// Encrypt GitHub App private key if being updated
	if pk, ok := updates["github_private_key"]; ok {
		if keyStr, ok := pk.(string); ok && keyStr != "" {
			enc, err := crypto.Encrypt(keyStr, s.jwtSecret)
			if err != nil {
				return fmt.Errorf("encrypt github private key: %w", err)
			}
			updates["github_private_key"] = enc
		}
	}

	return s.db.Model(&Project{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteProject deletes a project and cleans up resources.
func (s *Service) DeleteProject(id uint) error {
	project, err := s.GetProject(id)
	if err != nil {
		return err
	}

	// Stop process / container
	if project.DeployMode == "docker" {
		containerName := s.docker.ContainerName(id)
		s.docker.StopAndRemove(containerName)
	} else {
		s.proc.Uninstall(id)
	}

	// Delete reverse proxy host if created
	if project.HostID > 0 {
		s.coreAPI.DeleteHost(project.HostID)
		s.coreAPI.ReloadCaddy()
	}

	// Remove source code
	os.RemoveAll(s.git.ProjectDir(id))

	// Remove logs and build cache
	os.RemoveAll(s.builder.LogDir(id))
	s.builder.ClearCache(id)

	// Stop and remove extra processes
	var extraProcs []ExtraProcess
	s.db.Where("project_id = ?", id).Find(&extraProcs)
	for _, proc := range extraProcs {
		if project.DeployMode == "docker" {
			s.docker.StopExtraProcess(id, proc)
		} else {
			s.proc.UninstallExtraProcess(id, &proc)
		}
	}

	// Remove cron jobs from scheduler
	var cronJobs []CronJob
	s.db.Where("project_id = ?", id).Find(&cronJobs)
	for _, job := range cronJobs {
		s.cron.RemoveJob(job.ID)
	}

	// Delete DB records
	s.db.Where("project_id = ?", id).Delete(&CronJob{})
	s.db.Where("project_id = ?", id).Delete(&ExtraProcess{})
	s.db.Where("project_id = ?", id).Delete(&Deployment{})
	return s.db.Delete(&Project{}, id).Error
}

// CloneEnvVars copies environment variables from one project to another.
func (s *Service) CloneEnvVars(sourceID, targetID uint) error {
	source, err := s.GetProject(sourceID)
	if err != nil {
		return fmt.Errorf("source project not found: %w", err)
	}
	target, err := s.GetProject(targetID)
	if err != nil {
		return fmt.Errorf("target project not found: %w", err)
	}

	// Copy the encrypted env vars directly.
	if source.EnvVars == "" {
		return fmt.Errorf("source project has no environment variables")
	}

	_ = target // ensure target exists
	return s.db.Model(&Project{}).Where("id = ?", targetID).Update("env_vars", source.EnvVars).Error
}

// Build triggers a new build for a project (runs in background goroutine).
func (s *Service) Build(projectID uint) error {
	// Acquire per-project build semaphore. If a build is already running,
	// wait up to 5 minutes instead of immediately rejecting.
	s.buildMu.Lock()
	sem, ok := s.buildSems[projectID]
	if !ok {
		sem = make(chan struct{}, 1)
		s.buildSems[projectID] = sem
	}
	s.buildMu.Unlock()

	// Try to acquire immediately, otherwise wait with timeout.
	select {
	case sem <- struct{}{}:
		// Acquired immediately.
	default:
		// Another build is running — wait in queue.
		s.logger.Info("build queued, waiting for current build to finish", "project_id", projectID)
		timer := time.NewTimer(5 * time.Minute)
		defer timer.Stop()
		select {
		case sem <- struct{}{}:
			// Acquired after waiting.
		case <-timer.C:
			return fmt.Errorf("build queue timeout: another build is still running after 5 minutes")
		}
	}

	project, err := s.GetProject(projectID)
	if err != nil {
		s.releaseBuildLock(projectID)
		return err
	}

	// Create deployment record
	buildNum := project.CurrentBuild + 1
	deployment := &Deployment{
		ProjectID: projectID,
		BuildNum:  buildNum,
		Status:    "building",
	}
	if err := s.db.Create(deployment).Error; err != nil {
		s.releaseBuildLock(projectID)
		return err
	}

	// Update project status
	s.db.Model(&Project{}).Where("id = ?", projectID).Updates(map[string]interface{}{
		"status":        "building",
		"current_build": buildNum,
		"error_msg":     "",
	})

	// Create log file
	logDir := s.builder.LogDir(projectID)
	os.MkdirAll(logDir, 0755)
	logPath := s.builder.LogPath(projectID, buildNum)

	logWriter, err := NewLogWriter(logPath)
	if err != nil {
		s.releaseBuildLock(projectID)
		return fmt.Errorf("create log writer: %w", err)
	}

	// Store active log writer
	s.mu.Lock()
	s.activeLogs[projectID] = logWriter
	s.mu.Unlock()

	// Run build in background
	go s.runBuild(project, deployment, logWriter)

	return nil
}

// releaseBuildLock releases the per-project build lock.
func (s *Service) releaseBuildLock(projectID uint) {
	s.buildMu.Lock()
	if sem, ok := s.buildSems[projectID]; ok {
		select {
		case <-sem:
			// Released.
		default:
		}
	}
	s.buildMu.Unlock()
}

// runBuild executes the full build pipeline in background.
func (s *Service) runBuild(project *Project, deployment *Deployment, logWriter *LogWriter) {
	defer func() {
		logWriter.Close()
		s.mu.Lock()
		delete(s.activeLogs, project.ID)
		s.mu.Unlock()
		s.releaseBuildLock(project.ID)
	}()

	buildTimeout := 30 * time.Minute
	if project.BuildTimeout > 0 {
		buildTimeout = time.Duration(project.BuildTimeout) * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	// Resolve git credentials: decrypt deploy key or obtain GitHub App token.
	authMethod, deployKey, httpsToken, credErr := s.GetGitCredentials(project)
	if credErr != nil {
		logWriter.Write([]byte(fmt.Sprintf("ERROR: Failed to resolve git credentials: %v\n", credErr)))
		deployment.Status = "failed"
		s.db.Save(deployment)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": fmt.Sprintf("git credentials failed: %v", credErr),
		})
		return
	}

	// Prepare an in-memory copy of the project with resolved credentials.
	buildProject := *project
	if (authMethod == "github_app" || authMethod == "github_oauth") && httpsToken != "" {
		buildProject.GitURL = ConvertToHTTPS(project.GitURL, httpsToken)
		buildProject.DeployKey = "" // No SSH key needed for HTTPS
	} else {
		buildProject.DeployKey = deployKey // decrypted plaintext key
	}

	// Write .env file to project dir
	projectDir := s.git.ProjectDir(project.ID)
	GenerateEnvFile(projectDir, project.EnvVarList)

	result := s.builder.Build(ctx, &buildProject, logWriter)

	deployment.GitCommit = result.Commit
	deployment.Duration = int(result.Duration.Seconds())
	deployment.LogFile = s.builder.LogPath(project.ID, deployment.BuildNum)

	if !result.Success {
		deployment.Status = "failed"
		s.db.Save(deployment)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": result.ErrorMsg,
		})
		s.logger.Error("build failed", "project", project.Name, "error", result.ErrorMsg)

		// Emit build failure event for AI auto-diagnosis
		if s.eventBus != nil {
			// Read last portion of build log for diagnosis
			logContent, _ := s.builder.ReadLog(project.ID, deployment.BuildNum)
			// Truncate to last 4000 chars for efficient AI analysis
			if len(logContent) > 4000 {
				logContent = logContent[len(logContent)-4000:]
			}
			s.eventBus.Publish(pluginpkg.Event{
				Type:   "deploy.build.failed",
				Source: "deploy",
				Payload: map[string]interface{}{
					"project_id":   project.ID,
					"project_name": project.Name,
					"build_num":    deployment.BuildNum,
					"deployment_id": deployment.ID,
					"framework":    project.Framework,
					"error_msg":    result.ErrorMsg,
					"log_tail":     logContent,
				},
			})
		}
		return
	}

	deployment.Status = "success"
	s.db.Save(deployment)

	// Deploy based on mode
	if project.DeployMode == "docker" {
		s.runDockerDeploy(project, deployment, logWriter)
	} else {
		s.runBareDeploy(project, projectDir, logWriter)
	}

	// Start extra processes after successful deploy
	s.StartExtraProcesses(project)

	s.logger.Info("build completed", "project", project.Name, "build", deployment.BuildNum, "duration", result.Duration)
}

// setupReverseProxy creates a Caddy reverse proxy entry for the project.
func (s *Service) setupReverseProxy(project *Project) {
	hostID, err := s.coreAPI.CreateHost(pluginpkg.CreateHostRequest{
		Domain:       project.Domain,
		UpstreamAddr: fmt.Sprintf("localhost:%d", project.Port),
		TLSEnabled:   true,
		HTTPRedirect: true,
		WebSocket:    true,
	})
	if err != nil {
		s.logger.Error("create host failed", "project", project.Name, "error", err)
		return
	}
	s.db.Model(&Project{}).Where("id = ?", project.ID).Update("host_id", hostID)
	s.coreAPI.ReloadCaddy()
	s.logger.Info("reverse proxy created", "project", project.Name, "domain", project.Domain)
}

// updateUpstream switches the Caddy reverse proxy upstream to the new port and reloads.
func (s *Service) updateUpstream(project *Project, newPort int) {
	if project.HostID == 0 || project.Port == newPort {
		return
	}
	newUpstream := fmt.Sprintf("localhost:%d", newPort)
	if err := s.coreAPI.UpdateHostUpstream(project.HostID, newUpstream); err != nil {
		s.logger.Error("update upstream failed", "project", project.Name, "error", err)
		return
	}
	if err := s.coreAPI.ReloadCaddy(); err != nil {
		s.logger.Error("reload caddy failed after upstream update", "project", project.Name, "error", err)
	}
}

// runBareDeploy handles post-build deploy for bare (systemd) mode with zero-downtime support.
func (s *Service) runBareDeploy(project *Project, projectDir string, logWriter *LogWriter) {
	if project.StartCommand == "" {
		s.db.Model(&Project{}).Where("id = ?", project.ID).Update("status", "running")
		logWriter.Write([]byte("Static build complete.\n"))
		return
	}

	logWriter.Write([]byte("\n=== Starting process ===\n"))

	isFirstDeploy := !s.proc.IsRunning(project.ID) || project.HostID == 0

	if isFirstDeploy {
		// First deploy: simple install + start (no zero-downtime needed)
		if err := s.proc.Install(project, projectDir); err != nil {
			s.logger.Error("install service failed", "project", project.Name, "error", err)
			s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
				"status":    "error",
				"error_msg": fmt.Sprintf("service install failed: %v", err),
			})
			return
		}

		if err := s.proc.Restart(project.ID); err != nil {
			s.logger.Error("start service failed", "project", project.Name, "error", err)
			s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
				"status":    "error",
				"error_msg": fmt.Sprintf("service start failed: %v", err),
			})
			return
		}

		time.Sleep(2 * time.Second)
		if !s.proc.IsRunning(project.ID) {
			s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
				"status":    "error",
				"error_msg": "process exited shortly after start",
			})
			logWriter.Write([]byte("ERROR: Process exited shortly after start.\n"))
			return
		}

		s.runHealthCheck(project, project.Port, logWriter)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Update("status", "running")
		logWriter.Write([]byte("Process started successfully.\n"))

		if project.Domain != "" && project.HostID == 0 {
			s.setupReverseProxy(project)
		}
		return
	}

	// Zero-downtime deploy: staging service on alternate port
	newPort := s.ports.AlternatePort(project.Port, project.ID)
	logWriter.Write([]byte(fmt.Sprintf("==> Zero-downtime: staging on port %d\n", newPort)))

	if err := s.proc.InstallStaging(project, projectDir, newPort); err != nil {
		s.logger.Error("install staging service failed", "project", project.Name, "error", err)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": fmt.Sprintf("staging service install failed: %v", err),
		})
		return
	}

	if err := s.proc.StartStaging(project.ID); err != nil {
		s.proc.CleanupStaging(project.ID)
		s.logger.Error("start staging service failed", "project", project.Name, "error", err)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": fmt.Sprintf("staging service start failed: %v", err),
		})
		return
	}

	time.Sleep(2 * time.Second)
	if !s.proc.IsStagingRunning(project.ID) {
		s.proc.CleanupStaging(project.ID)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": "staging process exited shortly after start",
		})
		logWriter.Write([]byte("ERROR: Staging process exited shortly after start.\n"))
		return
	}

	// Health check on the new port
	if !s.runHealthCheck(project, newPort, logWriter) {
		s.proc.CleanupStaging(project.ID)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": "staging process health check failed",
		})
		return
	}

	// Switch traffic: update Caddy upstream to new port
	logWriter.Write([]byte(fmt.Sprintf("==> Switching traffic: port %d -> %d\n", project.Port, newPort)))
	s.updateUpstream(project, newPort)

	// Promote staging: stop old service, rename staging to main
	if err := s.proc.PromoteStaging(project.ID); err != nil {
		s.logger.Error("promote staging failed", "project", project.Name, "error", err)
	}

	s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"status": "running",
		"port":   newPort,
	})
	logWriter.Write([]byte(fmt.Sprintf("Zero-downtime deploy complete. Now running on port %d.\n", newPort)))
}

// runHealthCheck performs HTTP health check on the given port. Returns true if healthy.
func (s *Service) runHealthCheck(project *Project, port int, logWriter *LogWriter) bool {
	if port <= 0 {
		return true
	}
	hcPath := project.HealthCheckPath
	if hcPath == "" {
		hcPath = "/"
	}
	// Validate path to prevent SSRF via protocol injection or path traversal.
	if !strings.HasPrefix(hcPath, "/") {
		hcPath = "/" + hcPath
	}
	hcTimeout := time.Duration(project.HealthCheckTimeout) * time.Second
	if hcTimeout <= 0 {
		hcTimeout = 30 * time.Second
	}
	hcRetries := project.HealthCheckRetries
	if hcRetries <= 0 {
		hcRetries = 3
	}

	logWriter.Write([]byte(fmt.Sprintf("==> Health check: GET http://127.0.0.1:%d%s (retries=%d, timeout=%s)\n", port, hcPath, hcRetries, hcTimeout)))
	if err := s.health.WaitHealthy(port, hcPath, hcRetries, hcTimeout); err != nil {
		logWriter.Write([]byte(fmt.Sprintf("WARNING: Health check failed: %v\n", err)))
		return false
	}
	logWriter.Write([]byte("==> Health check passed.\n"))
	return true
}

// runDockerDeploy handles post-build deploy for Docker mode with zero-downtime support.
func (s *Service) runDockerDeploy(project *Project, deployment *Deployment, logWriter *LogWriter) {
	projectDir := s.git.ProjectDir(project.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Step 1: Build Docker image
	logWriter.Write([]byte("\n=== Docker: Building image ===\n"))
	imageTag, err := s.docker.BuildImage(ctx, projectDir, project.ID, deployment.BuildNum, logWriter)
	if err != nil {
		s.logger.Error("docker build failed", "project", project.Name, "error", err)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": fmt.Sprintf("docker build failed: %v", err),
		})
		return
	}

	// Decode env vars
	var envVars []EnvVar
	if len(project.EnvVarList) > 0 {
		envVars = project.EnvVarList
	} else if project.EnvVars != "" {
		json.Unmarshal([]byte(project.EnvVars), &envVars)
	}

	runOpts := RunOptions{
		MemoryLimitMB: project.MemoryLimit,
		CPULimitPct:   project.CPULimit,
	}

	isFirstDeploy := !s.docker.IsRunning(s.docker.ContainerName(project.ID)) || project.HostID == 0

	if isFirstDeploy {
		// First deploy: simple run (no zero-downtime needed)
		if project.Port == 0 {
			project.Port = s.ports.AllocatePort(project.ID)
			s.db.Model(&Project{}).Where("id = ?", project.ID).Update("port", project.Port)
		}

		logWriter.Write([]byte(fmt.Sprintf("\n=== Docker: Starting container (port %d) ===\n", project.Port)))
		containerID, err := s.docker.Run(ctx, project.ID, imageTag, project.Port, envVars, runOpts)
		if err != nil {
			s.logger.Error("docker run failed", "project", project.Name, "error", err)
			s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
				"status":    "error",
				"error_msg": fmt.Sprintf("docker run failed: %v", err),
			})
			return
		}

		time.Sleep(3 * time.Second)
		containerName := s.docker.ContainerName(project.ID)
		if !s.docker.IsRunning(containerName) {
			logs, _ := s.docker.Logs(containerName, 20)
			errMsg := "container exited shortly after start"
			if logs != "" {
				errMsg += "\n" + logs
			}
			s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
				"status":    "error",
				"error_msg": errMsg,
			})
			logWriter.Write([]byte(fmt.Sprintf("ERROR: Container exited shortly after start.\n%s\n", logs)))
			return
		}

		s.runHealthCheck(project, project.Port, logWriter)

		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":         "running",
			"docker_image":   imageTag,
			"container_id":   containerID,
			"container_name": containerName,
		})
		logWriter.Write([]byte(fmt.Sprintf("Container %s started successfully.\n", containerName)))

		if project.Domain != "" && project.HostID == 0 {
			s.setupReverseProxy(project)
		}
		return
	}

	// Zero-downtime deploy: staging container on alternate port
	newPort := s.ports.AlternatePort(project.Port, project.ID)
	logWriter.Write([]byte(fmt.Sprintf("\n=== Docker: Zero-downtime deploy (staging port %d) ===\n", newPort)))

	containerID, err := s.docker.RunStaging(ctx, project.ID, imageTag, newPort, envVars, runOpts)
	if err != nil {
		s.logger.Error("docker run staging failed", "project", project.Name, "error", err)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": fmt.Sprintf("docker run staging failed: %v", err),
		})
		return
	}

	time.Sleep(3 * time.Second)
	stagingName := s.docker.StagingContainerName(project.ID)
	if !s.docker.IsRunning(stagingName) {
		logs, _ := s.docker.Logs(stagingName, 20)
		s.docker.StopAndRemove(stagingName)
		errMsg := "staging container exited shortly after start"
		if logs != "" {
			errMsg += "\n" + logs
		}
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": errMsg,
		})
		logWriter.Write([]byte(fmt.Sprintf("ERROR: Staging container exited shortly after start.\n%s\n", logs)))
		return
	}

	// Health check on staging container
	if !s.runHealthCheck(project, newPort, logWriter) {
		s.docker.StopAndRemove(stagingName)
		s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"status":    "error",
			"error_msg": "staging container health check failed",
		})
		return
	}

	// Switch traffic: update Caddy upstream
	logWriter.Write([]byte(fmt.Sprintf("==> Switching traffic: port %d -> %d\n", project.Port, newPort)))
	s.updateUpstream(project, newPort)

	// Stop and remove old container, rename staging to main
	oldContainerName := s.docker.ContainerName(project.ID)
	s.docker.StopAndRemove(oldContainerName)

	mainContainerName := s.docker.ContainerName(project.ID)
	if err := s.docker.Rename(stagingName, mainContainerName); err != nil {
		s.logger.Warn("rename staging container failed (non-critical)", "error", err)
	}

	s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"status":         "running",
		"port":           newPort,
		"docker_image":   imageTag,
		"container_id":   containerID,
		"container_name": mainContainerName,
	})
	logWriter.Write([]byte(fmt.Sprintf("Zero-downtime deploy complete. Now running on port %d.\n", newPort)))
}

// StartProject starts the project process (without rebuilding).
func (s *Service) StartProject(id uint) error {
	project, err := s.GetProject(id)
	if err != nil {
		return err
	}

	if project.DeployMode == "docker" {
		containerName := s.docker.ContainerName(id)
		if err := s.docker.Start(containerName); err != nil {
			return err
		}
	} else {
		if project.StartCommand == "" {
			return fmt.Errorf("project has no start command")
		}
		if err := s.proc.Start(id); err != nil {
			return err
		}
	}
	return s.db.Model(&Project{}).Where("id = ?", id).Update("status", "running").Error
}

// StopProject stops the project process.
func (s *Service) StopProject(id uint) error {
	project, err := s.GetProject(id)
	if err != nil {
		return err
	}

	if project.DeployMode == "docker" {
		containerName := s.docker.ContainerName(id)
		if err := s.docker.Stop(containerName); err != nil {
			return err
		}
	} else {
		if err := s.proc.Stop(id); err != nil {
			return err
		}
	}
	return s.db.Model(&Project{}).Where("id = ?", id).Update("status", "stopped").Error
}

// Rollback rolls back to a previous build version.
func (s *Service) Rollback(projectID uint, buildNum int) error {
	var deployment Deployment
	if err := s.db.Where("project_id = ? AND build_num = ? AND status = ?", projectID, buildNum, "success").First(&deployment).Error; err != nil {
		return fmt.Errorf("deployment not found or was not successful")
	}

	project, err := s.GetProject(projectID)
	if err != nil {
		return err
	}

	// Mark newer deployments as rolled back
	s.db.Model(&Deployment{}).Where("project_id = ? AND build_num > ?", projectID, buildNum).Update("status", "rolled_back")
	s.db.Model(&Project{}).Where("id = ?", projectID).Update("current_build", buildNum)

	if project.DeployMode == "docker" {
		// For Docker mode: run the older image tag
		imageTag := s.docker.ImageTag(projectID, buildNum)

		var envVars []EnvVar
		if project.EnvVars != "" {
			json.Unmarshal([]byte(project.EnvVars), &envVars)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		runOpts := RunOptions{
			MemoryLimitMB: project.MemoryLimit,
			CPULimitPct:   project.CPULimit,
		}
		containerID, runErr := s.docker.Run(ctx, projectID, imageTag, project.Port, envVars, runOpts)
		if runErr != nil {
			return fmt.Errorf("docker rollback failed: %w", runErr)
		}
		containerName := s.docker.ContainerName(projectID)
		s.db.Model(&Project{}).Where("id = ?", projectID).Updates(map[string]interface{}{
			"status":         "running",
			"docker_image":   imageTag,
			"container_id":   containerID,
			"container_name": containerName,
		})
		return nil
	}

	// Bare mode: restart the process
	return s.proc.Restart(projectID)
}

// GetDeployments returns all deployments for a project.
func (s *Service) GetDeployments(projectID uint) ([]Deployment, error) {
	var deployments []Deployment
	err := s.db.Where("project_id = ?", projectID).Order("build_num desc").Find(&deployments).Error
	return deployments, err
}

// GetBuildLog returns the log content for a specific build.
func (s *Service) GetBuildLog(projectID uint, buildNum int) (string, error) {
	return s.builder.ReadLog(projectID, buildNum)
}

// GetRuntimeLog returns recent runtime log lines.
func (s *Service) GetRuntimeLog(projectID uint, lines int) (string, error) {
	var project Project
	if err := s.db.Select("deploy_mode").First(&project, projectID).Error; err != nil {
		return "", err
	}
	if project.DeployMode == "docker" {
		containerName := s.docker.ContainerName(projectID)
		return s.docker.Logs(containerName, lines)
	}
	return s.proc.ReadRuntimeLog(projectID, lines)
}

// GetActiveLogWriter returns the log writer for an in-progress build (for WebSocket streaming).
func (s *Service) GetActiveLogWriter(projectID uint) *LogWriter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeLogs[projectID]
}

// HandleWebhook processes a Git webhook trigger.
func (s *Service) HandleWebhook(token string) error {
	var project Project
	if err := s.db.Where("webhook_token = ? AND auto_deploy = ?", token, true).First(&project).Error; err != nil {
		return fmt.Errorf("project not found or auto-deploy disabled")
	}
	return s.Build(project.ID)
}

// DecryptDeployKey decrypts the project's stored deploy key.
func (s *Service) DecryptDeployKey(project *Project) (string, error) {
	if project.DeployKey == "" {
		return "", nil
	}
	decrypted, err := crypto.Decrypt(project.DeployKey, s.jwtSecret)
	if err != nil {
		// Might be a plaintext key from before migration
		if strings.HasPrefix(project.DeployKey, "-----") {
			return project.DeployKey, nil
		}
		return "", fmt.Errorf("decrypt deploy key: %w", err)
	}
	return decrypted, nil
}

// GetGitCredentials resolves the appropriate git credentials for a project.
// For ssh_key: returns decrypted deploy key (used via GIT_SSH_COMMAND).
// For github_app: obtains a GitHub App installation token for HTTPS cloning.
func (s *Service) GetGitCredentials(project *Project) (authMethod string, deployKey string, httpsToken string, err error) {
	authMethod = project.AuthMethod
	if authMethod == "" {
		authMethod = "ssh_key"
	}

	switch authMethod {
	case "github_app":
		if project.GitHubAppID == 0 || project.GitHubInstallationID == 0 || project.GitHubPrivateKey == "" {
			return authMethod, "", "", fmt.Errorf("GitHub App credentials incomplete")
		}
		// Decrypt GitHub App private key
		pemKey, decErr := crypto.Decrypt(project.GitHubPrivateKey, s.jwtSecret)
		if decErr != nil {
			return authMethod, "", "", fmt.Errorf("decrypt GitHub App key: %w", decErr)
		}
		// Get installation token
		token, tokenErr := s.ghApp.GetCloneToken(project.GitHubAppID, pemKey, project.GitHubInstallationID)
		if tokenErr != nil {
			return authMethod, "", "", fmt.Errorf("get GitHub App token: %w", tokenErr)
		}
		return authMethod, "", token, nil

	case "github_oauth":
		if project.GitHubOAuthInstallID == 0 {
			return authMethod, "", "", fmt.Errorf("GitHub OAuth installation not linked")
		}
		// Look up the installation record.
		var install GitHubInstallation
		if err := s.db.First(&install, project.GitHubOAuthInstallID).Error; err != nil {
			return authMethod, "", "", fmt.Errorf("GitHub installation not found: %w", err)
		}
		// Get installation token using global App credentials.
		token, tokenErr := s.ghOAuth.GetInstallationToken(install.InstallationID)
		if tokenErr != nil {
			return authMethod, "", "", fmt.Errorf("get GitHub OAuth token: %w", tokenErr)
		}
		return authMethod, "", token, nil

	default: // ssh_key
		dk, decErr := s.DecryptDeployKey(project)
		if decErr != nil {
			return authMethod, "", "", decErr
		}
		return authMethod, dk, "", nil
	}
}

// ClearCache clears the build cache for a project.
func (s *Service) ClearCache(projectID uint) error {
	return s.builder.ClearCache(projectID)
}

// GetCacheSize returns the cache size for a project in bytes.
func (s *Service) GetCacheSize(projectID uint) int64 {
	return s.builder.CacheSize(projectID)
}

// ---- CronJob CRUD ----

// ListCronJobs returns all cron jobs for a project.
func (s *Service) ListCronJobs(projectID uint) ([]CronJob, error) {
	var jobs []CronJob
	err := s.db.Where("project_id = ?", projectID).Order("created_at desc").Find(&jobs).Error
	return jobs, err
}

// CreateCronJob creates a new cron job and registers it with the scheduler.
func (s *Service) CreateCronJob(job *CronJob) error {
	if err := s.db.Create(job).Error; err != nil {
		return err
	}
	if job.Enabled {
		s.cron.AddJob(*job)
	}
	return nil
}

// UpdateCronJob updates a cron job and re-registers it with the scheduler.
// projectID is used to verify the job belongs to the specified project.
func (s *Service) UpdateCronJob(projectID, jobID uint, updates map[string]interface{}) error {
	result := s.db.Model(&CronJob{}).Where("id = ? AND project_id = ?", jobID, projectID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("cron job not found or does not belong to this project")
	}
	var job CronJob
	if err := s.db.First(&job, jobID).Error; err != nil {
		return err
	}
	s.cron.AddJob(job) // re-register (handles enable/disable)
	return nil
}

// DeleteCronJob deletes a cron job and removes it from the scheduler.
// projectID is used to verify the job belongs to the specified project.
func (s *Service) DeleteCronJob(projectID, jobID uint) error {
	result := s.db.Where("id = ? AND project_id = ?", jobID, projectID).Delete(&CronJob{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("cron job not found or does not belong to this project")
	}
	s.cron.RemoveJob(jobID)
	return nil
}

// ---- ExtraProcess CRUD ----

// ListExtraProcesses returns all extra processes for a project.
func (s *Service) ListExtraProcesses(projectID uint) ([]ExtraProcess, error) {
	var procs []ExtraProcess
	err := s.db.Where("project_id = ?", projectID).Order("created_at desc").Find(&procs).Error
	return procs, err
}

// CreateExtraProcess creates a new extra process.
func (s *Service) CreateExtraProcess(proc *ExtraProcess) error {
	return s.db.Create(proc).Error
}

// UpdateExtraProcess updates an extra process.
// projectID is used to verify the process belongs to the specified project.
func (s *Service) UpdateExtraProcess(projectID, procID uint, updates map[string]interface{}) error {
	result := s.db.Model(&ExtraProcess{}).Where("id = ? AND project_id = ?", procID, projectID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("extra process not found or does not belong to this project")
	}
	return nil
}

// DeleteExtraProcess stops and deletes an extra process.
// projectID is used to verify the process belongs to the specified project.
func (s *Service) DeleteExtraProcess(projectID, procID uint) error {
	var proc ExtraProcess
	if err := s.db.Where("id = ? AND project_id = ?", procID, projectID).First(&proc).Error; err != nil {
		return fmt.Errorf("extra process not found or does not belong to this project")
	}
	var project Project
	if err := s.db.First(&project, proc.ProjectID).Error; err != nil {
		return err
	}
	// Stop running instances
	if project.DeployMode == "docker" {
		s.docker.StopExtraProcess(project.ID, proc)
	} else {
		s.proc.UninstallExtraProcess(project.ID, &proc)
	}
	return s.db.Delete(&ExtraProcess{}, procID).Error
}

// RestartExtraProcess restarts an extra process.
// projectID is used to verify the process belongs to the specified project.
func (s *Service) RestartExtraProcess(projectID, procID uint) error {
	var proc ExtraProcess
	if err := s.db.Where("id = ? AND project_id = ?", procID, projectID).First(&proc).Error; err != nil {
		return fmt.Errorf("extra process not found or does not belong to this project")
	}
	var project Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return err
	}
	if project.DeployMode == "docker" {
		return s.docker.RestartExtraProcess(project.ID, proc)
	}
	return s.proc.RestartExtraProcess(project.ID, &proc)
}

// StartExtraProcesses installs and starts all enabled extra processes for a project after build.
func (s *Service) StartExtraProcesses(project *Project) {
	var procs []ExtraProcess
	s.db.Where("project_id = ? AND enabled = ?", project.ID, true).Find(&procs)
	if len(procs) == 0 {
		return
	}

	projectDir := s.git.ProjectDir(project.ID)

	for _, proc := range procs {
		if project.DeployMode == "docker" {
			// For Docker: run extra process containers using the project's image
			var envVars []EnvVar
			if project.EnvVars != "" {
				json.Unmarshal([]byte(project.EnvVars), &envVars)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			if err := s.docker.RunExtraProcess(ctx, project.ID, project.DockerImage, proc, envVars); err != nil {
				s.logger.Error("start extra process (docker) failed", "project", project.Name, "process", proc.Name, "err", err)
			}
			cancel()
		} else {
			// For bare: install and start systemd services
			if err := s.proc.InstallExtraProcess(project, &proc, projectDir); err != nil {
				s.logger.Error("install extra process failed", "project", project.Name, "process", proc.Name, "err", err)
				continue
			}
			if err := s.proc.StartExtraProcess(project.ID, &proc); err != nil {
				s.logger.Error("start extra process failed", "project", project.Name, "process", proc.Name, "err", err)
			}
		}
	}
	s.logger.Info("extra processes started", "project", project.Name, "count", len(procs))
}

// StartCronScheduler starts the cron scheduler (called from plugin Start).
func (s *Service) StartCronScheduler() {
	s.cron.Start()
}

// StopCronScheduler stops the cron scheduler (called from plugin Stop).
func (s *Service) StopCronScheduler() {
	s.cron.Stop()
}

// migrateDeployKeys encrypts any plaintext deploy keys found in the database.
func (s *Service) migrateDeployKeys() {
	if s.jwtSecret == "" {
		return
	}

	var projects []Project
	s.db.Where("deploy_key != ''").Find(&projects)

	migrated := 0
	for _, p := range projects {
		// Check if the key looks like plaintext (starts with SSH key prefix)
		if strings.HasPrefix(p.DeployKey, "-----") {
			enc, err := crypto.Encrypt(p.DeployKey, s.jwtSecret)
			if err != nil {
				s.logger.Error("migrate deploy key failed", "project", p.ID, "error", err)
				continue
			}
			s.db.Model(&Project{}).Where("id = ?", p.ID).Update("deploy_key", enc)
			migrated++
		}
	}
	if migrated > 0 {
		s.logger.Info("migrated plaintext deploy keys to encrypted", "count", migrated)
	}
}
