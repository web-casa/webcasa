package handler

import (
	"fmt"
	"net/http"

	"github.com/caddypanel/caddypanel/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GroupHandler manages group CRUD endpoints
type GroupHandler struct {
	svc *service.GroupService
	db  *gorm.DB
}

// NewGroupHandler creates a new GroupHandler
func NewGroupHandler(svc *service.GroupService, db *gorm.DB) *GroupHandler {
	return &GroupHandler{svc: svc, db: db}
}

func (h *GroupHandler) audit(c *gin.Context, action, targetID, detail string) {
	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), action, "group", targetID, detail, c.ClientIP())
	}
}

// List returns all groups
func (h *GroupHandler) List(c *gin.Context) {
	groups, err := h.svc.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.group_list_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups, "total": len(groups)})
}

// Create adds a new group
func (h *GroupHandler) Create(c *gin.Context) {
	var req struct {
		Name  string `json:"name" binding:"required"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	group, err := h.svc.Create(req.Name, req.Color)
	if err != nil {
		if err.Error() == "error.group_name_exists" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":     fmt.Sprintf("group name '%s' already exists", req.Name),
				"error_key": "error.group_name_exists",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.group_create_failed"})
		return
	}

	h.audit(c, "CREATE", fmt.Sprint(group.ID), fmt.Sprintf("Created group '%s'", group.Name))
	c.JSON(http.StatusCreated, group)
}

// Update modifies an existing group
func (h *GroupHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	var req struct {
		Name  string `json:"name" binding:"required"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	group, err := h.svc.Update(id, req.Name, req.Color)
	if err != nil {
		if err.Error() == "error.group_name_exists" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":     fmt.Sprintf("group name '%s' already exists", req.Name),
				"error_key": "error.group_name_exists",
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.group_update_failed"})
		return
	}

	h.audit(c, "UPDATE", fmt.Sprint(group.ID), fmt.Sprintf("Updated group '%s'", group.Name))
	c.JSON(http.StatusOK, group)
}

// Delete removes a group
func (h *GroupHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	if err := h.svc.Delete(id); err != nil {
		if err.Error() == "error.group_not_found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Group not found", "error_key": "error.group_not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.group_delete_failed"})
		return
	}

	h.audit(c, "DELETE", fmt.Sprint(id), "Deleted group")
	c.JSON(http.StatusOK, gin.H{"message": "Group deleted successfully"})
}

// BatchEnable enables all hosts in a group
func (h *GroupHandler) BatchEnable(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	if err := h.svc.BatchEnable(id); err != nil {
		if err.Error() == "error.group_not_found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Group not found", "error_key": "error.group_not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.batch_enable_failed"})
		return
	}

	h.audit(c, "BATCH_ENABLE", fmt.Sprint(id), "Batch enabled all hosts in group")
	c.JSON(http.StatusOK, gin.H{"message": "All hosts in group enabled"})
}

// BatchDisable disables all hosts in a group
func (h *GroupHandler) BatchDisable(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	if err := h.svc.BatchDisable(id); err != nil {
		if err.Error() == "error.group_not_found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Group not found", "error_key": "error.group_not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.batch_disable_failed"})
		return
	}

	h.audit(c, "BATCH_DISABLE", fmt.Sprint(id), "Batch disabled all hosts in group")
	c.JSON(http.StatusOK, gin.H{"message": "All hosts in group disabled"})
}
