package deploy

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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
		"memory_limit": true, "cpu_limit": true, "build_timeout": true,
		"auth_method": true, "github_app_id": true,
		"github_private_key": true, "github_installation_id": true,
	}
	filtered := make(map[string]interface{})
	for k, v := range req {
		if allowed[k] {
			filtered[k] = v
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
	if err := h.svc.HandleWebhook(token); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
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
	_, err := parseUintParam(c, "id")
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
	if err := h.svc.UpdateCronJob(uint(cronID), filtered); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteCronJob DELETE /api/plugins/deploy/projects/:id/crons/:cronId
func (h *Handler) DeleteCronJob(c *gin.Context) {
	_, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	cronID, err := strconv.ParseUint(c.Param("cronId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cronId"})
		return
	}
	if err := h.svc.DeleteCronJob(uint(cronID)); err != nil {
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
	_, err := parseUintParam(c, "id")
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
	if err := h.svc.UpdateExtraProcess(uint(procID), filtered); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteExtraProcess DELETE /api/plugins/deploy/projects/:id/processes/:procId
func (h *Handler) DeleteExtraProcess(c *gin.Context) {
	_, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	procID, err := strconv.ParseUint(c.Param("procId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid procId"})
		return
	}
	if err := h.svc.DeleteExtraProcess(uint(procID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RestartExtraProcess POST /api/plugins/deploy/projects/:id/processes/:procId/restart
func (h *Handler) RestartExtraProcess(c *gin.Context) {
	_, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	procID, err := strconv.ParseUint(c.Param("procId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid procId"})
		return
	}
	if err := h.svc.RestartExtraProcess(uint(procID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func parseUintParam(c *gin.Context, name string) (uint, error) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, err
	}
	return uint(v), nil
}
