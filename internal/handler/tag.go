package handler

import (
	"fmt"
	"net/http"

	"github.com/web-casa/webcasa/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TagHandler manages tag CRUD endpoints
type TagHandler struct {
	svc *service.TagService
	db  *gorm.DB
}

// NewTagHandler creates a new TagHandler
func NewTagHandler(svc *service.TagService, db *gorm.DB) *TagHandler {
	return &TagHandler{svc: svc, db: db}
}

func (h *TagHandler) audit(c *gin.Context, action, targetID, detail string) {
	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), action, "tag", targetID, detail, c.ClientIP())
	}
}

// List returns all tags
func (h *TagHandler) List(c *gin.Context) {
	tags, err := h.svc.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.tag_list_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags, "total": len(tags)})
}

// Create adds a new tag
func (h *TagHandler) Create(c *gin.Context) {
	var req struct {
		Name  string `json:"name" binding:"required"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	tag, err := h.svc.Create(req.Name, req.Color)
	if err != nil {
		if err.Error() == "error.tag_name_exists" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":     fmt.Sprintf("tag name '%s' already exists", req.Name),
				"error_key": "error.tag_name_exists",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.tag_create_failed"})
		return
	}

	h.audit(c, "CREATE", fmt.Sprint(tag.ID), fmt.Sprintf("Created tag '%s'", tag.Name))
	c.JSON(http.StatusCreated, tag)
}

// Update modifies an existing tag
func (h *TagHandler) Update(c *gin.Context) {
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

	tag, err := h.svc.Update(id, req.Name, req.Color)
	if err != nil {
		if err.Error() == "error.tag_name_exists" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":     fmt.Sprintf("tag name '%s' already exists", req.Name),
				"error_key": "error.tag_name_exists",
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.tag_update_failed"})
		return
	}

	h.audit(c, "UPDATE", fmt.Sprint(tag.ID), fmt.Sprintf("Updated tag '%s'", tag.Name))
	c.JSON(http.StatusOK, tag)
}

// Delete removes a tag
func (h *TagHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	if err := h.svc.Delete(id); err != nil {
		if err.Error() == "error.tag_not_found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found", "error_key": "error.tag_not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.tag_delete_failed"})
		return
	}

	h.audit(c, "DELETE", fmt.Sprint(id), "Deleted tag")
	c.JSON(http.StatusOK, gin.H{"message": "Tag deleted successfully"})
}
