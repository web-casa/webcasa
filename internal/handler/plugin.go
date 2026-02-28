package handler

import (
	"net/http"

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
	c.JSON(http.StatusOK, gin.H{"message": "Plugin enabled (restart required)"})
}

// Disable disables a plugin by ID.
func (h *PluginHandler) Disable(c *gin.Context) {
	id := c.Param("id")
	if err := h.mgr.Disable(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Plugin disabled (restart required)"})
}

// FrontendManifests returns the combined frontend manifests for all enabled plugins.
func (h *PluginHandler) FrontendManifests(c *gin.Context) {
	c.JSON(http.StatusOK, h.mgr.FrontendManifests())
}
