package deploy

import (
	"bytes"
	"crypto/hmac"
	"errors"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/web-casa/webcasa/plugins/deploy/builders"
)

// Handler provides HTTP handlers for the deploy plugin API.
type Handler struct {
	svc *Service
}

// NewHandler creates a new deploy handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListProjects GET /api/plugins/deploy/projects
func (h *Handler) ListProjects(c *gin.Context) {
	projects, err := h.svc.ListProjects()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, projects)
}

// GetProject GET /api/plugins/deploy/projects/:id
func (h *Handler) GetProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	project, err := h.svc.GetProject(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	// Mask env var values for non-admin users (viewers/operators can see keys but not values).
	role, _ := c.Get("user_role")
	if role != "admin" && role != "owner" {
		for i := range project.EnvVarList {
			project.EnvVarList[i].Value = "***"
		}
	}
	c.JSON(http.StatusOK, project)
}

// CreateProject POST /api/plugins/deploy/projects
func (h *Handler) CreateProject(c *gin.Context) {
	var req struct {
		Name         string   `json:"name" binding:"required"`
		Domain       string   `json:"domain"`
		GitURL       string   `json:"git_url" binding:"required"`
		GitBranch    string   `json:"git_branch"`
		DeployKey    string   `json:"deploy_key"`
		Framework    string   `json:"framework"`
		BuildCommand string   `json:"build_command"`
		StartCommand string   `json:"start_command"`
		InstallCmd   string   `json:"install_command"`
		Port         int      `json:"port"`
		AutoDeploy   bool     `json:"auto_deploy"`
		EnvVars      []EnvVar `json:"env_vars"`
		DeployMode         string `json:"deploy_mode"` // bare | docker
		HealthCheckPath    string `json:"health_check_path"`
		HealthCheckTimeout int    `json:"health_check_timeout"`
		HealthCheckRetries int    `json:"health_check_retries"`
		MemoryLimit        int    `json:"memory_limit"`
		CPULimit           int    `json:"cpu_limit"`
		BuildTimeout       int    `json:"build_timeout"`
		// GitHub App auth fields
		AuthMethod           string `json:"auth_method"`
		GitHubAppID          int64  `json:"github_app_id"`
		GitHubPrivateKey     string `json:"github_private_key"`
		GitHubInstallationID int64  `json:"github_installation_id"`
		// GitHub OAuth fields
		GitHubOAuthInstallID uint   `json:"github_oauth_install_id"`
		GitHubRepoFullName   string `json:"github_repo_full_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	branch := req.GitBranch
	if branch == "" {
		branch = "main"
	}

	project := &Project{
		Name:                 req.Name,
		Domain:               req.Domain,
		GitURL:               req.GitURL,
		GitBranch:            branch,
		DeployKey:            req.DeployKey,
		Framework:            req.Framework,
		BuildCommand:         req.BuildCommand,
		StartCommand:         req.StartCommand,
		InstallCmd:           req.InstallCmd,
		Port:                 req.Port,
		AutoDeploy:           req.AutoDeploy,
		EnvVarList:           req.EnvVars,
		DeployMode:           req.DeployMode,
		HealthCheckPath:      req.HealthCheckPath,
		HealthCheckTimeout:   req.HealthCheckTimeout,
		HealthCheckRetries:   req.HealthCheckRetries,
		MemoryLimit:          req.MemoryLimit,
		CPULimit:             req.CPULimit,
		BuildTimeout:         req.BuildTimeout,
		AuthMethod:           req.AuthMethod,
		GitHubAppID:          req.GitHubAppID,
		GitHubPrivateKey:     req.GitHubPrivateKey,
		GitHubInstallationID: req.GitHubInstallationID,
		GitHubOAuthInstallID: req.GitHubOAuthInstallID,
		GitHubRepoFullName:   req.GitHubRepoFullName,
	}

	if err := h.svc.CreateProject(project); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, project)
}

// UpdateProject PUT /api/plugins/deploy/projects/:id
func (h *Handler) UpdateProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Allowlist: only permit safe fields to be updated.
	allowed := map[string]bool{
		"name": true, "domain": true, "git_url": true, "git_branch": true,
		"deploy_key": true, "framework": true, "build_command": true,
		"start_command": true, "install_command": true, "port": true,
		"auto_deploy": true, "env_vars": true, "deploy_mode": true,
		"health_check_path": true, "health_check_timeout": true, "health_check_retries": true,
		"health_check_method": true, "health_check_expect_code": true,
		"health_check_expect_body": true, "health_check_start_period": true,
		"memory_limit": true, "cpu_limit": true, "build_timeout": true, "build_type": true,
		"auth_method": true, "github_app_id": true, "webhook_secret": true,
		"github_private_key": true, "github_installation_id": true,
		"github_oauth_install_id": true, "github_repo_full_name": true,
		"preview_enabled": true, "preview_expiry": true, "github_token": true,
	}
	filtered := make(map[string]interface{})
	for k, v := range req {
		if allowed[k] {
			filtered[k] = v
		}
	}

	// Validate build_type against allowlist.
	if bt, ok := filtered["build_type"]; ok {
		btStr, isStr := bt.(string)
		if !isStr || !builders.ValidBuilderTypes[btStr] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid build_type: must be dockerfile, nixpacks, paketo, railpack, static, auto, or empty"})
			return
		}
	}

	// Handle env_vars specially: convert from JSON array to []EnvVar then to JSON string
	if raw, ok := filtered["env_vars"]; ok {
		data, _ := json.Marshal(raw)
		var envVars []EnvVar
		if err := json.Unmarshal(data, &envVars); err == nil {
			filtered["env_vars"] = envVars
		}
	}

	if err := h.svc.UpdateProject(id, filtered); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteProject DELETE /api/plugins/deploy/projects/:id
func (h *Handler) DeleteProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.DeleteProject(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// BuildProject POST /api/plugins/deploy/projects/:id/build
func (h *Handler) BuildProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.Build(id); err != nil {
		if errors.Is(err, ErrBuildCoalesced) {
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "build coalesced into queued request"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "build started"})
}

// StartProject POST /api/plugins/deploy/projects/:id/start
func (h *Handler) StartProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.StartProject(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// StopProject POST /api/plugins/deploy/projects/:id/stop
func (h *Handler) StopProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.StopProject(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RollbackProject POST /api/plugins/deploy/projects/:id/rollback
func (h *Handler) RollbackProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req struct {
		BuildNum int `json:"build_num" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.Rollback(id, req.BuildNum); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetDeployments GET /api/plugins/deploy/projects/:id/deployments
func (h *Handler) GetDeployments(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	deployments, err := h.svc.GetDeployments(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, deployments)
}

// GetBuildLog GET /api/plugins/deploy/projects/:id/logs
func (h *Handler) GetBuildLog(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}

	logType := c.DefaultQuery("type", "build")
	buildNum, _ := strconv.Atoi(c.DefaultQuery("build", "0"))

	if logType == "runtime" {
		lines, _ := strconv.Atoi(c.DefaultQuery("lines", "200"))
		log, err := h.svc.GetRuntimeLog(id, lines)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"log": log, "type": "runtime"})
		return
	}

	// Build log
	if buildNum == 0 {
		// Get current build number
		project, err := h.svc.GetProject(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		buildNum = project.CurrentBuild
	}

	log, err := h.svc.GetBuildLog(id, buildNum)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "log not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"log": log, "type": "build", "build_num": buildNum})
}

