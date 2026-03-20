package docker

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for Docker management.
type Plugin struct {
	client          *Client
	svc             *Service
	handler         *Handler
	dockerAvailable bool
	dockerError     string
	socketPath      string // configured docker socket path
}

// New creates a new Docker plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "docker",
		Name:        "Docker",
		Version:     "1.0.0",
		Description: "Docker & Docker Compose management with simple and advanced modes",
		Author:      "Web.Casa",
		Priority:    10,
		Icon:        "Container",
		Category:    "deploy",
	}
}

// Init initialises the Docker plugin: connects to Docker, migrates DB, registers routes.
// If Docker is not available, the plugin still initialises with limited functionality
// so that its routes exist for the guard middleware and status endpoint.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Read socket path from plugin config, default to /var/run/docker.sock.
	p.socketPath = ctx.ConfigStore.Get("socket_path")
	if p.socketPath == "" {
		p.socketPath = "/var/run/docker.sock"
	}

	// Connect to Docker daemon (graceful: don't fail if Docker is unavailable).
	client, err := NewClient(p.socketPath)
	if err != nil {
		p.dockerAvailable = false
		p.dockerError = err.Error()
		ctx.Logger.Warn("Docker not available, plugin will run in limited mode", "err", err)
	} else {
		p.dockerAvailable = true
		p.client = client
	}

	// Migrate models.
	if err := ctx.DB.AutoMigrate(&Stack{}); err != nil {
		return err
	}

	// Create service and handler (may use nil client, handler checks dockerAvailable).
	p.svc = NewService(ctx.DB, client, ctx.DataDir, ctx.Logger)
	p.handler = NewHandler(p.svc, client)
	p.handler.reconnectFn = p.tryReconnect

	// Register API routes under /api/plugins/docker/
	r := ctx.Router      // read-only
	a := ctx.AdminRouter // admin-only

	// Docker status endpoint (always available, even if Docker is not installed).
	r.GET("/status", p.dockerStatus)

	// Docker install endpoint (admin only, SSE streaming).
	a.POST("/install", p.installDocker)

	// Daemon configuration (settings page — no requireDocker, settings should
	// be accessible even if the daemon is currently down).
	r.GET("/daemon-config", p.handler.GetDaemonConfig)
	a.PUT("/daemon-config", p.handler.UpdateDaemonConfig)

	// System (read)
	r.GET("/info", p.requireDocker(), p.handler.Info)

	// Stacks (read + admin mutations)
	r.GET("/stacks", p.requireDocker(), p.handler.ListStacks)
	a.POST("/stacks", p.requireDocker(), p.handler.CreateStack)
	r.GET("/stacks/:id", p.requireDocker(), p.handler.GetStack)
	a.PUT("/stacks/:id", p.requireDocker(), p.handler.UpdateStack)
	a.DELETE("/stacks/:id", p.requireDocker(), p.handler.DeleteStack)
	a.POST("/stacks/:id/up", p.requireDocker(), p.handler.StackUp)
	a.POST("/stacks/:id/down", p.requireDocker(), p.handler.StackDown)
	a.POST("/stacks/:id/restart", p.requireDocker(), p.handler.StackRestart)
	a.POST("/stacks/:id/pull", p.requireDocker(), p.handler.StackPull)
	r.GET("/stacks/:id/logs", p.requireDocker(), p.handler.StackLogs)

	// Containers (read + admin mutations)
	r.GET("/containers", p.requireDocker(), p.handler.ListContainers)
	a.POST("/containers/run", p.requireDocker(), p.handler.RunContainer)
	a.POST("/containers/:id/start", p.requireDocker(), p.handler.StartContainer)
	a.POST("/containers/:id/stop", p.requireDocker(), p.handler.StopContainer)
	a.POST("/containers/:id/restart", p.requireDocker(), p.handler.RestartContainer)
	a.DELETE("/containers/:id", p.requireDocker(), p.handler.RemoveContainer)
	r.GET("/containers/:id/logs", p.requireDocker(), p.handler.ContainerLogs)
	r.GET("/containers/:id/stats", p.requireDocker(), p.handler.ContainerStats)

	// Images (read + admin mutations)
	r.GET("/images", p.requireDocker(), p.handler.ListImages)
	a.POST("/images/pull", p.requireDocker(), p.handler.PullImage)
	a.DELETE("/images/:id", p.requireDocker(), p.handler.RemoveImage)
	a.POST("/images/prune", p.requireDocker(), p.handler.PruneImages)
	r.GET("/images/search", p.requireDocker(), p.handler.SearchImages)

	// Networks (read + admin mutations)
	r.GET("/networks", p.requireDocker(), p.handler.ListNetworks)
	a.POST("/networks", p.requireDocker(), p.handler.CreateNetwork)
	a.DELETE("/networks/:id", p.requireDocker(), p.handler.RemoveNetwork)

	// Volumes (read + admin mutations)
	r.GET("/volumes", p.requireDocker(), p.handler.ListVolumes)
	a.POST("/volumes", p.requireDocker(), p.handler.CreateVolume)
	a.DELETE("/volumes/:id", p.requireDocker(), p.handler.RemoveVolume)

	// WebSocket log streaming (read)
	r.GET("/containers/:id/logs/ws", p.requireDocker(), p.handler.ContainerLogsWS)
	r.GET("/stacks/:id/logs/ws", p.requireDocker(), p.handler.StackLogsWS)

	ctx.Logger.Info("Docker plugin routes registered", "docker_available", p.dockerAvailable)
	return nil
}

