package appstore

import (
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the App Store plugin.
type Plugin struct {
	svc     *Service
	tplSvc  *TemplateService
	updater *Updater
	handler *Handler
}

// New creates an App Store plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:           "appstore",
		Name:         "App Store",
		Version:      "1.0.0",
		Description:  "One-click Docker app installation and project template marketplace",
		Author:       "Web.Casa",
		Dependencies: []string{"docker"},
		Priority:     60,
		Icon:         "Store",
		Category:     "deploy",
	}
}

// Init initializes the plugin: migrate DB, create services, register routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// 1. Auto-migrate all models
	if err := ctx.DB.AutoMigrate(
		&AppSource{},
		&AppDefinition{},
		&InstalledApp{},
		&ProjectTemplate{},
	); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// 2. Seed official sources
	SeedOfficialSources(ctx.DB)

	// 3. Create services
	sourceMgr := NewSourceManager(ctx.DB, ctx.DataDir, ctx.Logger)
	p.svc = NewService(ctx.DB, sourceMgr, ctx.CoreAPI, ctx.EventBus, ctx.Logger, ctx.DataDir)
	p.tplSvc = NewTemplateService(ctx.DB, sourceMgr, ctx.Logger, ctx.DataDir)
	p.updater = NewUpdater(ctx.DB, sourceMgr, ctx.Logger)
	p.handler = NewHandler(p.svc, p.tplSvc)

	// 4. Register routes
	r := ctx.Router

	// App catalog
	r.GET("/apps", p.handler.ListApps)
	r.GET("/apps/:id", p.handler.GetApp)
	r.GET("/apps/:id/logo", p.handler.AppLogo)
	r.GET("/categories", p.handler.ListCategories)

	// Sources
	r.GET("/sources", p.handler.ListSources)
	r.POST("/sources", p.handler.AddSource)
	r.POST("/sources/:id/sync", p.handler.SyncSource)
	r.DELETE("/sources/:id", p.handler.RemoveSource)

	// Installed apps
	r.GET("/installed", p.handler.ListInstalled)
	r.GET("/installed/:id", p.handler.GetInstalled)
	r.POST("/install", p.handler.InstallApp)
	r.POST("/installed/:id/start", p.handler.StartApp)
	r.POST("/installed/:id/stop", p.handler.StopApp)
	r.POST("/installed/:id/update", p.handler.UpdateApp)
	r.DELETE("/installed/:id", p.handler.UninstallApp)
	r.GET("/updates", p.handler.CheckUpdates)

	// Project templates
	r.GET("/templates", p.handler.ListTemplates)
	r.GET("/templates/:id", p.handler.GetTemplate)
	r.POST("/templates/deploy", p.handler.DeployFromTemplate)

	ctx.Logger.Info("App Store plugin initialized")
	return nil
}

// Start begins the background update checker.
func (p *Plugin) Start() error {
	p.updater.Start()
	return nil
}

// Stop stops the background update checker.
func (p *Plugin) Stop() error {
	p.updater.Stop()
	return nil
}

// FrontendManifest returns the frontend routes for the App Store.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "appstore",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/store", Component: "AppStore", Menu: true, Icon: "Store", Label: "App Store", LabelZh: "应用商店"},
			{Path: "/store/app/:id", Component: "AppDetail", Label: "App Detail", LabelZh: "应用详情"},
			{Path: "/store/templates", Component: "TemplateMarket", Menu: true, Icon: "LayoutTemplate", Label: "Templates", LabelZh: "项目模板"},
		},
		MenuGroup: "deploy",
		MenuOrder: 5,
	}
}

// Compile-time interface checks.
var (
	_ pluginpkg.Plugin           = (*Plugin)(nil)
	_ pluginpkg.FrontendProvider = (*Plugin)(nil)
)
