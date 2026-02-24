package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/web-casa/webcasa/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DnsProviderHandler manages DNS provider CRUD
type DnsProviderHandler struct {
	db *gorm.DB
}

// NewDnsProviderHandler creates a new DnsProviderHandler
func NewDnsProviderHandler(db *gorm.DB) *DnsProviderHandler {
	return &DnsProviderHandler{db: db}
}

func (h *DnsProviderHandler) audit(c *gin.Context, action, detail string) {
	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), action, "dns_provider", "", detail, c.ClientIP())
	}
}

// List returns all DNS providers
func (h *DnsProviderHandler) List(c *gin.Context) {
	var providers []model.DnsProvider
	if err := h.db.Order("id ASC").Find(&providers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Mask config secrets in list view
	for i := range providers {
		providers[i].Config = "***"
	}
	c.JSON(http.StatusOK, gin.H{"providers": providers, "total": len(providers)})
}

// Get returns a single DNS provider
func (h *DnsProviderHandler) Get(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var p model.DnsProvider
	if err := h.db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "DNS provider not found"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// Create creates a new DNS provider
func (h *DnsProviderHandler) Create(c *gin.Context) {
	var req struct {
		Name      string `json:"name" binding:"required"`
		Provider  string `json:"provider" binding:"required"`
		Config    string `json:"config" binding:"required"`
		IsDefault *bool  `json:"is_default"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate provider type
	validProviders := map[string]bool{
		"cloudflare": true, "alidns": true, "tencentcloud": true, "route53": true,
	}
	if !validProviders[req.Provider] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider type"})
		return
	}

	isDefault := false
	if req.IsDefault != nil {
		isDefault = *req.IsDefault
	}

	p := model.DnsProvider{
		Name:      req.Name,
		Provider:  req.Provider,
		Config:    req.Config,
		IsDefault: &isDefault,
	}

	// If set as default, unset other defaults
	if isDefault {
		h.db.Model(&model.DnsProvider{}).Where("is_default = ?", true).Update("is_default", false)
	}

	if err := h.db.Create(&p).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, "CREATE", fmt.Sprintf("Created DNS provider: %s (%s)", p.Name, p.Provider))
	c.JSON(http.StatusCreated, p)
}

// Update updates an existing DNS provider
func (h *DnsProviderHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var p model.DnsProvider
	if err := h.db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "DNS provider not found"})
		return
	}

	var req struct {
		Name      string `json:"name"`
		Provider  string `json:"provider"`
		Config    string `json:"config"`
		IsDefault *bool  `json:"is_default"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != "" {
		p.Name = req.Name
	}
	if req.Provider != "" {
		p.Provider = req.Provider
	}
	if req.Config != "" {
		p.Config = req.Config
	}
	if req.IsDefault != nil {
		if *req.IsDefault {
			h.db.Model(&model.DnsProvider{}).Where("is_default = ?", true).Update("is_default", false)
		}
		p.IsDefault = req.IsDefault
	}

	if err := h.db.Save(&p).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, "UPDATE", fmt.Sprintf("Updated DNS provider: %s", p.Name))
	c.JSON(http.StatusOK, p)
}

// Delete deletes a DNS provider
func (h *DnsProviderHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var p model.DnsProvider
	if err := h.db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "DNS provider not found"})
		return
	}

	// Check if any hosts use this provider
	var count int64
	h.db.Model(&model.Host{}).Where("dns_provider_id = ?", id).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Cannot delete: %d hosts still use this provider", count)})
		return
	}

	if err := h.db.Delete(&p).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, "DELETE", fmt.Sprintf("Deleted DNS provider: %s", p.Name))
	c.JSON(http.StatusOK, gin.H{"message": "Deleted"})
}
