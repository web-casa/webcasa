package firewall

import (
	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for firewall management.
type Plugin struct {
	svc     *Service
	handler *Handler
}

// New creates a new firewall plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "firewall",
		Name:        "Firewall",
		Version:     "1.0.0",
		Description: "Manage firewalld rules via web UI",
		Author:      "Web.Casa",
		Priority:    45,
		Icon:        "Shield",
		Category:    "tool",
	}
}

// Init initialises the firewall plugin: creates service, registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Get the panel port to protect it from removal.
	panelPort := "39921"
	if v, err := ctx.CoreAPI.GetSetting("port"); err == nil && v != "" {
		panelPort = v
	}

	p.svc = NewService(ctx.Logger, panelPort)
	p.handler = NewHandler(p.svc)

	r := ctx.Router      // read-only (JWT)
	a := ctx.AdminRouter // admin-only

	r.GET("/status", p.handler.GetStatus)
	r.GET("/zones", p.handler.ListZones)
	r.GET("/zones/:name", p.handler.GetZone)
	r.GET("/available-services", p.handler.AvailableServices)

	a.POST("/ports", p.handler.AddPort)
	a.DELETE("/ports", p.handler.RemovePort)
	a.POST("/services", p.handler.AddService)
	a.DELETE("/services", p.handler.RemoveService)
	a.POST("/rich-rules", p.handler.AddRichRule)
	a.DELETE("/rich-rules", p.handler.RemoveRichRule)
	a.POST("/reload", p.handler.ReloadFirewall)
	a.POST("/start", p.handler.StartFirewalld)
	a.POST("/install", p.handler.InstallFirewalld)

	ctx.Logger.Info("Firewall plugin routes registered")
	return nil
}

// Start is a no-op for the firewall plugin.
func (p *Plugin) Start() error { return nil }

// Stop is a no-op for the firewall plugin.
func (p *Plugin) Stop() error { return nil }

// FrontendManifest declares the frontend routes for the firewall plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "firewall",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/firewall", Component: "FirewallManager", Menu: true, Icon: "Shield", Label: "Firewall", LabelZh: "防火墙"},
		},
		MenuGroup: "tool",
		MenuOrder: 45,
	}
}

// Compile-time interface checks.
var (
	_ pluginpkg.Plugin           = (*Plugin)(nil)
	_ pluginpkg.FrontendProvider = (*Plugin)(nil)
)
