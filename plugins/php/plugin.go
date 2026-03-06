package php

import (
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for PHP management.
type Plugin struct {
	svc     *Service
	handler *Handler
}

// New creates a new PHP plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:           "php",
		Name:         "PHP",
		Version:      "1.0.0",
		Description:  "Manage PHP-FPM and FrankenPHP runtimes, create PHP websites with one click",
		Author:       "Web.Casa",
		Dependencies: []string{"docker"},
		Priority:     16,
		Icon:         "FileCode",
		Category:     "deploy",
	}
}

// Init initialises the PHP plugin: migrates DB, registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Migrate models.
	if err := ctx.DB.AutoMigrate(&PHPRuntime{}, &PHPSite{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Create service and handler.
	p.svc = NewService(ctx.DB, ctx.DataDir, ctx.Logger, ctx.CoreAPI)
	p.handler = NewHandler(p.svc)

	// Register API routes under /api/plugins/php/
	r := ctx.Router      // read-only
	a := ctx.AdminRouter // admin-only

	// Versions & extensions catalog (read)
	r.GET("/versions", p.handler.ListVersions)
	r.GET("/common-extensions", p.handler.ListCommonExtensions)

	// Runtimes (read + admin mutations)
	r.GET("/runtimes", p.handler.ListRuntimes)
	a.POST("/runtimes", p.handler.CreateRuntimeStream)
	a.DELETE("/runtimes/:id", p.handler.DeleteRuntime)
	a.POST("/runtimes/:id/start", p.handler.StartRuntime)
	a.POST("/runtimes/:id/stop", p.handler.StopRuntime)
	a.POST("/runtimes/:id/restart", p.handler.RestartRuntime)
	r.GET("/runtimes/:id/logs", p.handler.RuntimeLogs)

	// Config (read + admin)
	r.GET("/runtimes/:id/config", p.handler.GetConfig)
	a.PUT("/runtimes/:id/config", p.handler.UpdateConfig)
	a.POST("/runtimes/:id/optimize", p.handler.Optimize)

	// Extensions (read + admin)
	r.GET("/runtimes/:id/extensions", p.handler.GetExtensions)
	a.POST("/runtimes/:id/extensions", p.handler.InstallExtensions)
	a.DELETE("/runtimes/:id/extensions/:name", p.handler.RemoveExtension)

	// Sites (read + admin)
	r.GET("/sites", p.handler.ListSites)
	a.POST("/sites", p.handler.CreateSiteStream)
	r.GET("/sites/:id", p.handler.GetSite)
	a.PUT("/sites/:id", p.handler.UpdateSite)
	a.DELETE("/sites/:id", p.handler.DeleteSite)

	// System info for tuning
	r.GET("/system-info", p.handler.GetSystemInfo)
	r.GET("/tuning-presets", p.handler.GetTuningPresets)

	ctx.Logger.Info("PHP plugin routes registered")
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

// FrontendManifest declares the frontend routes for the PHP plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "php",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/php", Component: "PHPManager", Menu: true, Icon: "FileCode", Label: "PHP", LabelZh: "PHP"},
		},
		MenuGroup: "deploy",
		MenuOrder: 17,
	}
}

// GetService returns the service for CoreAPI integration.
func (p *Plugin) GetService() *Service {
	return p.svc
}

// Compile-time interface checks.
var (
	_ pluginpkg.Plugin           = (*Plugin)(nil)
	_ pluginpkg.FrontendProvider = (*Plugin)(nil)
)
