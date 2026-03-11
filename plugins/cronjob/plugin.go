package cronjob

import (
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for cron job management.
type Plugin struct {
	svc     *Service
	handler *Handler
}

// New creates a new cronjob plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "cronjob",
		Name:        "Cron Jobs",
		Version:     "1.0.0",
		Description: "General-purpose scheduled task management with shell command execution",
		Author:      "Web.Casa",
		Priority:    50,
		Icon:        "Clock",
		Category:    "management",
	}
}

// Init migrates the database, creates the service, and registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	if err := ctx.DB.AutoMigrate(&CronTask{}, &CronLog{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	p.svc = NewService(ctx.DB, ctx.Logger, ctx.EventBus)
	p.handler = NewHandler(p.svc)

	r := ctx.Router      // JWT required
	a := ctx.AdminRouter // JWT + admin

	r.GET("/tasks", p.handler.ListTasks)
	r.GET("/tasks/:id", p.handler.GetTask)
	a.POST("/tasks", p.handler.CreateTask)
	a.PUT("/tasks/:id", p.handler.UpdateTask)
	a.DELETE("/tasks/:id", p.handler.DeleteTask)
	a.POST("/tasks/:id/trigger", p.handler.TriggerTask)
	r.GET("/tasks/:id/logs", p.handler.ListTaskLogs)
	r.GET("/logs", p.handler.ListAllLogs)

	ctx.Logger.Info("Cron job plugin routes registered")
	return nil
}

// Start begins the cron scheduler.
func (p *Plugin) Start() error {
	if p.svc != nil {
		p.svc.Start()
	}
	return nil
}

// Stop cleans up resources.
func (p *Plugin) Stop() error {
	if p.svc != nil {
		p.svc.Stop()
	}
	return nil
}

// FrontendManifest declares the frontend routes for the cron job plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "cronjob",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/cronjob", Component: "CronJobManager", Menu: true, Icon: "Clock", Label: "Cron Jobs", LabelZh: "定时任务"},
		},
		MenuGroup: "tool",
		MenuOrder: 42,
	}
}

// Compile-time interface checks.
var (
	_ pluginpkg.Plugin           = (*Plugin)(nil)
	_ pluginpkg.FrontendProvider = (*Plugin)(nil)
)
