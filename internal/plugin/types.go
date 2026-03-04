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
	AdminRouter  *gin.RouterGroup // API route group: /api/plugins/{id}/ (requires JWT + admin role)
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
	ListHosts() ([]map[string]interface{}, error)
	GetHost(id uint) (map[string]interface{}, error)
	UpdateHostUpstream(hostID uint, newUpstream string) error
	ReloadCaddy() error

	// Settings — shared key-value store.
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error

	// GetDB returns the core database connection for read-only queries.
	// Plugins should NOT write to core tables directly.
	GetDB() *gorm.DB

	// Cross-plugin queries — used by AI tool use.
	ListProjects() ([]map[string]interface{}, error)
	GetProject(id uint) (map[string]interface{}, error)
	GetBuildLog(projectID uint, buildNum int) (string, error)
	GetRuntimeLog(projectID uint, lines int) (string, error)
	TriggerBuild(projectID uint) error
	CreateProject(req CreateProjectRequest) (uint, error)
	GetEnvSuggestions(framework string) ([]map[string]interface{}, error)
	DockerPS() ([]map[string]interface{}, error)
	DockerLogs(containerID string, tail int) (string, error)
	GetMetrics() (map[string]interface{}, error)
	RunCommand(cmd string, timeoutSec int) (string, error)

	// Batch 2 additions
	TriggerBackup() error
	UpdateHost(id uint, req UpdateHostRequest) error
	GetRecentAlerts() ([]map[string]interface{}, error)

	// Batch 3: Database management
	DatabaseListInstances() ([]map[string]interface{}, error)
	DatabaseCreateInstance(req DatabaseCreateInstanceRequest) (uint, error)
	DatabaseCreateDatabase(instanceID uint, name, charset string) error
	DatabaseCreateUser(instanceID uint, username, password string, databases []string) error
	DatabaseExecuteQuery(instanceID uint, database, query string) (map[string]interface{}, error)

	// Batch 3: Docker extended
	DockerListStacks() ([]map[string]interface{}, error)
	DockerManageContainer(containerID, action string) error
	DockerRunContainer(req DockerRunContainerRequest) (string, error)
	DockerPullImage(image string) error
	DockerGetContainerStats(containerID string) (map[string]interface{}, error)

	// Batch 3: App Store
	AppStoreSearchApps(query string) ([]map[string]interface{}, error)
	AppStoreInstallApp(appID string, config map[string]interface{}) (uint, error)
	AppStoreListInstalled() ([]map[string]interface{}, error)

	// Batch 3: File write operations
	FileWrite(path, content string) error
	FileDelete(path string) error
	FileRename(oldPath, newPath string) error
}

// UpdateHostRequest describes fields that can be changed on an existing host via AI.
type UpdateHostRequest struct {
	Upstream     string `json:"upstream,omitempty"`
	TLSMode      string `json:"tls_mode,omitempty"`      // auto, dns, custom, off
	ForceHTTPS   *bool  `json:"force_https,omitempty"`
	WebSocket    *bool  `json:"websocket,omitempty"`
	Compression  *bool  `json:"compression,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty"`
}

// CreateProjectRequest is the set of fields needed to create a deployment project via AI.
type CreateProjectRequest struct {
	Name         string `json:"name"`
	GitURL       string `json:"git_url"`
	GitBranch    string `json:"git_branch"`
	Domain       string `json:"domain"`
	Framework    string `json:"framework"`     // optional, auto-detect if empty
	DeployMode   string `json:"deploy_mode"`   // bare | docker, default: bare
	AutoDeploy   bool   `json:"auto_deploy"`
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
	Enabled       bool `json:"enabled"`
	ShowInSidebar bool `json:"show_in_sidebar"`
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

// DatabaseCreateInstanceRequest holds parameters for creating a database instance.
type DatabaseCreateInstanceRequest struct {
	Engine       string `json:"engine"`        // mysql, postgres, mariadb, redis
	Version      string `json:"version"`       // e.g. "8.0", "16", "7.2"
	Name         string `json:"name"`          // display name
	Port         int    `json:"port"`          // host port
	RootPassword string `json:"root_password"` // root/admin password
	MemoryLimit  string `json:"memory_limit"`  // e.g. "512m", "1g"
}

// DockerRunContainerRequest holds parameters for running a standalone container.
type DockerRunContainerRequest struct {
	Image         string            `json:"image"`          // e.g. "nginx:latest"
	Name          string            `json:"name"`           // container name
	Ports         []string          `json:"ports"`          // e.g. ["8080:80"]
	Env           map[string]string `json:"env"`            // environment variables
	Volumes       []string          `json:"volumes"`        // e.g. ["/data:/var/lib/data"]
	RestartPolicy string            `json:"restart_policy"` // no, always, unless-stopped, on-failure
}
