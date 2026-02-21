package handler

import (
	"net/http"

	"github.com/caddypanel/caddypanel/internal/caddy"
	"github.com/gin-gonic/gin"
)

// CaddyHandler manages Caddy process control endpoints
type CaddyHandler struct {
	mgr *caddy.Manager
}

// NewCaddyHandler creates a new CaddyHandler
func NewCaddyHandler(mgr *caddy.Manager) *CaddyHandler {
	return &CaddyHandler{mgr: mgr}
}

// Status returns the current Caddy status
func (h *CaddyHandler) Status(c *gin.Context) {
	status := h.mgr.Status()
	c.JSON(http.StatusOK, status)
}

// Start starts the Caddy process
func (h *CaddyHandler) Start(c *gin.Context) {
	if err := h.mgr.Start(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Caddy started successfully"})
}

// Stop stops the Caddy process
func (h *CaddyHandler) Stop(c *gin.Context) {
	if err := h.mgr.Stop(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Caddy stopped successfully"})
}

// Reload reloads the Caddy configuration
func (h *CaddyHandler) Reload(c *gin.Context) {
	if err := h.mgr.Reload(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Caddy reloaded successfully"})
}

// GetCaddyfile returns the current Caddyfile content
func (h *CaddyHandler) GetCaddyfile(c *gin.Context) {
	content, err := h.mgr.GetCaddyfileContent()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read Caddyfile"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}