// Webhook POST /api/plugins/deploy/webhook/:token
func (h *Handler) Webhook(c *gin.Context) {
	token := c.Param("token")

	// Look up the project to check for HMAC secret.
	var project Project
	if err := h.svc.db.Where("webhook_token = ?", token).First(&project).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	// Verify webhook signature if the project has a webhook secret configured.
	if project.WebhookSecret != "" {
		// Decrypt the stored secret (it's AES-GCM encrypted).
		secret, decErr := h.svc.decryptField(project.WebhookSecret)
		if decErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt webhook secret"})
			return
		}

		body, _ := io.ReadAll(io.LimitReader(c.Request.Body, 1024*1024)) // 1MB cap
		// Restore body so downstream code can re-read it.
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		verified := false

		// GitHub: HMAC-SHA256 signature in X-Hub-Signature-256 header.
		if sig := c.GetHeader("X-Hub-Signature-256"); sig != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
			if hmac.Equal([]byte(sig), []byte(expected)) {
				verified = true
			}
		}

		// GitLab: plain secret token comparison in X-Gitlab-Token header.
		if !verified {
			if tok := c.GetHeader("X-Gitlab-Token"); tok != "" {
				if subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1 {
					verified = true
				}
			}
		}

		if !verified {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing webhook signature"})
			return
		}
	}

	if !project.AutoDeploy {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auto-deploy is disabled"})
		return
	}

	if err := h.svc.Build(project.ID); err != nil {
		if errors.Is(err, ErrBuildCoalesced) {
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "build coalesced into queued request"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "build triggered"})
}

