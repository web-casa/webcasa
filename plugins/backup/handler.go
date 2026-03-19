package backup

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Handler implements the REST API for the backup plugin.
type Handler struct {
	svc *Service
}

// NewHandler creates a backup Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ── Install ──

// InstallKopia streams the Kopia installation progress via SSE.
func (h *Handler) InstallKopia(c *gin.Context) {
	// Set SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	writeSSE := func(data string) {
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}

	writeEvent := func(event, data string) {
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		c.Writer.Flush()
	}

	h.svc.kopia.InstallKopia(writeSSE, writeEvent)
}

// ── Config ──

// CheckDependency returns the availability of the Kopia CLI.
func (h *Handler) CheckDependency(c *gin.Context) {
	status := h.svc.CheckDependency()
	c.JSON(http.StatusOK, status)
}

// GetConfig returns the backup configuration.
func (h *Handler) GetConfig(c *gin.Context) {
	cfg, err := h.svc.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// UpdateConfig updates the backup configuration.
func (h *Handler) UpdateConfig(c *gin.Context) {
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg, err := h.svc.UpdateConfig(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// TestConnection tests the backup target connection.
func (h *Handler) TestConnection(c *gin.Context) {
	if err := h.svc.TestConnection(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Connection successful"})
}

// ── Snapshots ──

// ListSnapshots returns all backup snapshots.
func (h *Handler) ListSnapshots(c *gin.Context) {
	snapshots, err := h.svc.ListSnapshots()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": snapshots})
}

// CreateSnapshot triggers a manual backup.
// The backup runs asynchronously — the response contains the snapshot record
// with status "running". The frontend should poll GET /status to track progress.
func (h *Handler) CreateSnapshot(c *gin.Context) {
	// Pre-flight checks synchronously so errors are returned immediately.
	if status := h.svc.CheckDependency(); !status.Available {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kopia is not installed"})
		return
	}
	h.svc.mu.Lock()
	if h.svc.running {
		h.svc.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "a backup is already running"})
		return
	}
	h.svc.mu.Unlock()

	// Start backup in the background.
	go func() {
		if _, err := h.svc.RunBackup("manual"); err != nil {
			h.svc.logger.Error("manual backup failed", "err", err)
		}
	}()

	// Return early with 202 Accepted.
	c.JSON(http.StatusAccepted, gin.H{"message": "backup started"})
}

// RestoreSnapshot restores from a snapshot.
func (h *Handler) RestoreSnapshot(c *gin.Context) {
	id, err := parseIDParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.RestoreSnapshot(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Restore completed"})
}

// DeleteSnapshot removes a snapshot.
func (h *Handler) DeleteSnapshot(c *gin.Context) {
	id, err := parseIDParam(c, "id")
	if err != nil {
		return
	}
	if err := h.svc.DeleteSnapshot(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Snapshot deleted"})
}

// ── Status & Logs ──

// GetStatus returns the current backup status.
func (h *Handler) GetStatus(c *gin.Context) {
	status, err := h.svc.GetStatus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

// ListLogs returns backup logs.
func (h *Handler) ListLogs(c *gin.Context) {
	snapshotIDStr := c.Query("snapshot_id")
	limitStr := c.DefaultQuery("limit", "100")

	var snapshotID uint
	if snapshotIDStr != "" {
		v, _ := strconv.ParseUint(snapshotIDStr, 10, 32)
		snapshotID = uint(v)
	}
	limit, _ := strconv.Atoi(limitStr)

	logs, err := h.svc.ListLogs(snapshotID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// ── Helpers ──

func parseIDParam(c *gin.Context, name string) (uint, error) {
	id, err := strconv.ParseUint(c.Param(name), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, err
	}
	return uint(id), nil
}
