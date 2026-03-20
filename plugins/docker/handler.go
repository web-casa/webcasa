package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Handler implements the REST API for Docker management.
type Handler struct {
	svc         *Service
	client      *Client
	reconnectFn func() bool // called after daemon restart to reconnect
}

// NewHandler creates a Docker Handler.
func NewHandler(svc *Service, client *Client) *Handler {
	return &Handler{svc: svc, client: client}
}

func (h *Handler) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// ── System ──

// Info returns Docker system info.
func (h *Handler) Info(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	info, err := h.client.Info(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

// ── Stacks ──

// ListStacks returns all stacks.
func (h *Handler) ListStacks(c *gin.Context) {
	stacks, err := h.svc.ListStacks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stacks": stacks})
}

// GetStack returns a single stack.
func (h *Handler) GetStack(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	stack, err := h.svc.GetStack(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stack not found"})
		return
	}
	c.JSON(http.StatusOK, stack)
}

// CreateStack creates a new stack.
func (h *Handler) CreateStack(c *gin.Context) {
	var req CreateStackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	stack, err := h.svc.CreateStack(&req)
	if err != nil {
		// If the stack was created but auto-start failed, return 201 with a warning.
		if stack != nil {
			c.JSON(http.StatusCreated, gin.H{"data": stack, "warning": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, stack)
}

// UpdateStack updates a stack.
func (h *Handler) UpdateStack(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req CreateStackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	stack, err := h.svc.UpdateStack(id, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stack)
}

// DeleteStack deletes a stack.
func (h *Handler) DeleteStack(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.DeleteStack(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Stack deleted"})
}

// StackUp starts a stack.
func (h *Handler) StackUp(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StackUp(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Stack started"})
}

// StackDown stops a stack.
func (h *Handler) StackDown(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StackDown(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Stack stopped"})
}

// StackRestart restarts a stack.
func (h *Handler) StackRestart(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StackRestart(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Stack restarted"})
}

// StackPull pulls latest images for a stack.
func (h *Handler) StackPull(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StackPull(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Images pulled"})
}

// StackLogs returns recent logs for a stack.
func (h *Handler) StackLogs(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	tail := sanitizeTail(c.DefaultQuery("tail", "200"))
	logs, err := h.svc.StackLogs(id, tail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// ── Containers ──

// ListContainers returns all containers.
func (h *Handler) ListContainers(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	all := c.DefaultQuery("all", "true") == "true"
	containers, err := h.client.ListContainers(ctx, all)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"containers": containers})
}

// StartContainer starts a container.
func (h *Handler) StartContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.StartContainer(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Container started"})
}

// StopContainer stops a container.
func (h *Handler) StopContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.StopContainer(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Container stopped"})
}

// RestartContainer restarts a container.
func (h *Handler) RestartContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RestartContainer(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Container restarted"})
}

// RemoveContainer removes a container.
func (h *Handler) RemoveContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RemoveContainer(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Container removed"})
}

// ContainerLogs returns recent logs.
func (h *Handler) ContainerLogs(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tail := sanitizeTail(c.DefaultQuery("tail", "200"))
	reader, err := h.client.ContainerLogs(ctx, c.Param("id"), tail, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer reader.Close()

	data, _ := readAll(reader, 1<<20) // max 1MB
	c.JSON(http.StatusOK, gin.H{"logs": string(data)})
}

// ContainerStats returns resource stats.
func (h *Handler) ContainerStats(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	stats, err := h.client.GetContainerStats(ctx, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// ── Daemon Configuration ──

// GetDaemonConfig returns the current Docker daemon configuration.
func (h *Handler) GetDaemonConfig(c *gin.Context) {
	cfg, _, err := ReadDaemonConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"config": cfg})
}

// UpdateDaemonConfig writes daemon.json and restarts Docker.
func (h *Handler) UpdateDaemonConfig(c *gin.Context) {
	var cfg DaemonConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Read existing raw config to preserve unmanaged fields.
	_, raw, err := ReadDaemonConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read current config: " + err.Error()})
		return
	}

	// Back up the current config as raw bytes so we can restore on restart failure.
	oldConfig, _ := os.ReadFile("/etc/docker/daemon.json")

	// Write merged config.
	if err := WriteDaemonConfig(&cfg, raw); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write config: " + err.Error()})
		return
	}

	// Restart Docker daemon. If it fails, the new config is likely invalid —
	// restore the previous config so Docker can start again.
	if err := RestartDockerDaemon(); err != nil {
		// Attempt to rollback.
		if rollbackErr := WriteDaemonConfigRaw(oldConfig); rollbackErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("restart failed: %v; rollback also failed: %v — manual intervention required", err, rollbackErr),
			})
			return
		}
		// Try restarting with the old config.
		_ = RestartDockerDaemon()
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid config: Docker failed to restart, previous config restored"})
		return
	}

	// Wait for daemon to come back (up to 15 seconds).
	reconnected := false
	if h.reconnectFn != nil {
		for i := 0; i < 15; i++ {
			time.Sleep(1 * time.Second)
			if h.reconnectFn() {
				reconnected = true
				break
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"reconnected": reconnected,
	})
}

// ── Run Container ──

// RunContainer creates and starts a standalone container.
func (h *Handler) RunContainer(c *gin.Context) {
	var req RunContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image is required"})
		return
	}

	// Validate restart policy.
	switch req.RestartPolicy {
	case "", "no", "always", "unless-stopped", "on-failure":
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid restart_policy, must be one of: no, always, unless-stopped, on-failure"})
		return
	}

	// Try to pull the image first, but don't fail if it errors — the image
	// may already exist locally (local build, offline environment, etc.).
	pullCtx, pullCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer pullCancel()
	if reader, err := h.client.PullImage(pullCtx, req.Image); err == nil {
		_, _ = io.Copy(io.Discard, reader)
		reader.Close()
	}

	// Create and start the container.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := h.client.RunContainer(ctx, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "Container created and started"})
}

// ── Images ──

// ListImages returns all local images.
func (h *Handler) ListImages(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	images, err := h.client.ListImages(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"images": images})
}

// PullImage pulls an image.
func (h *Handler) PullImage(c *gin.Context) {
	var req struct {
		Image string `json:"image" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	reader, err := h.client.PullImage(ctx, req.Image)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer reader.Close()

	// Drain the pull output (we could stream it via SSE in the future).
	data, _ := readAll(reader, 1<<20)
	c.JSON(http.StatusOK, gin.H{"message": "Image pulled", "output": string(data)})
}

// RemoveImage removes an image.
func (h *Handler) RemoveImage(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RemoveImage(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Image removed"})
}

// PruneImages removes unused images.
func (h *Handler) PruneImages(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	reclaimed, err := h.client.PruneImages(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Images pruned", "space_reclaimed": reclaimed})
}

// ── Networks ──

// ListNetworks returns all networks.
func (h *Handler) ListNetworks(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	nets, err := h.client.ListNetworks(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"networks": nets})
}

// CreateNetwork creates a network.
func (h *Handler) CreateNetwork(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := h.ctx()
	defer cancel()
	id, err := h.client.CreateNetwork(ctx, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "Network created"})
}

// RemoveNetwork removes a network.
func (h *Handler) RemoveNetwork(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RemoveNetwork(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Network removed"})
}

// ── Volumes ──

// ListVolumes returns all volumes.
func (h *Handler) ListVolumes(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	vols, err := h.client.ListVolumes(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"volumes": vols})
}

// CreateVolume creates a volume.
func (h *Handler) CreateVolume(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.CreateVolume(ctx, req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Volume created"})
}

// RemoveVolume removes a volume.
func (h *Handler) RemoveVolume(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RemoveVolume(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Volume removed"})
}

// ── Helpers ──

// sanitizeTail validates the tail parameter as a positive integer with a max cap.
func sanitizeTail(s string) string {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return "200"
	}
	if n > 5000 {
		n = 5000
	}
	return strconv.Itoa(n)
}

func parseID(c *gin.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, err
	}
	return uint(id), nil
}

func readAll(r interface{ Read([]byte) (int, error) }, maxBytes int) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) < maxBytes {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	if len(buf) > maxBytes {
		buf = buf[:maxBytes]
	}
	return buf, nil
}

// ── WebSocket Log Streaming ──

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

// ContainerLogsWS streams container logs via WebSocket.
func (h *Handler) ContainerLogsWS(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	containerID := c.Param("id")
	tail := sanitizeTail(c.DefaultQuery("tail", "100"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start a goroutine to detect client disconnect
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	reader, err := h.client.ContainerLogs(ctx, containerID, tail, true)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Docker log stream has 8-byte header; strip it for clean output
		if len(line) > 8 {
			line = line[8:]
		}
		if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
			return
		}
	}
}

// StackLogsWS streams stack logs via WebSocket.
func (h *Handler) StackLogsWS(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	id, err := parseID(c)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: invalid id"))
		return
	}
	tail := sanitizeTail(c.DefaultQuery("tail", "100"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	reader, err := h.svc.StackLogsFollow(ctx, id, tail)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		if err := conn.WriteMessage(websocket.TextMessage, scanner.Bytes()); err != nil {
			return
		}
	}
}

// ── Image Search ──

// SearchImages searches Docker Hub for images.
func (h *Handler) SearchImages(c *gin.Context) {
	term := c.Query("q")
	if term == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q parameter is required"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
	ctx, cancel := h.ctx()
	defer cancel()
	results, err := h.client.SearchImages(ctx, term, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}