// DetectFramework GET /api/plugins/deploy/detect
func (h *Handler) DetectFramework(c *gin.Context) {
	url := c.Query("url")
	branch := c.DefaultQuery("branch", "main")
	if url == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	preset, err := DetectFrameworkFromURL(url, branch)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, preset)
}

// GetFrameworks GET /api/plugins/deploy/frameworks
func (h *Handler) GetFrameworks(c *gin.Context) {
	presets := make([]FrameworkPreset, 0, len(frameworkPresets))
	for _, p := range frameworkPresets {
		presets = append(presets, p)
	}
	c.JSON(http.StatusOK, presets)
}

// GetWebhookInfo GET /api/plugins/deploy/projects/:id/webhook (admin only)
// Returns the webhook token so the admin can set up Git hooks.
func (h *Handler) GetWebhookInfo(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var project Project
	if err := h.svc.db.Select("id, webhook_token").First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook_token": project.WebhookToken})
}

// ClearCache DELETE /api/plugins/deploy/projects/:id/cache
func (h *Handler) ClearCache(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.ClearCache(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetCacheInfo GET /api/plugins/deploy/projects/:id/cache
func (h *Handler) GetCacheInfo(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	size := h.svc.GetCacheSize(id)
	c.JSON(http.StatusOK, gin.H{"size": size})
}

// SuggestEnv GET /api/plugins/deploy/suggest-env?framework=nextjs
func (h *Handler) SuggestEnv(c *gin.Context) {
	framework := c.Query("framework")
	if framework == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "framework is required"})
		return
	}
	suggestions := GetEnvSuggestions(framework)
	if suggestions == nil {
		suggestions = []EnvVarSuggestion{}
	}
	c.JSON(http.StatusOK, suggestions)
}

