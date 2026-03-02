package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/web-casa/webcasa/internal/plugin"
)

// PluginHandler exposes plugin management endpoints.
type PluginHandler struct {
	mgr *plugin.Manager
}

// NewPluginHandler creates a PluginHandler.
func NewPluginHandler(mgr *plugin.Manager) *PluginHandler {
	return &PluginHandler{mgr: mgr}
}

// List returns all registered plugins with their enabled state.
func (h *PluginHandler) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"plugins": h.mgr.List()})
}

// Enable enables a plugin by ID.
func (h *PluginHandler) Enable(c *gin.Context) {
	id := c.Param("id")
	if err := h.mgr.Enable(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Plugin enabled"})
}

// Disable disables a plugin by ID.
func (h *PluginHandler) Disable(c *gin.Context) {
	id := c.Param("id")
	if err := h.mgr.Disable(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Plugin disabled"})
}

// FrontendManifests returns the combined frontend manifests for all enabled plugins.
func (h *PluginHandler) FrontendManifests(c *gin.Context) {
	c.JSON(http.StatusOK, h.mgr.FrontendManifests())
}

// SetSidebarVisibility toggles whether a plugin appears in the sidebar.
func (h *PluginHandler) SetSidebarVisibility(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Visible bool `json:"visible"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.mgr.SetSidebarVisible(id, req.Visible); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Sidebar visibility updated"})
}

// Install enables a plugin with SSE streaming progress.
func (h *PluginHandler) Install(c *gin.Context) {
	id := c.Param("id")

	// Set SSE headers.
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

	// Step 1: Check plugin exists.
	plugins := h.mgr.List()
	var target *plugin.PluginInfo
	for i := range plugins {
		if plugins[i].ID == id {
			target = &plugins[i]
			break
		}
	}
	if target == nil {
		writeSSE("ERROR: Plugin not found: " + id)
		writeEvent("error", "Plugin not found")
		return
	}
	writeSSE("Found plugin: " + target.Name + " v" + target.Version)
	time.Sleep(200 * time.Millisecond)

	// Step 2: Check dependencies.
	writeSSE("Checking dependencies...")
	for _, dep := range target.Dependencies {
		var depEnabled bool
		for _, p := range plugins {
			if p.ID == dep {
				depEnabled = p.Enabled
				break
			}
		}
		if !depEnabled {
			msg := fmt.Sprintf("ERROR: Required plugin not enabled: %s", dep)
			writeSSE(msg)
			writeEvent("error", msg)
			return
		}
		writeSSE("  ✓ " + dep + " is enabled")
	}
	if len(target.Dependencies) == 0 {
		writeSSE("  No dependencies required")
	}
	time.Sleep(200 * time.Millisecond)

	// Step 3: Enable the plugin.
	writeSSE("Enabling plugin...")
	if err := h.mgr.Enable(id); err != nil {
		msg := "ERROR: Failed to enable plugin: " + err.Error()
		writeSSE(msg)
		writeEvent("error", msg)
		return
	}
	writeSSE("Plugin enabled and started successfully")
	time.Sleep(100 * time.Millisecond)

	writeSSE("Installation complete!")
	writeEvent("done", "ok")
}
