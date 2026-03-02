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
	r := ctx.Router

	// Engines
	r.GET("/engines", p.handler.ListEngines)

	// Instances
	r.GET("/instances", p.handler.ListInstances)
	r.POST("/instances", p.handler.CreateInstance)
	r.GET("/instances/:id", p.handler.GetInstance)
	r.DELETE("/instances/:id", p.handler.DeleteInstance)
	r.POST("/instances/:id/start", p.handler.StartInstance)
	r.POST("/instances/:id/stop", p.handler.StopInstance)
	r.POST("/instances/:id/restart", p.handler.RestartInstance)
	r.GET("/instances/:id/logs", p.handler.InstanceLogs)
	r.GET("/instances/:id/logs/ws", p.handler.InstanceLogsWS)
	r.GET("/instances/:id/connection", p.handler.GetConnectionInfo)
	r.GET("/instances/:id/password", p.handler.GetRootPassword)

	// Database CRUD
	r.GET("/instances/:id/databases", p.handler.ListDatabases)
	r.POST("/instances/:id/databases", p.handler.CreateDatabase)
	r.DELETE("/instances/:id/databases/:dbname", p.handler.DeleteDatabase)

	// User CRUD
	r.GET("/instances/:id/users", p.handler.ListUsers)
	r.POST("/instances/:id/users", p.handler.CreateUser)
	r.DELETE("/instances/:id/users/:username", p.handler.DeleteUser)

	// Query execution
	r.POST("/instances/:id/query", p.handler.ExecuteQuery)

	// SQLite Browser
	r.GET("/sqlite/tables", p.handler.SQLiteTables)
	r.GET("/sqlite/schema", p.handler.SQLiteSchema)
	r.POST("/sqlite/query", p.handler.SQLiteQuery)

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
