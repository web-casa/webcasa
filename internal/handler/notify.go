package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/web-casa/webcasa/internal/notify"
)

// NotifyHandler provides HTTP handlers for notification channels.
type NotifyHandler struct {
	notifier *notify.Notifier
}

// NewNotifyHandler creates a new NotifyHandler.
func NewNotifyHandler(notifier *notify.Notifier) *NotifyHandler {
	return &NotifyHandler{notifier: notifier}
}

// ListChannels GET /api/notify/channels
func (h *NotifyHandler) ListChannels(c *gin.Context) {
	channels, err := h.notifier.ListChannels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, channels)
}

// CreateChannel POST /api/notify/channels
func (h *NotifyHandler) CreateChannel(c *gin.Context) {
	var ch notify.Channel
	if err := c.ShouldBindJSON(&ch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if ch.Type == "" || ch.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and name are required"})
		return
	}
	if err := h.notifier.CreateChannel(&ch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, ch)
}

// UpdateChannel PUT /api/notify/channels/:id
func (h *NotifyHandler) UpdateChannel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Filter allowed fields
	allowed := map[string]bool{
		"name": true, "type": true, "config": true, "enabled": true, "events": true,
	}
	filtered := make(map[string]interface{})
	for k, v := range updates {
		if allowed[k] {
			filtered[k] = v
		}
	}

	if err := h.notifier.UpdateChannel(uint(id), filtered); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteChannel DELETE /api/notify/channels/:id
func (h *NotifyHandler) DeleteChannel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.notifier.DeleteChannel(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TestChannel POST /api/notify/channels/:id/test
func (h *NotifyHandler) TestChannel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	ch, err := h.notifier.GetChannel(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}

	if err := h.notifier.TestChannel(*ch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "test notification sent"})
}
