package appstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	webhandler "github.com/web-casa/webcasa/internal/handler"
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
// GET /api/plugins/appstore/apps?category=&search=&page=1&page_size=24&lang=zh
func (h *Handler) ListApps(c *gin.Context) {
	category := c.Query("category")
	search := c.Query("search")
	lang := c.Query("lang")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "24"))

	result, err := h.svc.ListApps(category, search, lang, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Apply i18n translations to each app
	if lang != "" && lang != "en" {
		for i := range result.Apps {
			applyI18n(&result.Apps[i], lang)
		}
	}

	// Large multilingual payload (often 50KB+); gzip when client accepts.
	webhandler.SuccessGzipped(c, result)
}

// GetApp returns a single app with full details (including compose and description).
// GET /api/plugins/appstore/apps/:id?lang=zh
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

	lang := c.Query("lang")

	// Apply i18n translations
	applyI18n(app, lang)

	// Use localized description if available
	description := app.Description
	if lang == "zh" && app.DescZh != "" {
		description = app.DescZh
	}

	// Translate form field labels/hints
	formFields := applyFormFieldsI18n(app.FormFields, app.I18nJSON, lang)

	c.JSON(http.StatusOK, gin.H{
		"id":                app.ID,
		"app_id":            app.AppID,
		"source_id":         app.SourceID,
		"name":              app.Name,
		"short_desc":        app.ShortDesc,
		"description":       description,
		"version":           app.Version,
		"author":            app.Author,
		"categories":        app.Categories,
		"port":              app.Port,
		"exposable":         app.Exposable,
		"compose_file":      app.ComposeFile,
		"form_fields":       formFields,
		"website":           app.Website,
		"source_url":        app.Source,
		"available":         app.Available,
		"url_suffix":        app.UrlSuffix,
		"no_gui":            app.NoGUI,
		"force_expose":      app.ForceExpose,
		"security_warnings": DetectSecurityFlags(app.ComposeFile),
	})
}

// AppLogo serves the logo image for an app.
// GET /api/plugins/appstore/apps/:id/logo
// :id can be a numeric database ID or a string app_id (e.g. "nextcloud").
func (h *Handler) AppLogo(c *gin.Context) {
	idStr := c.Param("id")
	var app *AppDefinition
	var err error

	// Try numeric ID first, fall back to app_id string
	if numID, parseErr := strconv.ParseUint(idStr, 10, 32); parseErr == nil {
		app, err = h.svc.GetApp(uint(numID))
	} else {
		app, err = h.svc.GetAppByAppID(idStr)
	}
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "App not found"})
		return
	}

	logoPath := h.svc.sources.GetAppLogoPath(app.SourceID, app.LogoPath)
	if logoPath == "" {
		c.Status(http.StatusNotFound)
		return
	}

	// Security: reject symlinks and verify the resolved path stays within the source directory.
	fi, err := os.Lstat(logoPath)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "symlinks not allowed"})
		return
	}
	resolved, err := filepath.EvalSymlinks(logoPath)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	sourceRoot := h.svc.sources.SourceDir(app.SourceID)
	if sourceRoot != "" {
		resolvedRoot, _ := filepath.EvalSymlinks(sourceRoot)
		if resolvedRoot == "" {
			resolvedRoot = sourceRoot
		}
		if !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) && resolved != resolvedRoot {
			c.JSON(http.StatusForbidden, gin.H{"error": "path outside source directory"})
			return
		}
	}

	c.File(resolved)
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

// SyncSourceStream triggers a sync and streams progress via SSE.
// GET /api/plugins/appstore/sources/:id/sync/stream
func (h *Handler) SyncSourceStream(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}

	// SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	writeSSE := func(data string) {
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}

	writeEvent := func(event, data string) {
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		c.Writer.Flush()
	}

	// Subscribe to progress
	ch := h.svc.sources.SubscribeSync(id)
	defer h.svc.sources.UnsubscribeSync(id, ch)

	// Start sync in background with cancellable context
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	syncDone := make(chan error, 1)
	go func() {
		syncDone <- h.svc.sources.SyncSourceWithContext(ctx, id)
	}()

	writeSSE("Sync started...")

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(msg)
		case err := <-syncDone:
			// Drain remaining messages
			for {
				select {
				case msg, ok := <-ch:
					if !ok {
						goto done
					}
					writeSSE(msg)
				default:
					goto done
				}
			}
		done:
			if err != nil {
				writeEvent("error", err.Error())
			} else {
				writeEvent("done", "ok")
			}
			return
		case <-c.Request.Context().Done():
			cancel()
			return
		}
	}
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

// UpdateDomain changes the domain for an installed app.
// PUT /api/plugins/appstore/installed/:id/domain
func (h *Handler) UpdateDomain(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req struct {
		Domain string `json:"domain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateDomain(id, req.Domain); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Domain updated"})
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

// ── i18n Helpers ──

// applyI18n replaces Name and ShortDesc with translated versions from I18nJSON.
func applyI18n(app *AppDefinition, lang string) {
	if lang == "" || lang == "en" || app.I18nJSON == "" {
		return
	}
	var m map[string]*AppI18n
	if err := json.Unmarshal([]byte(app.I18nJSON), &m); err != nil {
		return
	}
	tr := m[lang]
	if tr == nil {
		return
	}
	if tr.Name != "" {
		app.Name = tr.Name
	}
	if tr.ShortDesc != "" {
		app.ShortDesc = tr.ShortDesc
	}
}

// applyFormFieldsI18n translates form field labels/hints using I18nJSON.
func applyFormFieldsI18n(fieldsJSON, i18nJSON, lang string) string {
	if lang == "" || lang == "en" || i18nJSON == "" || fieldsJSON == "" {
		return fieldsJSON
	}
	var m map[string]*AppI18n
	if err := json.Unmarshal([]byte(i18nJSON), &m); err != nil {
		return fieldsJSON
	}
	tr := m[lang]
	if tr == nil || len(tr.Fields) == 0 {
		return fieldsJSON
	}

	var fields []map[string]interface{}
	if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
		return fieldsJSON
	}

	for i, field := range fields {
		envVar, _ := field["env_variable"].(string)
		if envVar == "" {
			continue
		}
		if fi, ok := tr.Fields[envVar]; ok {
			if fi.Label != "" {
				fields[i]["label"] = fi.Label
			}
			if fi.Hint != "" {
				fields[i]["hint"] = fi.Hint
			}
		}
	}

	result, err := json.Marshal(fields)
	if err != nil {
		return fieldsJSON
	}
	return string(result)
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

// parseI18nJSON safely parses the i18n JSON string into a map.
func parseI18nJSON(raw string) interface{} {
	if raw == "" {
		return nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil
	}
	return result
}
