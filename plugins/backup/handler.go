package backup

import (
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
func (h *Handler) CreateSnapshot(c *gin.Context) {
	snap, err := h.svc.RunBackup("manual")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, snap)
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
