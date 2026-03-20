package ai

import (
	"encoding/json"
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

// getUserID extracts the current user ID from the gin context.
func getUserID(c *gin.Context) uint {
	if id, exists := c.Get("user_id"); exists {
		switch v := id.(type) {
		case uint:
			return v
		case int:
			return uint(v)
		case float64:
			return uint(v)
		}
	}
	return 0
}

// getUserRole extracts the current user role from the gin context.
// Falls back to querying the DB if user_role is not set (e.g. on the read-only router).
func (h *Handler) getUserRole(c *gin.Context) string {
	if role, exists := c.Get("user_role"); exists {
		if s, ok := role.(string); ok {
			return s
		}
	}
	// Fallback: query role from DB using user_id.
	userID := getUserID(c)
	if userID == 0 {
		return "viewer"
	}
	var role string
	if err := h.svc.db.Table("users").Select("role").Where("id = ?", userID).Row().Scan(&role); err != nil {
		return "viewer"
	}
	return role
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

// GetPresets returns available AI provider presets.
func (h *Handler) GetPresets(c *gin.Context) {
	c.JSON(http.StatusOK, ProviderPresets)
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

// TestEmbeddingConnection tests the embedding API connectivity.
func (h *Handler) TestEmbeddingConnection(c *gin.Context) {
	if err := h.svc.TestEmbeddingConnection(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Chat handles a chat message with SSE streaming response and tool use support.
// SSE events:
//   - event: delta       → data: "text chunk"
//   - event: tool_call   → data: {"id":"...","name":"...","arguments":"..."}
//   - event: tool_result → data: {"tool_call_id":"...","name":"...","content":"..."}
//   - event: done        → data: "conversation_id"
//   - event: error       → data: "error message"
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

	convID, err := h.svc.ChatWithTools(c.Request.Context(), req, getUserID(c), h.getUserRole(c), func(event StreamEvent) error {
		switch event.Type {
		case "delta":
			writeSSEEvent(c.Writer, "delta", event.Content)
		case "tool_call":
			if event.ToolCall != nil {
				data, _ := json.Marshal(event.ToolCall)
				writeSSEEvent(c.Writer, "tool_call", string(data))
			}
		case "tool_result":
			if event.ToolCall != nil {
				data, _ := json.Marshal(map[string]string{
					"tool_call_id": event.ToolCall.ID,
					"name":         event.ToolCall.Name,
					"content":      event.Content,
				})
				writeSSEEvent(c.Writer, "tool_result", string(data))
			}
		case "confirm_required":
			writeSSEEvent(c.Writer, "confirm_required", event.Content)
		case "done":
			// Don't send done here — we send it below with the conversation ID
			return nil
		}
		c.Writer.Flush()
		return nil
	})
	if err != nil {
		writeSSEEvent(c.Writer, "error", err.Error())
		c.Writer.Flush()
		return
	}

	// Send conversation ID as the final done event.
	writeSSEEvent(c.Writer, "done", fmt.Sprintf("%d", convID))
	c.Writer.Flush()
}

// ListConversations returns conversations for the current user.
func (h *Handler) ListConversations(c *gin.Context) {
	convs, err := h.svc.ListConversations(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversations": convs})
}

// GetConversation returns a conversation with messages, scoped to current user.
func (h *Handler) GetConversation(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	conv, err := h.svc.GetConversation(uint(id), getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, conv)
}

// DeleteConversation removes a conversation, scoped to current user.
func (h *Handler) DeleteConversation(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.DeleteConversation(uint(id), getUserID(c)); err != nil {
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

// GenerateDockerfile converts natural language to an optimized Dockerfile (SSE).
func (h *Handler) GenerateDockerfile(c *gin.Context) {
	var req struct {
		Description string `json:"description"`
	}
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

	if err := h.svc.GenerateDockerfile(c.Request.Context(), req.Description, func(delta string) error {
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

// ReviewCode performs an AI code review of a project's source code (SSE).
func (h *Handler) ReviewCode(c *gin.Context) {
	var req struct {
		ProjectID uint `json:"project_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	if err := h.svc.ReviewCode(c.Request.Context(), req.ProjectID, func(delta string) error {
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

// Confirm resolves a pending tool execution confirmation.
func (h *Handler) Confirm(c *gin.Context) {
	var req struct {
		PendingID string `json:"pending_id"`
		Approved  bool   `json:"approved"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.PendingID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pending_id is required"})
		return
	}

	if err := h.svc.ResolveConfirmation(req.PendingID, req.Approved, getUserID(c)); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ──────────────────────────────────────────────
// Memory endpoints
// ──────────────────────────────────────────────

// ListMemories returns paginated AI memories.
func (h *Handler) ListMemories(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	category := c.Query("category")

	memories, total, err := h.svc.memory.ListMemories(getUserID(c), page, pageSize, category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"memories": memories,
		"total":    total,
		"page":     page,
	})
}

// DeleteMemory removes a single memory by ID.
func (h *Handler) DeleteMemory(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.memory.DeleteMemory(getUserID(c), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ClearMemories removes all AI memories.
func (h *Handler) ClearMemories(c *gin.Context) {
	if err := h.svc.memory.ClearAll(getUserID(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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

// RunInspection triggers a manual system inspection.
func (h *Handler) RunInspection(c *gin.Context) {
	inspection := h.svc.tools.inspection
	if inspection == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "inspection service not configured"})
		return
	}
	report, err := inspection.RunInspection()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

// GetInspectionConfig returns the inspection configuration.
func (h *Handler) GetInspectionConfig(c *gin.Context) {
	cs := h.svc.configStore
	c.JSON(http.StatusOK, gin.H{
		"enabled":    cs.Get("inspection_enabled") == "true",
		"hour":       cs.Get("inspection_hour"),
		"ai_summary": cs.Get("inspection_ai_summary") != "false",
	})
}

// UpdateInspectionConfig saves the inspection configuration.
func (h *Handler) UpdateInspectionConfig(c *gin.Context) {
	var req struct {
		Enabled   *bool `json:"enabled"`
		Hour      *int  `json:"hour"`
		AISummary *bool `json:"ai_summary"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cs := h.svc.configStore
	if req.Enabled != nil {
		val := "false"
		if *req.Enabled {
			val = "true"
		}
		if err := cs.Set("inspection_enabled", val); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if req.Hour != nil {
		if *req.Hour < 0 || *req.Hour > 23 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "hour must be 0-23"})
			return
		}
		if err := cs.Set("inspection_hour", strconv.Itoa(*req.Hour)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if req.AISummary != nil {
		val := "false"
		if *req.AISummary {
			val = "true"
		}
		if err := cs.Set("inspection_ai_summary", val); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// Reschedule the inspection loop based on the updated config.
	if inspection := h.svc.tools.inspection; inspection != nil {
		inspection.Reschedule()
	}

	c.JSON(http.StatusOK, gin.H{"message": "inspection config updated"})
}

// GetInspectionHistory returns recent inspection records.
func (h *Handler) GetInspectionHistory(c *gin.Context) {
	inspection := h.svc.tools.inspection
	if inspection == nil {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}
	limit := 20
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	records, err := inspection.GetHistory(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, records)
}
