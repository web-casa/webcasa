package backup

import (
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for backup management.
type Plugin struct {
	svc     *Service
	handler *Handler
}

// New creates a new backup plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "backup",
		Name:        "Backup Manager",
		Version:     "1.0.0",
		Description: "Backup and restore panel data, Docker volumes, and databases via Kopia",
		Author:      "Web.Casa",
		Priority:    55,
		Icon:        "HardDrive",
		Category:    "tool",
	}
}

// Init initialises the backup plugin: migrates DB, registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Migrate models.
	if err := ctx.DB.AutoMigrate(&BackupConfig{}, &BackupSnapshot{}, &BackupLog{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Create service and handler.
	p.svc = NewService(ctx.DB, ctx.DataDir, ctx.Logger)
	p.handler = NewHandler(p.svc)

	// Register API routes under /api/plugins/backup/
	r := ctx.Router       // read-only
	a := ctx.AdminRouter  // admin-only

	// Install Kopia (admin-only, SSE streaming)
	a.POST("/install-kopia", p.handler.InstallKopia)

	// Config (read + admin mutations)
	r.GET("/dependency-check", p.handler.CheckDependency)
	r.GET("/config", p.handler.GetConfig)
	a.PUT("/config", p.handler.UpdateConfig)
	a.POST("/config/test", p.handler.TestConnection)

	// Snapshots (read + admin mutations)
	r.GET("/snapshots", p.handler.ListSnapshots)
	a.POST("/snapshots", p.handler.CreateSnapshot)
	a.POST("/snapshots/:id/restore", p.handler.RestoreSnapshot)
	a.DELETE("/snapshots/:id", p.handler.DeleteSnapshot)

	// Status & Logs (read)
	r.GET("/status", p.handler.GetStatus)
	r.GET("/logs", p.handler.ListLogs)

	// Subscribe to backup.trigger event (from AI tool or other plugins).
	// Use TriggerAsync so the goroutine is tracked by Service.wg —
	// Plugin.Stop() then drains it instead of leaving it to write to a
	// DB that's about to close.
	ctx.EventBus.Subscribe("backup.trigger", func(e pluginpkg.Event) {
		ctx.Logger.Info("Backup triggered via event", "source", e.Source)
		p.svc.TriggerAsync("ai-triggered")
	})

	ctx.Logger.Info("Backup plugin routes registered")
	return nil
}

// Start begins the backup scheduler.
func (p *Plugin) Start() error {
	if p.svc != nil {
		return p.svc.Start()
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

// FrontendManifest declares the frontend routes for the backup plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "backup",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/backup", Component: "BackupManager", Menu: true, Icon: "HardDrive", Label: "Backup", LabelZh: "备份管理"},
		},
		MenuGroup: "tool",
		MenuOrder: 55,
	}
}

// Compile-time interface checks.
var (
	_ pluginpkg.Plugin           = (*Plugin)(nil)
	_ pluginpkg.FrontendProvider = (*Plugin)(nil)
)
