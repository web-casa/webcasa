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
		Name:         req.Name,
		Domain:       req.Domain,
		GitURL:       req.GitURL,
		GitBranch:    branch,
		DeployKey:    req.DeployKey,
		Framework:    req.Framework,
		BuildCommand: req.BuildCommand,
		StartCommand: req.StartCommand,
		InstallCmd:   req.InstallCmd,
		Port:         req.Port,
		AutoDeploy:   req.AutoDeploy,
		EnvVarList:   req.EnvVars,
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
		"auto_deploy": true, "env_vars": true,
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

func parseUintParam(c *gin.Context, name string) (uint, error) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, err
	}
	return uint(v), nil
}
