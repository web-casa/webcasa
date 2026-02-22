package handler

import (
	"fmt"
	"net/http"

	"github.com/caddypanel/caddypanel/internal/caddy"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CaddyHandler manages Caddy process control endpoints
type CaddyHandler struct {
	mgr *caddy.Manager
	db  *gorm.DB
}

// NewCaddyHandler creates a new CaddyHandler
func NewCaddyHandler(mgr *caddy.Manager, db *gorm.DB) *CaddyHandler {
	return &CaddyHandler{mgr: mgr, db: db}
}

func (h *CaddyHandler) audit(c *gin.Context, action, detail string) {
	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), action, "caddy", "", detail, c.ClientIP())
	}
}

// Status returns the current Caddy status
func (h *CaddyHandler) Status(c *gin.Context) {
	status := h.mgr.Status()
	c.JSON(http.StatusOK, status)
}

// Start starts the Caddy process
func (h *CaddyHandler) Start(c *gin.Context) {
	if err := h.mgr.Start(); err != nil {
		// If already running, treat as success (idempotent)
		if h.mgr.IsRunning() {
			c.JSON(http.StatusOK, gin.H{"message": "Caddy is already running"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "START", "Started Caddy")
	c.JSON(http.StatusOK, gin.H{"message": "Caddy started successfully"})
}

// Stop stops the Caddy process
func (h *CaddyHandler) Stop(c *gin.Context) {
	if err := h.mgr.Stop(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "STOP", "Stopped Caddy")
	c.JSON(http.StatusOK, gin.H{"message": "Caddy stopped successfully"})
}

// Reload reloads the Caddy configuration
func (h *CaddyHandler) Reload(c *gin.Context) {
	if err := h.mgr.Reload(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "RELOAD", "Reloaded Caddy configuration")
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