// requireDocker returns middleware that blocks requests if Docker is not available.
func (p *Plugin) requireDocker() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !p.dockerAvailable {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":     "Docker is not available",
				"installed": false,
				"detail":    p.dockerError,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// tryReconnect attempts to connect to the Docker daemon and update the plugin state.
// Returns true if the daemon is reachable.
func (p *Plugin) tryReconnect() bool {
	client, err := NewClient(p.socketPath)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		client.Close()
		return false
	}
	// Close old client to release resources (HTTP connection pool).
	if p.client != nil {
		p.client.Close()
	}
	// Update plugin state
	p.client = client
	p.dockerAvailable = true
	p.dockerError = ""
	if p.svc != nil {
		p.svc.client = client
	}
	if p.handler != nil {
		p.handler.client = client
	}
	return true
}

// dockerStatus returns the current Docker availability status.
// It performs a live check: if the cached state says Docker is unavailable
// but the binary is installed, it tries to reconnect.
func (p *Plugin) dockerStatus(c *gin.Context) {
	installed := false
	version := ""

	// Check if docker binary is installed.
	if path, err := exec.LookPath("docker"); err == nil && path != "" {
		installed = true
		if out, err := exec.Command("docker", "--version").Output(); err == nil {
			version = strings.TrimSpace(string(out))
		}
	}

	daemonRunning := p.dockerAvailable

	// Live check: if Docker binary is present but cached state says not available,
	// try to reconnect now. This handles the case where Docker was installed
	// after the plugin started.
	if installed && !daemonRunning {
		daemonRunning = p.tryReconnect()
	}

	// Also verify that a cached "available" state is still valid.
	if daemonRunning && p.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := p.client.Ping(ctx); err != nil {
			daemonRunning = false
			p.dockerAvailable = false
			p.dockerError = err.Error()
		}
	}

	errMsg := ""
	if !daemonRunning {
		errMsg = p.dockerError
	}

	c.JSON(http.StatusOK, gin.H{
		"installed":      installed,
		"daemon_running": daemonRunning,
		"version":        version,
		"error":          errMsg,
	})
}

