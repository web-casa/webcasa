package database

import (
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for database management.
type Plugin struct {
	svc     *Service
	handler *Handler
	sqlite  *SQLiteBrowser
}

// New creates a new database plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:           "database",
		Name:         "Database",
		Version:      "1.0.0",
		Description:  "Create and manage MySQL, PostgreSQL, MariaDB, Redis instances via Docker",
		Author:       "Web.Casa",
		Dependencies: []string{"docker"},
		Priority:     15,
		Icon:         "Database",
		Category:     "database",
	}
}

// Init initialises the database plugin: migrates DB, registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Migrate models.
	if err := ctx.DB.AutoMigrate(&Instance{}, &Database{}, &DatabaseUser{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Create service and handler.
	p.svc = NewService(ctx.DB, ctx.DataDir, ctx.Logger)
	p.sqlite = NewSQLiteBrowser(ctx.Logger)
	p.handler = NewHandler(p.svc, p.sqlite)

	// Register API routes under /api/plugins/database/
	r := ctx.Router       // read-only
	a := ctx.AdminRouter  // admin-only

	// Engines (read)
	r.GET("/engines", p.handler.ListEngines)
	r.GET("/presets", p.handler.GetPresets)

	// Instances (read + admin mutations)
	r.GET("/instances", p.handler.ListInstances)
	a.POST("/instances", p.handler.CreateInstance)
	a.POST("/instances/stream", p.handler.CreateInstanceStream)
	r.GET("/instances/:id", p.handler.GetInstance)
	a.DELETE("/instances/:id", p.handler.DeleteInstance)
	a.POST("/instances/:id/start", p.handler.StartInstance)
	a.POST("/instances/:id/stop", p.handler.StopInstance)
	a.POST("/instances/:id/restart", p.handler.RestartInstance)
	r.GET("/instances/:id/logs", p.handler.InstanceLogs)
	r.GET("/instances/:id/logs/ws", p.handler.InstanceLogsWS)
	r.GET("/instances/:id/connection", p.handler.GetConnectionInfo)
	a.GET("/instances/:id/password", p.handler.GetRootPassword) // sensitive

	// Database CRUD (admin)
	r.GET("/instances/:id/databases", p.handler.ListDatabases)
	a.POST("/instances/:id/databases", p.handler.CreateDatabase)
	a.DELETE("/instances/:id/databases/:dbname", p.handler.DeleteDatabase)

	// User CRUD (admin)
	r.GET("/instances/:id/users", p.handler.ListUsers)
	a.POST("/instances/:id/users", p.handler.CreateUser)
	a.DELETE("/instances/:id/users/:username", p.handler.DeleteUser)

	// Query execution (admin)
	a.POST("/instances/:id/query", p.handler.ExecuteQuery)

	// SQLite Browser (read + admin query)
	r.GET("/sqlite/tables", p.handler.SQLiteTables)
	r.GET("/sqlite/schema", p.handler.SQLiteSchema)
	a.POST("/sqlite/query", p.handler.SQLiteQuery)

	ctx.Logger.Info("Database plugin routes registered")
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

// FrontendManifest declares the frontend routes for the database plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "database",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/database", Component: "DatabaseInstances", Menu: true, Icon: "Database", Label: "Database", LabelZh: "数据库"},
			{Path: "/database/sqlite", Component: "SQLiteBrowser", Label: "SQLite Browser", LabelZh: "SQLite 浏览器"},
			{Path: "/database/query", Component: "DatabaseQuery", Label: "Query Console", LabelZh: "查询控制台"},
			{Path: "/database/:id", Component: "DatabaseDetail", Label: "Instance Detail", LabelZh: "实例详情"},
		},
		MenuGroup: "database",
		MenuOrder: 15,
	}
}

// Compile-time interface checks.
var (
	_ pluginpkg.Plugin           = (*Plugin)(nil)
	_ pluginpkg.FrontendProvider = (*Plugin)(nil)
)
