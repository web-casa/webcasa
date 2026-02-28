package deploy

import (
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for project deployment.
type Plugin struct {
	svc     *Service
	handler *Handler
}

// New creates a new deploy plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "deploy",
		Name:        "Project Deploy",
		Version:     "1.0.0",
		Description: "Source code deployment for Node.js, Go, PHP, Python projects with auto-detection and process management",
		Author:      "Web.Casa",
		Priority:    20,
		Icon:        "Rocket",
		Category:    "deploy",
	}
}

// Init initialises the deploy plugin: migrates DB, creates service, registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Migrate models
	if err := ctx.DB.AutoMigrate(&Project{}, &Deployment{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Create service and handler
	p.svc = NewService(ctx.DB, ctx.CoreAPI, ctx.Logger, ctx.DataDir)
	p.handler = NewHandler(p.svc)

	// Register API routes under /api/plugins/deploy/
	r := ctx.Router

	// Frameworks presets
	r.GET("/frameworks", p.handler.GetFrameworks)
	r.GET("/detect", p.handler.DetectFramework)

	// Projects CRUD
	r.GET("/projects", p.handler.ListProjects)
	r.POST("/projects", p.handler.CreateProject)
	r.GET("/projects/:id", p.handler.GetProject)
	r.PUT("/projects/:id", p.handler.UpdateProject)
	r.DELETE("/projects/:id", p.handler.DeleteProject)

	// Project actions
	r.POST("/projects/:id/build", p.handler.BuildProject)
	r.POST("/projects/:id/start", p.handler.StartProject)
	r.POST("/projects/:id/stop", p.handler.StopProject)
	r.POST("/projects/:id/rollback", p.handler.RollbackProject)

	// Deployments & logs
	r.GET("/projects/:id/deployments", p.handler.GetDeployments)
	r.GET("/projects/:id/logs", p.handler.GetBuildLog)

	// Webhook — public route (no JWT required, uses random token for auth)
	ctx.PublicRouter.POST("/webhook/:token", p.handler.Webhook)

	ctx.Logger.Info("Deploy plugin routes registered")
	return nil
}

// Start is called after Init. No background tasks needed.
func (p *Plugin) Start() error {
	return nil
}

// Stop cleans up resources.
func (p *Plugin) Stop() error {
	return nil
}

// FrontendManifest declares the frontend routes for the deploy plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "deploy",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/deploy", Component: "ProjectList", Menu: true, Icon: "Rocket", Label: "Projects", LabelZh: "项目部署"},
			{Path: "/deploy/create", Component: "ProjectCreate", Label: "Create Project", LabelZh: "创建项目"},
			{Path: "/deploy/:id", Component: "ProjectDetail", Label: "Project Detail", LabelZh: "项目详情"},
		},
		MenuGroup: "deploy",
		MenuOrder: 20,
	}
}
