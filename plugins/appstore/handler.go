package appstore

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Handler implements the REST API handlers for the App Store.
type Handler struct {
	svc    *Service
	tplSvc *TemplateService
}

// NewHandler creates an App Store handler.
func NewHandler(svc *Service, tplSvc *TemplateService) *Handler {
	return &Handler{svc: svc, tplSvc: tplSvc}
}

// ── App Catalog ──

// ListApps returns paginated apps with optional category and search filters.
// GET /api/plugins/appstore/apps?category=&search=&page=1&page_size=24
func (h *Handler) ListApps(c *gin.Context) {
	category := c.Query("category")
	search := c.Query("search")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "24"))

	result, err := h.svc.ListApps(category, search, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetApp returns a single app with full details (including compose and description).
// GET /api/plugins/appstore/apps/:id
func (h *Handler) GetApp(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	app, err := h.svc.GetApp(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "App not found"})
		return
	}
	// Return full details including compose_file and description
	c.JSON(http.StatusOK, gin.H{
		"id":           app.ID,
		"app_id":       app.AppID,
		"source_id":    app.SourceID,
		"name":         app.Name,
		"short_desc":   app.ShortDesc,
		"description":  app.Description,
		"version":      app.Version,
		"author":       app.Author,
		"categories":   app.Categories,
		"port":         app.Port,
		"exposable":    app.Exposable,
		"compose_file": app.ComposeFile,
		"form_fields":  app.FormFields,
		"website":      app.Website,
		"source_url":   app.Source,
		"available":    app.Available,
	})
}

// AppLogo serves the logo image for an app.
// GET /api/plugins/appstore/apps/:id/logo
func (h *Handler) AppLogo(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	app, err := h.svc.GetApp(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "App not found"})
		return
	}

	logoPath := h.svc.sources.GetAppLogoPath(app.SourceID, app.LogoPath)
	if logoPath == "" {
		c.Status(http.StatusNotFound)
		return
	}

	if _, err := os.Stat(logoPath); os.IsNotExist(err) {
		c.Status(http.StatusNotFound)
		return
	}

	c.File(logoPath)
}

// ListCategories returns all available app categories.
// GET /api/plugins/appstore/categories
func (h *Handler) ListCategories(c *gin.Context) {
	categories := h.svc.GetCategories()
	c.JSON(http.StatusOK, gin.H{"categories": categories})
}

// ── Sources ──

// ListSources returns configured app/template sources.
// GET /api/plugins/appstore/sources
func (h *Handler) ListSources(c *gin.Context) {
	sources, err := h.svc.sources.ListSources()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sources": sources})
}

// AddSource creates a new app or template source.
// POST /api/plugins/appstore/sources
func (h *Handler) AddSource(c *gin.Context) {
	var req struct {
		Name   string `json:"name" binding:"required"`
		URL    string `json:"url" binding:"required"`
		Branch string `json:"branch"`
		Kind   string `json:"kind"` // "app" or "template"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	src, err := h.svc.sources.AddSource(req.Name, req.URL, req.Branch, req.Kind)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, src)
}

// SyncSource triggers a manual sync for a source.
// POST /api/plugins/appstore/sources/:id/sync
func (h *Handler) SyncSource(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}

	// Run async to avoid HTTP timeout
	go func() {
		if err := h.svc.sources.SyncSource(id); err != nil {
			h.svc.logger.Error("manual sync failed", "source_id", id, "err", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Sync started"})
}

// RemoveSource deletes a custom source.
// DELETE /api/plugins/appstore/sources/:id
func (h *Handler) RemoveSource(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.sources.RemoveSource(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Source removed"})
}

// ── Installed Apps ──

// ListInstalled returns all installed apps.
// GET /api/plugins/appstore/installed
func (h *Handler) ListInstalled(c *gin.Context) {
	apps, err := h.svc.ListInstalled()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"apps": apps})
}

// GetInstalled returns a single installed app.
// GET /api/plugins/appstore/installed/:id
func (h *Handler) GetInstalled(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	app, err := h.svc.GetInstalled(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
		return
	}
	c.JSON(http.StatusOK, app)
}

// InstallApp installs an app from the catalog.
// POST /api/plugins/appstore/install
func (h *Handler) InstallApp(c *gin.Context) {
	var req InstallAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	app, err := h.svc.InstallApp(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, app)
}

// StartApp starts a stopped installed app.
// POST /api/plugins/appstore/installed/:id/start
func (h *Handler) StartApp(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StartApp(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "App started"})
}

// StopApp stops a running installed app.
// POST /api/plugins/appstore/installed/:id/stop
func (h *Handler) StopApp(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StopApp(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "App stopped"})
}

// UpdateApp updates an installed app to the latest version.
// POST /api/plugins/appstore/installed/:id/update
func (h *Handler) UpdateApp(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.UpdateApp(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "App updated"})
}

// UninstallApp removes an installed app.
// DELETE /api/plugins/appstore/installed/:id?remove_data=true
func (h *Handler) UninstallApp(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	removeData := c.Query("remove_data") == "true"
	if err := h.svc.UninstallApp(id, removeData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "App uninstalled"})
}

// CheckUpdates returns available updates for installed apps.
// GET /api/plugins/appstore/updates
func (h *Handler) CheckUpdates(c *gin.Context) {
	updates, err := h.svc.CheckUpdates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updates": updates})
}

// ── Templates ──

// ListTemplates returns project templates with optional filtering.
// GET /api/plugins/appstore/templates?framework=&search=
func (h *Handler) ListTemplates(c *gin.Context) {
	framework := c.Query("framework")
	search := c.Query("search")

	templates, err := h.tplSvc.ListTemplates(framework, search)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	frameworks := h.tplSvc.GetFrameworks()
	c.JSON(http.StatusOK, gin.H{"templates": templates, "frameworks": frameworks})
}

// GetTemplate returns a single template.
// GET /api/plugins/appstore/templates/:id
func (h *Handler) GetTemplate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	tpl, err := h.tplSvc.GetTemplate(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
		return
	}
	c.JSON(http.StatusOK, tpl)
}

// DeployFromTemplate creates a project from a template.
// POST /api/plugins/appstore/templates/deploy
func (h *Handler) DeployFromTemplate(c *gin.Context) {
	var req CreateFromTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	projectID, err := h.tplSvc.DeployFromTemplate(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"project_id": projectID, "message": "Project created from template"})
}

// ── Helpers ──

func parseID(c *gin.Context) (uint, error) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return 0, err
	}
	return uint(id), nil
}