// CloneEnvVars POST /api/plugins/deploy/projects/:id/clone-env
func (h *Handler) CloneEnvVars(c *gin.Context) {
	targetID, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req struct {
		SourceID uint `json:"source_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.CloneEnvVars(req.SourceID, targetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ---- CronJob Handlers ----

// ListCronJobs GET /api/plugins/deploy/projects/:id/crons
func (h *Handler) ListCronJobs(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	jobs, err := h.svc.ListCronJobs(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, jobs)
}

// CreateCronJob POST /api/plugins/deploy/projects/:id/crons
func (h *Handler) CreateCronJob(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req struct {
		Name     string `json:"name" binding:"required"`
		Schedule string `json:"schedule" binding:"required"`
		Command  string `json:"command" binding:"required"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	job := &CronJob{
		ProjectID: id,
		Name:      req.Name,
		Schedule:  req.Schedule,
		Command:   req.Command,
		Enabled:   enabled,
	}
	if err := h.svc.CreateCronJob(job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, job)
}

// UpdateCronJob PUT /api/plugins/deploy/projects/:id/crons/:cronId
func (h *Handler) UpdateCronJob(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	cronID, err := strconv.ParseUint(c.Param("cronId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cronId"})
		return
	}
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	allowed := map[string]bool{"name": true, "schedule": true, "command": true, "enabled": true}
	filtered := make(map[string]interface{})
	for k, v := range req {
		if allowed[k] {
			filtered[k] = v
		}
	}
	if err := h.svc.UpdateCronJob(id, uint(cronID), filtered); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteCronJob DELETE /api/plugins/deploy/projects/:id/crons/:cronId
func (h *Handler) DeleteCronJob(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	cronID, err := strconv.ParseUint(c.Param("cronId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cronId"})
		return
	}
	if err := h.svc.DeleteCronJob(id, uint(cronID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- ExtraProcess Handlers ----

// ListExtraProcesses GET /api/plugins/deploy/projects/:id/processes
func (h *Handler) ListExtraProcesses(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	procs, err := h.svc.ListExtraProcesses(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, procs)
}

// CreateExtraProcess POST /api/plugins/deploy/projects/:id/processes
func (h *Handler) CreateExtraProcess(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req struct {
		Name      string `json:"name" binding:"required"`
		Command   string `json:"command" binding:"required"`
		Instances int    `json:"instances"`
		Enabled   *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	instances := req.Instances
	if instances <= 0 {
		instances = 1
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	proc := &ExtraProcess{
		ProjectID: id,
		Name:      req.Name,
		Command:   req.Command,
		Instances: instances,
		Enabled:   enabled,
	}
	if err := h.svc.CreateExtraProcess(proc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, proc)
}

// UpdateExtraProcess PUT /api/plugins/deploy/projects/:id/processes/:procId
func (h *Handler) UpdateExtraProcess(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	procID, err := strconv.ParseUint(c.Param("procId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid procId"})
		return
	}
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	allowed := map[string]bool{"name": true, "command": true, "instances": true, "enabled": true}
	filtered := make(map[string]interface{})
	for k, v := range req {
		if allowed[k] {
			filtered[k] = v
		}
	}
	if err := h.svc.UpdateExtraProcess(id, uint(procID), filtered); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteExtraProcess DELETE /api/plugins/deploy/projects/:id/processes/:procId
func (h *Handler) DeleteExtraProcess(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	procID, err := strconv.ParseUint(c.Param("procId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid procId"})
		return
	}
	if err := h.svc.DeleteExtraProcess(id, uint(procID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RestartExtraProcess POST /api/plugins/deploy/projects/:id/processes/:procId/restart
func (h *Handler) RestartExtraProcess(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	procID, err := strconv.ParseUint(c.Param("procId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid procId"})
		return
	}
	if err := h.svc.RestartExtraProcess(id, uint(procID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── GitHub OAuth Handlers ──

// GetGitHubConfig GET /api/plugins/deploy/github/config
func (h *Handler) GetGitHubConfig(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.ghOAuth.GetConfig())
}

// SaveGitHubConfig PUT /api/plugins/deploy/github/config
func (h *Handler) SaveGitHubConfig(c *gin.Context) {
	var cfg GitHubAppConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.ghOAuth.SaveConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GitHubAuthorize GET /api/plugins/deploy/github/authorize
func (h *Handler) GitHubAuthorize(c *gin.Context) {
	// Build callback URL from the current request.
	scheme := "https"
	if c.Request.TLS == nil {
		if fwd := c.GetHeader("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		} else {
			scheme = "http"
		}
	}
	callbackURL := scheme + "://" + c.Request.Host + "/api/plugins/deploy/github/callback"

	authorizeURL, err := h.svc.ghOAuth.GetAuthorizeURL(callbackURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": authorizeURL})
}

// GitHubCallback GET /api/plugins/deploy/github/callback (PUBLIC — browser redirect from GitHub)
func (h *Handler) GitHubCallback(c *gin.Context) {
	state := c.Query("state")
	code := c.Query("code")
	installIDStr := c.Query("installation_id")

	// Validate CSRF state.
	if state == "" || !h.svc.ghOAuth.ValidateState(state) {
		c.String(http.StatusBadRequest, "Invalid or expired state parameter")
		return
	}

	installID, _ := strconv.ParseInt(installIDStr, 10, 64)

	install, err := h.svc.ghOAuth.HandleCallback(code, installID)
	if err != nil {
		h.svc.logger.Error("GitHub OAuth callback failed", "err", err)
		c.String(http.StatusInternalServerError, "GitHub OAuth error, please try again")
		return
	}

	// Redirect browser back to the panel frontend.
	redirectURL := "/#/deploy/create?github_connected=1"
	if install != nil {
		redirectURL += "&installation_id=" + strconv.FormatUint(uint64(install.ID), 10)
	}
	c.Redirect(http.StatusFound, redirectURL)
}

// ListGitHubInstallations GET /api/plugins/deploy/github/installations
func (h *Handler) ListGitHubInstallations(c *gin.Context) {
	installations, err := h.svc.ghOAuth.ListInstallations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, installations)
}

// DeleteGitHubInstallation DELETE /api/plugins/deploy/github/installations/:id
func (h *Handler) DeleteGitHubInstallation(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.ghOAuth.DeleteInstallation(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListGitHubRepos GET /api/plugins/deploy/github/installations/:id/repos
func (h *Handler) ListGitHubRepos(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}

	// Look up the installation to get the GitHub installation_id.
	var install GitHubInstallation
	if err := h.svc.db.First(&install, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "installation not found"})
		return
	}

	repos, err := h.svc.ghOAuth.ListRepos(install.InstallationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, repos)
}

func parseUintParam(c *gin.Context, name string) (uint, error) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, err
	}
	return uint(v), nil
}
