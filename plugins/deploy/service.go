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
	db      *gorm.DB
	git     *GitClient
	builder *Builder
	proc    *ProcessManager
	ports   *PortAllocator
	coreAPI pluginpkg.CoreAPI
	logger  *slog.Logger
	dataDir string
	jwtSecret string // for encrypting deploy keys and GitHub App private keys

	// Active log writers for in-progress builds (keyed by project ID)
	mu         sync.RWMutex
	activeLogs map[uint]*LogWriter

	// Build locks per project to prevent concurrent builds
	buildMu    sync.Mutex
	buildLocks map[uint]bool

	// GitHub App auth helper
	ghApp *GitHubAppAuth
}

// NewService creates a new deploy service.
func NewService(db *gorm.DB, coreAPI pluginpkg.CoreAPI, logger *slog.Logger, dataDir string, jwtSecret string) *Service {
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
		ports:      NewPortAllocator(10000),
		coreAPI:    coreAPI,
		logger:     logger,
		dataDir:    dataDir,
		jwtSecret:  jwtSecret,
		activeLogs: make(map[uint]*LogWriter),
		buildLocks: make(map[uint]bool),
		ghApp:      &GitHubAppAuth{},
	}

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
	// Resolve live status from systemd
	for i := range projects {
		if projects[i].Status == "running" && !s.proc.IsRunning(projects[i].ID) {
			projects[i].Status = "stopped"
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
	if project.Status == "running" && !s.proc.IsRunning(project.ID) {
		project.Status = "stopped"
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

	// Assign port if not set
	if project.Port == 0 && project.StartCommand != "" {
		// Will be assigned after DB insert (need ID)
	}

	if err := s.db.Create(project).Error; err != nil {
		return err
	}

	// Assign port based on project ID
	if project.Port == 0 && project.StartCommand != "" {
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

	// Stop process
	s.proc.Uninstall(id)

	// Delete reverse proxy host if created
	if project.HostID > 0 {
		s.coreAPI.DeleteHost(project.HostID)
		s.coreAPI.ReloadCaddy()
	}

	// Remove source code
	os.RemoveAll(s.git.ProjectDir(id))

	// Remove logs
	os.RemoveAll(s.builder.LogDir(id))

	// Delete DB records
	s.db.Where("project_id = ?", id).Delete(&Deployment{})
	return s.db.Delete(&Project{}, id).Error
}

// Build triggers a new build for a project (runs in background goroutine).
func (s *Service) Build(projectID uint) error {
	// Acquire per-project build lock to prevent concurrent builds.
	s.buildMu.Lock()
	if s.buildLocks[projectID] {
		s.buildMu.Unlock()
		return fmt.Errorf("project is already building")
	}
	s.buildLocks[projectID] = true
	s.buildMu.Unlock()

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
	delete(s.buildLocks, projectID)
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
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
	if authMethod == "github_app" && httpsToken != "" {
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
		return
	}

	deployment.Status = "success"
	s.db.Save(deployment)

	// If project has a start command, install/update systemd service and start
	if project.StartCommand != "" {
		logWriter.Write([]byte("\n=== Starting process ===\n"))

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

		// Wait a moment and check if it's running
		time.Sleep(2 * time.Second)
		if s.proc.IsRunning(project.ID) {
			s.db.Model(&Project{}).Where("id = ?", project.ID).Update("status", "running")
			logWriter.Write([]byte("Process started successfully.\n"))

			// Auto-create reverse proxy if domain is set and host doesn't exist
			if project.Domain != "" && project.HostID == 0 {
				s.setupReverseProxy(project)
			}
		} else {
			s.db.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
				"status":    "error",
				"error_msg": "process exited shortly after start",
			})
			logWriter.Write([]byte("ERROR: Process exited shortly after start.\n"))
		}
	} else {
		// Static site (no start command), just mark as running
		s.db.Model(&Project{}).Where("id = ?", project.ID).Update("status", "running")
		logWriter.Write([]byte("Static build complete.\n"))
	}

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

// StartProject starts the project process (without rebuilding).
func (s *Service) StartProject(id uint) error {
	project, err := s.GetProject(id)
	if err != nil {
		return err
	}
	if project.StartCommand == "" {
		return fmt.Errorf("project has no start command")
	}
	if err := s.proc.Start(id); err != nil {
		return err
	}
	return s.db.Model(&Project{}).Where("id = ?", id).Update("status", "running").Error
}

// StopProject stops the project process.
func (s *Service) StopProject(id uint) error {
	if err := s.proc.Stop(id); err != nil {
		return err
	}
	return s.db.Model(&Project{}).Where("id = ?", id).Update("status", "stopped").Error
}

// Rollback rolls back to a previous build version.
func (s *Service) Rollback(projectID uint, buildNum int) error {
	var deployment Deployment
	if err := s.db.Where("project_id = ? AND build_num = ? AND status = ?", projectID, buildNum, "success").First(&deployment).Error; err != nil {
		return fmt.Errorf("deployment not found or was not successful")
	}

	// For now, trigger a fresh build (full rollback with version management is Phase 2.1.7 enhancement)
	// Mark the deployment as rolled back
	s.db.Model(&Deployment{}).Where("project_id = ? AND build_num > ?", projectID, buildNum).Update("status", "rolled_back")
	s.db.Model(&Project{}).Where("id = ?", projectID).Update("current_build", buildNum)

	// Restart the process
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

	default: // ssh_key
		dk, decErr := s.DecryptDeployKey(project)
		if decErr != nil {
			return authMethod, "", "", decErr
		}
		return authMethod, dk, "", nil
	}
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
