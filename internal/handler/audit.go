package handler

import (
	"net/http"
	"strconv"

	"github.com/caddypanel/caddypanel/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuditHandler handles audit log queries
type AuditHandler struct {
	db *gorm.DB
}

func NewAuditHandler(db *gorm.DB) *AuditHandler {
	return &AuditHandler{db: db}
}

// List returns audit logs with pagination
func (h *AuditHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}

	var total int64
	h.db.Model(&model.AuditLog{}).Count(&total)

	var logs []model.AuditLog
	h.db.Order("created_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&logs)

	c.JSON(http.StatusOK, gin.H{
		"logs":     logs,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// WriteLog is a helper to create an audit log entry
func WriteAuditLog(db *gorm.DB, userID uint, username, action, target, targetID, detail, ip string) {
	db.Create(&model.AuditLog{
		UserID:   userID,
		Username: username,
		Action:   action,
		Target:   target,
		TargetID: targetID,
		Detail:   detail,
		IP:       ip,
	})
}
