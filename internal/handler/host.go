package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/caddypanel/caddypanel/internal/model"
	"github.com/caddypanel/caddypanel/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HostHandler manages proxy host CRUD endpoints
type HostHandler struct {
	svc *service.HostService
	db  *gorm.DB
}

// NewHostHandler creates a new HostHandler
func NewHostHandler(svc *service.HostService, db *gorm.DB) *HostHandler {
	return &HostHandler{svc: svc, db: db}
}

func (h *HostHandler) audit(c *gin.Context, action, targetID, detail string) {
	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), action, "host", targetID, detail, c.ClientIP())
	}
}

// List returns all proxy hosts
func (h *HostHandler) List(c *gin.Context) {
	hosts, err := h.svc.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"hosts": hosts, "total": len(hosts)})
}

// Get returns a single proxy host
func (h *HostHandler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	host, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	c.JSON(http.StatusOK, host)
}

// Create adds a new proxy host
func (h *HostHandler) Create(c *gin.Context) {
	var req model.HostCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	host, err := h.svc.Create(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, "CREATE", fmt.Sprint(host.ID), fmt.Sprintf("Created %s host '%s'", host.HostType, host.Domain))
	c.JSON(http.StatusCreated, host)
}

// Update modifies an existing proxy host
func (h *HostHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var req model.HostCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	host, err := h.svc.Update(id, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, "UPDATE", fmt.Sprint(host.ID), fmt.Sprintf("Updated host '%s'", host.Domain))
	c.JSON(http.StatusOK, host)
}

// Delete removes a proxy host
func (h *HostHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	if err := h.svc.Delete(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, "DELETE", fmt.Sprint(id), "Deleted host")
	c.JSON(http.StatusOK, gin.H{"message": "Host deleted successfully"})
}

// Toggle enables/disables a proxy host
func (h *HostHandler) Toggle(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	host, err := h.svc.Toggle(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	enabled := host.Enabled == nil || *host.Enabled
	action := "DISABLE"
	if enabled {
		action = "ENABLE"
	}
	h.audit(c, action, fmt.Sprint(host.ID), fmt.Sprintf("Toggled host '%s' → %s", host.Domain, action))
	c.JSON(http.StatusOK, host)
}

func parseID(c *gin.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	return uint(id), err
}
