package handler

import (
	"net/http"

	"github.com/web-casa/webcasa/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SettingHandler manages panel settings
type SettingHandler struct {
	db *gorm.DB
}

// NewSettingHandler creates a new SettingHandler
func NewSettingHandler(db *gorm.DB) *SettingHandler {
	return &SettingHandler{db: db}
}

// GetAll returns all settings as a key-value map
func (h *SettingHandler) GetAll(c *gin.Context) {
	var settings []model.Setting
	h.db.Find(&settings)
	result := make(map[string]string, len(settings))
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	c.JSON(http.StatusOK, gin.H{"settings": result})
}

// Update updates a setting by key
func (h *SettingHandler) Update(c *gin.Context) {
	var req struct {
		Key   string `json:"key" binding:"required"`
		Value string `json:"value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Only allow known settings
	allowed := map[string]bool{"auto_reload": true, "server_ipv4": true, "server_ipv6": true}
	if !allowed[req.Key] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown setting: " + req.Key})
		return
	}

	h.db.Where("key = ?", req.Key).Assign(model.Setting{Value: req.Value}).FirstOrCreate(&model.Setting{Key: req.Key})
	c.JSON(http.StatusOK, gin.H{"message": "Setting updated"})
}
