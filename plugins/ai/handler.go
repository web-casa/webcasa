package ai

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler exposes AI assistant REST API endpoints.
type Handler struct {
	svc *Service
}

// NewHandler creates a new AI handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// writeSSEData writes an SSE data field, encoding multi-line content correctly.
// Each line of the payload must be prefixed with "data: " per the SSE spec.
func writeSSEData(w gin.ResponseWriter, payload string) error {
	lines := strings.Split(payload, "\n")
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprint(w, "\n") // empty line terminates the event
	return err
}

// writeSSEEvent writes a named SSE event with properly encoded data.
func writeSSEEvent(w gin.ResponseWriter, event, payload string) {
	fmt.Fprintf(w, "event: %s\n", event)
	lines := strings.Split(payload, "\n")
	for _, line := range lines {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

// GetConfig returns the AI configuration (API key masked).
func (h *Handler) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.GetConfig())
}

// UpdateConfig saves the AI configuration.
func (h *Handler) UpdateConfig(c *gin.Context) {
	var cfg AIConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// TestConnection tests the AI API connectivity.
func (h *Handler) TestConnection(c *gin.Context) {
	if err := h.svc.TestConnection(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Chat handles a chat message with SSE streaming response.
func (h *Handler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

	// Set SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	convID, err := h.svc.Chat(c.Request.Context(), req, func(delta string) error {
		if err := writeSSEData(c.Writer, delta); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	})
	if err != nil {
		writeSSEEvent(c.Writer, "error", err.Error())
		c.Writer.Flush()
		return
	}

	// Send conversation ID as the final event.
	writeSSEEvent(c.Writer, "done", fmt.Sprintf("%d", convID))
	c.Writer.Flush()
}

// ListConversations returns all conversations.
func (h *Handler) ListConversations(c *gin.Context) {
	convs, err := h.svc.ListConversations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversations": convs})
}

// GetConversation returns a conversation with messages.
func (h *Handler) GetConversation(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	conv, err := h.svc.GetConversation(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, conv)
}

// DeleteConversation removes a conversation.
func (h *Handler) DeleteConversation(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.DeleteConversation(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GenerateCompose converts natural language to Docker Compose YAML (SSE).
func (h *Handler) GenerateCompose(c *gin.Context) {
	var req GenerateComposeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Description == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description is required"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	if err := h.svc.GenerateCompose(c.Request.Context(), req.Description, func(delta string) error {
		if err := writeSSEData(c.Writer, delta); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}); err != nil {
		writeSSEEvent(c.Writer, "error", err.Error())
	}
	writeSSEEvent(c.Writer, "done", "")
	c.Writer.Flush()
}

// Diagnose analyses error logs and streams diagnosis (SSE).
func (h *Handler) Diagnose(c *gin.Context) {
	var req DiagnoseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Logs == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "logs is required"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	if err := h.svc.Diagnose(c.Request.Context(), req, func(delta string) error {
		if err := writeSSEData(c.Writer, delta); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}); err != nil {
		writeSSEEvent(c.Writer, "error", err.Error())
	}
	writeSSEEvent(c.Writer, "done", "")
	c.Writer.Flush()
}
