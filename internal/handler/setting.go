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
	// Value is *string so we can distinguish missing field from empty
	// string. wildcard_domain explicitly allows empty (= disable
	// preview deploys); other keys require a non-nil value (and most
	// reject empty-string too — see per-key validation below).
	// PB-R3-M1 fix: previously Value lost its `required` tag entirely,
	// which silently let `auto_reload=""` slip through and disable
	// the auto-reload feature.
	var req struct {
		Key   string  `json:"key" binding:"required"`
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value is required"})
		return
	}
	value := *req.Value

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
	switch req.Key {
	case "wildcard_domain":
		// Empty is allowed (disables previews). Non-empty must be a
		// bare DNS suffix; defensively normalize before validating.
		if value != "" {
			v := strings.ToLower(strings.TrimSpace(value))
			if !validWildcardDomain(v) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wildcard_domain — must be a bare DNS suffix (e.g. preview.example.com)"})
				return
			}
			value = v
		}
	case "auto_reload":
		// Strict boolean string. Anything else (including "") could
		// silently flip the read-side `!= "false"` default check.
		if value != "true" && value != "false" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auto_reload must be 'true' or 'false'"})
			return
		}
	}

	h.db.Where("key = ?", req.Key).Assign(model.Setting{Value: value}).FirstOrCreate(&model.Setting{Key: req.Key})
	c.JSON(http.StatusOK, gin.H{"message": "Setting updated"})
}

// validWildcardDomain matches a bare DNS suffix: at least two labels,
// each label `a-z0-9` with optional `-` (not at edges) AND ≤63 chars
// per RFC 1035, total ≤253. PB-R3-L2 fix.
var wildcardDomainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

func validWildcardDomain(s string) bool {
	if len(s) > 253 {
		return false
	}
	if !wildcardDomainRE.MatchString(s) {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) > 63 {
			return false
		}
	}
	return true
}
