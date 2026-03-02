package monitoring

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Handler implements the REST API for the monitoring plugin.
type Handler struct {
	svc *Service
}

// NewHandler creates a monitoring Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetCurrent returns the latest system metrics.
func (h *Handler) GetCurrent(c *gin.Context) {
	snap, err := h.svc.GetCurrent()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, snap)
}

// GetHistory returns historical metrics.
func (h *Handler) GetHistory(c *gin.Context) {
	period := c.DefaultQuery("period", "1h")
	records, err := h.svc.GetHistory(period)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"records": records, "period": period, "count": len(records)})
}

// GetContainers returns current container metrics.
func (h *Handler) GetContainers(c *gin.Context) {
	containers, err := h.svc.GetContainers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"containers": containers})
}

// MetricsWS upgrades the connection to a WebSocket for real-time metrics.
func (h *Handler) MetricsWS(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	h.svc.Broadcaster().AddClient(conn)

	// Read loop to detect client disconnect.
	go func() {
		defer func() {
			h.svc.Broadcaster().RemoveClient(conn)
			conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

// ── Alert Rules ──

// ListAlertRules returns all alert rules.
func (h *Handler) ListAlertRules(c *gin.Context) {
	rules, err := h.svc.ListAlertRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

// CreateAlertRule creates a new alert rule.
func (h *Handler) CreateAlertRule(c *gin.Context) {
	var req CreateAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rule, err := h.svc.CreateAlertRule(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rule)
}

// UpdateAlertRule updates an alert rule.
func (h *Handler) UpdateAlertRule(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req UpdateAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rule, err := h.svc.UpdateAlertRule(id, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rule)
}

// DeleteAlertRule removes an alert rule.
func (h *Handler) DeleteAlertRule(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.DeleteAlertRule(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Alert rule deleted"})
}

// ListAlertHistory returns alert trigger history.
func (h *Handler) ListAlertHistory(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)
	history, err := h.svc.ListAlertHistory(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"history": history})
}

// ── Helpers ──

// wsUpgraderHandler is a compile-time check that wsUpgrader is a valid Upgrader.
var _ websocket.Upgrader = wsUpgrader

func parseUintParam(c *gin.Context, name string) (uint, error) {
	id, err := strconv.ParseUint(c.Param(name), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, err
	}
	return uint(id), nil
}
