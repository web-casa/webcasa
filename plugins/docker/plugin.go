package docker

import (
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for Docker management.
type Plugin struct {
	client  *Client
	svc     *Service
	handler *Handler
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
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Read socket path from plugin config, default to /var/run/docker.sock.
	socketPath := ctx.ConfigStore.Get("socket_path")
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}

	// Connect to Docker daemon.
	client, err := NewClient(socketPath)
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	p.client = client

	// Migrate models.
	if err := ctx.DB.AutoMigrate(&Stack{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Create service and handler.
	p.svc = NewService(ctx.DB, client, ctx.DataDir, ctx.Logger)
	p.handler = NewHandler(p.svc, client)

	// Register API routes under /api/plugins/docker/
	r := ctx.Router

	// System
	r.GET("/info", p.handler.Info)

	// Stacks (simple mode)
	r.GET("/stacks", p.handler.ListStacks)
	r.POST("/stacks", p.handler.CreateStack)
	r.GET("/stacks/:id", p.handler.GetStack)
	r.PUT("/stacks/:id", p.handler.UpdateStack)
	r.DELETE("/stacks/:id", p.handler.DeleteStack)
	r.POST("/stacks/:id/up", p.handler.StackUp)
	r.POST("/stacks/:id/down", p.handler.StackDown)
	r.POST("/stacks/:id/restart", p.handler.StackRestart)
	r.POST("/stacks/:id/pull", p.handler.StackPull)
	r.GET("/stacks/:id/logs", p.handler.StackLogs)

	// Containers (advanced mode)
	r.GET("/containers", p.handler.ListContainers)
	r.POST("/containers/:id/start", p.handler.StartContainer)
	r.POST("/containers/:id/stop", p.handler.StopContainer)
	r.POST("/containers/:id/restart", p.handler.RestartContainer)
	r.DELETE("/containers/:id", p.handler.RemoveContainer)
	r.GET("/containers/:id/logs", p.handler.ContainerLogs)
	r.GET("/containers/:id/stats", p.handler.ContainerStats)

	// Images
	r.GET("/images", p.handler.ListImages)
	r.POST("/images/pull", p.handler.PullImage)
	r.DELETE("/images/:id", p.handler.RemoveImage)
	r.POST("/images/prune", p.handler.PruneImages)
	r.GET("/images/search", p.handler.SearchImages)

	// Networks
	r.GET("/networks", p.handler.ListNetworks)
	r.POST("/networks", p.handler.CreateNetwork)
	r.DELETE("/networks/:id", p.handler.RemoveNetwork)

	// Volumes
	r.GET("/volumes", p.handler.ListVolumes)
	r.POST("/volumes", p.handler.CreateVolume)
	r.DELETE("/volumes/:id", p.handler.RemoveVolume)

	// WebSocket log streaming
	r.GET("/containers/:id/logs/ws", p.handler.ContainerLogsWS)
	r.GET("/stacks/:id/logs/ws", p.handler.StackLogsWS)

	ctx.Logger.Info("Docker plugin routes registered")
	return nil
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
