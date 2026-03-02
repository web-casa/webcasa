package docker

import (
	"net/http"
	"os/exec"

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
	socketPath := ctx.ConfigStore.Get("socket_path")
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}

	// Connect to Docker daemon (graceful: don't fail if Docker is unavailable).
	client, err := NewClient(socketPath)
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

	// Register API routes under /api/plugins/docker/
	r := ctx.Router      // read-only
	a := ctx.AdminRouter // admin-only

	// Docker status endpoint (always available, even if Docker is not installed).
	r.GET("/status", p.dockerStatus)

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

// dockerStatus returns the current Docker availability status.
func (p *Plugin) dockerStatus(c *gin.Context) {
	installed := false
	daemonRunning := p.dockerAvailable
	version := ""
	errMsg := p.dockerError

	// Check if docker binary is installed.
	if path, err := exec.LookPath("docker"); err == nil && path != "" {
		installed = true
		// Try to get version.
		if out, err := exec.Command("docker", "--version").Output(); err == nil {
			version = string(out)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"installed":      installed,
		"daemon_running": daemonRunning,
		"version":        version,
		"error":          errMsg,
	})
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
		},
		MenuGroup: "deploy",
		MenuOrder: 10,
	}
}
