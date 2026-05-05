package handler

import (
	"net/http"
	"regexp"
	"strings"

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
		Key string `json:"key" binding:"required"`
		// Value is intentionally NOT required — `wildcard_domain=""`
		// is a valid "disable preview deploys" state.
		Value string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Only allow known settings
	allowed := map[string]bool{
		"auto_reload":     true,
		"server_ipv4":     true,
		"server_ipv6":     true,
		"wildcard_domain": true, // PB-R2-H2: required by Preview Deploy (v0.14+)
	}
	if !allowed[req.Key] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown setting: " + req.Key})
		return
	}

	// Per-key normalize + validate. Defense-in-depth against a UI that
	// bypassed the frontend regex.
	if req.Key == "wildcard_domain" && req.Value != "" {
		v := strings.ToLower(strings.TrimSpace(req.Value))
		if !validWildcardDomain(v) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wildcard_domain — must be a bare DNS suffix (e.g. preview.example.com)"})
			return
		}
		req.Value = v
	}

	h.db.Where("key = ?", req.Key).Assign(model.Setting{Value: req.Value}).FirstOrCreate(&model.Setting{Key: req.Key})
	c.JSON(http.StatusOK, gin.H{"message": "Setting updated"})
}

// validWildcardDomain matches a bare DNS suffix: at least two labels,
// each label `a-z0-9` with optional `-` (not at edges). Same rule the
// frontend enforces; duplicated here so a non-browser client can't
// bypass.
var wildcardDomainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

func validWildcardDomain(s string) bool {
	return wildcardDomainRE.MatchString(s)
}
