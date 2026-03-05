package firewall

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler implements the REST API for the firewall plugin.
type Handler struct {
	svc *Service
}

// NewHandler creates a firewall Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetStatus returns the firewalld status.
func (h *Handler) GetStatus(c *gin.Context) {
	status, err := h.svc.Status()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

// ListZones returns all zones with details.
func (h *Handler) ListZones(c *gin.Context) {
	zones, err := h.svc.ListZones()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"zones": zones})
}

// GetZone returns a single zone's details.
func (h *Handler) GetZone(c *gin.Context) {
	name := c.Param("name")
	zone, err := h.svc.GetZone(name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, zone)
}

// AddPort adds a port rule.
func (h *Handler) AddPort(c *gin.Context) {
	var req AddPortRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.AddPort(req.Zone, req.Port, req.Protocol); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "port added"})
}

// RemovePort removes a port rule.
func (h *Handler) RemovePort(c *gin.Context) {
	var req AddPortRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.RemovePort(req.Zone, req.Port, req.Protocol); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "port removed"})
}

// AddService adds a service rule.
func (h *Handler) AddService(c *gin.Context) {
	var req AddServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.AddService(req.Zone, req.Service); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "service added"})
}

// RemoveService removes a service rule.
func (h *Handler) RemoveService(c *gin.Context) {
	var req AddServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.RemoveService(req.Zone, req.Service); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "service removed"})
}

// AddRichRule adds a rich rule.
func (h *Handler) AddRichRule(c *gin.Context) {
	var req AddRichRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.AddRichRule(req.Zone, req.Rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "rich rule added"})
}

// RemoveRichRule removes a rich rule.
func (h *Handler) RemoveRichRule(c *gin.Context) {
	var req AddRichRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.RemoveRichRule(req.Zone, req.Rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "rich rule removed"})
}

// AvailableServices returns all known firewalld services.
func (h *Handler) AvailableServices(c *gin.Context) {
	services, err := h.svc.AvailableServices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"services": services})
}

// ReloadFirewall reloads firewalld configuration.
func (h *Handler) ReloadFirewall(c *gin.Context) {
	if err := h.svc.Reload(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "firewalld reloaded"})
}