// installDocker runs the EasyDocker script (https://github.com/web-casa/easydocker)
// and streams the output to the client via SSE.
func (p *Plugin) installDocker(c *gin.Context) {
	// Parse optional mirror parameter.
	var req struct {
		Mirror string `json:"mirror"` // none | public
	}
	_ = c.ShouldBindJSON(&req)

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

	// Quick check: if Docker and Compose are already installed and daemon is running,
	// just reconnect the plugin and skip the EasyDocker script entirely.
	if p.checkDockerAlreadyReady(writeSSE, writeEvent) {
		return
	}

	writeSSE("Downloading EasyDocker install script...")

	// Build command: download and run EasyDocker with non-interactive flags.
	// --mode install: skip the interactive mode selection menu
	// -y: skip all confirmation prompts
	// --mirror: select mirror (none by default, or user-specified)
	baseCmd := "curl -sSL https://raw.githubusercontent.com/web-casa/easydocker/main/docker.sh | bash -s -- --mode install -y"
	// Validate mirror to prevent command injection — only allow known safe values.
	switch req.Mirror {
	case "public":
		baseCmd += " --mirror public"
	case "":
		// no mirror flag
	default:
		writeEvent("error", fmt.Sprintf("unsupported mirror value: %q (allowed: \"public\" or empty)", req.Mirror))
		return
	}
	args := []string{"-c", baseCmd}

	cmd := exec.Command("bash", args...)
	// Merge stdout and stderr.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeSSE("ERROR: " + err.Error())
		writeEvent("error", err.Error())
		return
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		writeSSE("ERROR: " + err.Error())
		writeEvent("error", err.Error())
		return
	}

	// Stream output line by line.
	rebootDetected := false
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		// Strip ANSI color codes for cleaner SSE output.
		clean := stripANSI(line)
		if clean != "" {
			// Detect reboot signal from EasyDocker script
			if strings.Contains(strings.ToLower(clean), "reboot") {
				rebootDetected = true
			}
			writeSSE(clean)
		}
	}

	if err := cmd.Wait(); err != nil {
		// If a reboot was detected, this is expected — the script triggers a reboot
		// which kills the process. This is not a real failure.
		if rebootDetected {
			writeSSE("Server is rebooting to load new kernel modules...")
			writeEvent("reboot", "ok")
			return
		}
		writeSSE("ERROR: Installation failed: " + err.Error())
		writeEvent("error", "Installation failed: "+err.Error())
		return
	}

	writeSSE("Docker installation completed successfully!")

	// Try to reconnect to Docker daemon after installation.
	if p.tryReconnect() {
		writeSSE("Docker daemon connected successfully!")
	} else {
		writeSSE("Docker installed but daemon connection pending. Please start Docker if needed.")
	}

	writeEvent("done", "ok")
}

// checkDockerAlreadyReady checks if Docker and Docker Compose are already
// installed and the daemon is running. If so, it reconnects the plugin and
// returns true (the caller should return immediately). This avoids running
// the EasyDocker script unnecessarily.
func (p *Plugin) checkDockerAlreadyReady(writeSSE func(string), writeEvent func(string, string)) bool {
	// Check docker binary
	dockerPath, err := exec.LookPath("docker")
	if err != nil || dockerPath == "" {
		return false
	}

	// Check docker compose (plugin mode: "docker compose version")
	composeOut, err := exec.Command("docker", "compose", "version").CombinedOutput()
	if err != nil {
		return false
	}

	// Check daemon is running by trying to connect
	if !p.tryReconnect() {
		// Daemon binary exists but isn't running — try to start it
		writeSSE("Docker is installed but not running. Starting Docker service...")
		if startErr := exec.Command("systemctl", "start", "docker").Run(); startErr != nil {
			return false // couldn't start, let EasyDocker handle it
		}
		// Wait a moment and retry
		time.Sleep(2 * time.Second)
		if !p.tryReconnect() {
			return false
		}
	}

	// Docker + Compose are installed and daemon is running
	dockerVer := ""
	if out, err := exec.Command("docker", "--version").Output(); err == nil {
		dockerVer = strings.TrimSpace(string(out))
	}
	composeVer := strings.TrimSpace(string(composeOut))

	writeSSE("Docker is already installed and running!")
	writeSSE(fmt.Sprintf("  %s", dockerVer))
	writeSSE(fmt.Sprintf("  %s", composeVer))
	writeSSE("No installation needed.")
	writeEvent("done", "ok")
	return true
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm' or end of string.
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
			} else {
				i = j
			}
			continue
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// Start is called after Init. No background tasks needed yet.
func (p *Plugin) Start() error {
	return nil
}

// Stop closes the Docker client.
func (p *Plugin) Stop() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}

// FrontendManifest declares the frontend routes for the Docker plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "docker",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/docker", Component: "DockerOverview", Menu: true, Icon: "Container", Label: "Docker", LabelZh: "Docker 管理"},
			{Path: "/docker/containers", Component: "DockerContainers", Label: "Containers", LabelZh: "容器管理"},
			{Path: "/docker/images", Component: "DockerImages", Label: "Images", LabelZh: "镜像管理"},
			{Path: "/docker/networks", Component: "DockerNetworks", Label: "Networks", LabelZh: "网络管理"},
			{Path: "/docker/volumes", Component: "DockerVolumes", Label: "Volumes", LabelZh: "存储卷"},
			{Path: "/docker/settings", Component: "DockerSettings", Label: "Settings", LabelZh: "设置"},
		},
		MenuGroup: "deploy",
		MenuOrder: 10,
	}
}
