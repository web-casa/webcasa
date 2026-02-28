package plugin

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Plugin is the interface that all plugins must implement.
type Plugin interface {
	// Metadata returns the plugin's metadata.
	Metadata() Metadata

	// Init is called once during startup. The plugin should register
	// API routes, run database migrations, and perform any one-time setup.
	Init(ctx *Context) error

	// Start is called after Init to start background tasks.
	Start() error

	// Stop is called during shutdown to clean up resources.
	Stop() error
}

// Metadata describes a plugin.
type Metadata struct {
	ID           string   `json:"id"`           // unique identifier, e.g. "docker"
	Name         string   `json:"name"`         // display name, e.g. "Docker 管理"
	Version      string   `json:"version"`      // semver, e.g. "1.0.0"
	Description  string   `json:"description"`  // short description
	Author       string   `json:"author"`       // author name
	Dependencies []string `json:"dependencies"` // IDs of plugins this one depends on
	Priority     int      `json:"priority"`     // load order (lower = earlier)
	Icon         string   `json:"icon"`         // Lucide icon name
	Category     string   `json:"category"`     // "deploy", "database", "tool", "monitor"
}

// Context is the runtime context provided to plugins during Init.
type Context struct {
	DB           *gorm.DB         // database connection (use plugin-prefixed tables)
	Router       *gin.RouterGroup // API route group: /api/plugins/{id}/ (requires JWT)
	PublicRouter *gin.RouterGroup // public API route group: /api/plugins/{id}/ (no JWT)
	EventBus     *EventBus        // publish/subscribe event bus
	Logger       *slog.Logger     // structured logger with plugin ID prefix
	DataDir      string           // plugin-specific data directory
	ConfigStore  *ConfigStore     // plugin configuration reader/writer
	CoreAPI      CoreAPI          // access to core panel functionality
}

// CoreAPI exposes core panel functionality to plugins.
type CoreAPI interface {
	// Host management — lets plugins create reverse proxy entries automatically.
	CreateHost(req CreateHostRequest) (uint, error)
	DeleteHost(id uint) error
	ReloadCaddy() error

	// Settings — shared key-value store.
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error

	// GetDB returns the core database connection for read-only queries.
	// Plugins should NOT write to core tables directly.
	GetDB() *gorm.DB
}

// CreateHostRequest is the minimal set of fields a plugin needs to create a
// reverse proxy entry. It is intentionally simpler than model.HostCreateRequest
// to keep the plugin API surface small.
type CreateHostRequest struct {
	Domain       string `json:"domain"`
	UpstreamAddr string `json:"upstream_addr"` // e.g. "localhost:3000"
	TLSEnabled   bool   `json:"tls_enabled"`
	HTTPRedirect bool   `json:"http_redirect"`
	WebSocket    bool   `json:"websocket"`
}

// PluginInfo is the serialisable representation returned by the management API.
type PluginInfo struct {
	Metadata
	Enabled bool `json:"enabled"`
}

// FrontendManifest declares the routes and menu items a plugin contributes to
// the frontend. Plugins return this from an optional FrontendProvider interface.
type FrontendManifest struct {
	ID        string          `json:"id"`
	Routes    []FrontendRoute `json:"routes"`
	MenuGroup string          `json:"menu_group"` // sidebar group: "deploy", "database", "tool"
	MenuOrder int             `json:"menu_order"` // sort order within group
}

// FrontendRoute describes a single frontend route contributed by a plugin.
type FrontendRoute struct {
	Path      string `json:"path"`      // e.g. "/docker"
	Component string `json:"component"` // component file name (without .jsx)
	Menu      bool   `json:"menu"`      // whether to show in sidebar
	Icon      string `json:"icon"`      // Lucide icon name (for menu)
	Label     string `json:"label"`     // menu label
	LabelZh   string `json:"label_zh"`  // Chinese label (i18n)
}

// FrontendProvider is an optional interface plugins can implement to declare
// frontend routes and sidebar menu items.
type FrontendProvider interface {
	FrontendManifest() FrontendManifest
}
