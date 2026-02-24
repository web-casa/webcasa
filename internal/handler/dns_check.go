package handler

import (
	"net/http"

	"github.com/caddypanel/caddypanel/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DnsCheckHandler handles DNS check API endpoints
type DnsCheckHandler struct {
	svc *service.DnsCheckService
	db  *gorm.DB
}

// NewDnsCheckHandler creates a new DnsCheckHandler
func NewDnsCheckHandler(svc *service.DnsCheckService, db *gorm.DB) *DnsCheckHandler {
	return &DnsCheckHandler{svc: svc, db: db}
}

// Check performs a DNS resolution check for the given domain
// GET /api/dns-check?domain=xxx
func (h *DnsCheckHandler) Check(c *gin.Context) {
	domain := c.Query("domain")
	if domain == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "domain parameter is required",
			"error_key": "error.domain_required",
		})
		return
	}

	result, err := h.svc.Check(domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":     err.Error(),
			"error_key": "error.dns_check_failed",
		})
		return
	}

	c.JSON(http.StatusOK, result)
}
