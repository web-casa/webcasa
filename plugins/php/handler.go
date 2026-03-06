package php

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler implements the REST API for the PHP plugin.
type Handler struct {
	svc *Service
}

// NewHandler creates a PHP Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ── Versions ──

// ListVersions returns all available PHP versions.
func (h *Handler) ListVersions(c *gin.Context) {
	c.JSON(http.StatusOK, SupportedVersions)
}

// ListCommonExtensions returns the common extensions list.
func (h *Handler) ListCommonExtensions(c *gin.Context) {
	c.JSON(http.StatusOK, CommonExtensions)
}

// ── Runtimes ──

// ListRuntimes returns all installed runtimes.
func (h *Handler) ListRuntimes(c *gin.Context) {
	runtimes, err := h.svc.ListRuntimes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, runtimes)
}

// CreateRuntimeStream creates a runtime with SSE progress streaming.
func (h *Handler) CreateRuntimeStream(c *gin.Context) {
	var req CreateRuntimeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	progressCb := func(line string) {
		sseWriteData(c.Writer, flusher, line)
	}

	rt, err := h.svc.CreateRuntimeStream(&req, progressCb)
	if err != nil {
		sseWriteEvent(c.Writer, flusher, "error", err.Error())
		return
	}

	sseWriteEvent(c.Writer, flusher, "done", fmt.Sprintf("%d", rt.ID))
}

// DeleteRuntime deletes a runtime.
func (h *Handler) DeleteRuntime(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.DeleteRuntime(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "runtime deleted"})
}

// StartRuntime starts a runtime.
func (h *Handler) StartRuntime(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StartRuntime(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "runtime started"})
}

// StopRuntime stops a runtime.
func (h *Handler) StopRuntime(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StopRuntime(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "runtime stopped"})
}

// RestartRuntime restarts a runtime.
func (h *Handler) RestartRuntime(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.RestartRuntime(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "runtime restarted"})
}

// RuntimeLogs returns recent logs.
func (h *Handler) RuntimeLogs(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	lines := 100
	if l := c.Query("lines"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			lines = n
			if lines > 1000 {
				lines = 1000
			}
		}
	}
	logs, err := h.svc.GetRuntimeLogs(id, lines)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// ── Config ──

// GetConfig returns the runtime's PHP and FPM config.
func (h *Handler) GetConfig(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	phpCfg, fpmCfg, err := h.svc.GetConfig(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"php_config": phpCfg, "fpm_config": fpmCfg})
}

// UpdateConfig updates the runtime config.
func (h *Handler) UpdateConfig(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateConfig(id, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config updated"})
}

// Optimize auto-calculates optimal FPM settings.
func (h *Handler) Optimize(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	cfg, err := h.svc.Optimize(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// ── Extensions ──

// GetExtensions returns the runtime's installed extensions.
func (h *Handler) GetExtensions(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	exts, err := h.svc.GetExtensions(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, exts)
}

// InstallExtensions installs extensions with SSE streaming.
func (h *Handler) InstallExtensions(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req InstallExtensionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	progressCb := func(line string) {
		sseWriteData(c.Writer, flusher, line)
	}

	if err := h.svc.InstallExtensions(id, req.Extensions, progressCb); err != nil {
		sseWriteEvent(c.Writer, flusher, "error", err.Error())
		return
	}

	sseWriteEvent(c.Writer, flusher, "done", "ok")
}

// RemoveExtension removes an extension with SSE streaming.
func (h *Handler) RemoveExtension(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	extName := c.Param("name")
	if extName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "extension name required"})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	progressCb := func(line string) {
		sseWriteData(c.Writer, flusher, line)
	}

	if err := h.svc.RemoveExtension(id, extName, progressCb); err != nil {
		sseWriteEvent(c.Writer, flusher, "error", err.Error())
		return
	}

	sseWriteEvent(c.Writer, flusher, "done", "ok")
}

// ── Sites ──

// ListSites returns all PHP sites.
func (h *Handler) ListSites(c *gin.Context) {
	sites, err := h.svc.ListSites()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sites)
}

// GetSite returns a single site.
func (h *Handler) GetSite(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	site, err := h.svc.GetSite(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, site)
}

// CreateSiteStream creates a site with SSE progress streaming.
func (h *Handler) CreateSiteStream(c *gin.Context) {
	var req CreateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	progressCb := func(line string) {
		sseWriteData(c.Writer, flusher, line)
	}

	site, err := h.svc.CreateSite(&req, progressCb)
	if err != nil {
		sseWriteEvent(c.Writer, flusher, "error", err.Error())
		return
	}

	sseWriteEvent(c.Writer, flusher, "done", fmt.Sprintf("%d", site.ID))
}

// UpdateSite updates a site.
func (h *Handler) UpdateSite(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req UpdateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateSite(id, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "site updated"})
}

// DeleteSite deletes a site.
func (h *Handler) DeleteSite(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	deleteFiles := c.Query("delete_files") == "true"
	if err := h.svc.DeleteSite(id, deleteFiles); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "site deleted"})
}

// ── System Info ──

// GetSystemInfo returns server RAM and CPU info for tuning.
func (h *Handler) GetSystemInfo(c *gin.Context) {
	info := GetSystemInfo()
	c.JSON(http.StatusOK, info)
}

// GetTuningPresets returns the available FPM tuning presets.
func (h *Handler) GetTuningPresets(c *gin.Context) {
	c.JSON(http.StatusOK, TuningPresets)
}

// ── SSE Helpers ──

// sseWriteData writes an SSE data message, properly handling newlines.
func sseWriteData(w http.ResponseWriter, f http.Flusher, msg string) {
	for _, line := range strings.Split(msg, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprintf(w, "\n")
	f.Flush()
}

// sseWriteEvent writes a named SSE event.
func sseWriteEvent(w http.ResponseWriter, f http.Flusher, event, data string) {
	fmt.Fprintf(w, "event: %s\n", event)
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprintf(w, "\n")
	f.Flush()
}

// ── Helpers ──

func parseID(c *gin.Context) (uint, error) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return 0, err
	}
	return uint(id), nil
}
