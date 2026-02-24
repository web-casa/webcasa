package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/service"
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
	var filter service.HostListFilter
	if gid := c.Query("group_id"); gid != "" {
		if id, err := strconv.ParseUint(gid, 10, 32); err == nil {
			uid := uint(id)
			filter.GroupID = &uid
		}
	}
	if tid := c.Query("tag_id"); tid != "" {
		if id, err := strconv.ParseUint(tid, 10, 32); err == nil {
			uid := uint(id)
			filter.TagID = &uid
		}
	}

	hosts, err := h.svc.List(filter)
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
// Clone creates a deep copy of an existing host with a new domain
func (h *HostHandler) Clone(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	var req struct {
		Domain string `json:"domain" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	// Get source host domain for audit log
	sourceHost, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found", "error_key": "error.host_not_found"})
		return
	}

	newHost, err := h.svc.CloneHost(id, req.Domain)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "error.domain_exists":
			c.JSON(http.StatusBadRequest, gin.H{
				"error":     fmt.Sprintf("domain '%s' already exists", req.Domain),
				"error_key": "error.domain_exists",
			})
		case "error.host_not_found":
			c.JSON(http.StatusNotFound, gin.H{
				"error":     "Host not found",
				"error_key": "error.host_not_found",
			})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":     errMsg,
				"error_key": "error.clone_failed",
			})
		}
		return
	}

	h.audit(c, "CLONE", fmt.Sprint(newHost.ID),
		fmt.Sprintf("Cloned host '%s' → '%s'", sourceHost.Domain, req.Domain))
	c.JSON(http.StatusCreated, newHost)
}

func parseID(c *gin.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	return uint(id), err
}
