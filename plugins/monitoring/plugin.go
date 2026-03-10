package monitoring

import (
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for system monitoring.
type Plugin struct {
	svc     *Service
	handler *Handler
}

// New creates a new monitoring plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "monitoring",
		Name:        "System Monitoring",
		Version:     "1.0.0",
		Description: "Real-time system metrics, historical charts, and threshold alerts",
		Author:      "Web.Casa",
		Priority:    50,
		Icon:        "Activity",
		Category:    "monitor",
	}
}

// Init initialises the monitoring plugin: migrates DB, registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Migrate models.
	if err := ctx.DB.AutoMigrate(&MetricRecord{}, &AlertRule{}, &AlertHistory{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Create service and handler.
	p.svc = NewService(ctx.DB, ctx.Logger, ctx.EventBus)
	p.handler = NewHandler(p.svc)

	// Register API routes under /api/plugins/monitoring/
	r := ctx.Router       // read-only
	a := ctx.AdminRouter  // admin-only

	// Metrics (read)
	r.GET("/metrics/current", p.handler.GetCurrent)
	r.GET("/metrics/history", p.handler.GetHistory)
	r.GET("/metrics/containers", p.handler.GetContainers)
	r.GET("/metrics/ws", p.handler.MetricsWS)

	// Alerts (read + admin mutations)
	r.GET("/alerts", p.handler.ListAlertRules)
	a.POST("/alerts", p.handler.CreateAlertRule)
	a.PUT("/alerts/:id", p.handler.UpdateAlertRule)
	a.DELETE("/alerts/:id", p.handler.DeleteAlertRule)
	r.GET("/alerts/history", p.handler.ListAlertHistory)

	ctx.Logger.Info("Monitoring plugin routes registered")
	return nil
}

// Start begins the background metric collection goroutine.
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

// FrontendManifest declares the frontend routes for the monitoring plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "monitoring",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/monitoring", Component: "MonitoringDashboard", Menu: true, Icon: "Activity", Label: "Monitoring", LabelZh: "系统监控"},
		},
		MenuGroup: "monitor",
		MenuOrder: 50,
	}
}

// Compile-time interface checks.
var (
	_ pluginpkg.Plugin           = (*Plugin)(nil)
	_ pluginpkg.FrontendProvider = (*Plugin)(nil)
)
