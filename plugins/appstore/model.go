package appstore

import "time"

// ── App Source ──

// AppSource represents a Git repository that contains app definitions.
type AppSource struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	Name       string     `gorm:"size:128;not null" json:"name"`
	URL        string     `gorm:"size:512;not null" json:"url"`
	Branch     string     `gorm:"size:64;default:main" json:"branch"`
	Kind       string     `gorm:"size:16;default:app" json:"kind"` // "app" or "template"
	IsDefault  bool       `gorm:"default:false" json:"is_default"`
	LastSyncAt *time.Time `json:"last_sync_at"`
	SyncStatus string     `gorm:"size:16;default:pending" json:"sync_status"` // pending, syncing, synced, error
	SyncError  string     `gorm:"type:text" json:"sync_error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (AppSource) TableName() string { return "plugin_appstore_sources" }

// ── App Definition ──

// AppDefinition represents a single app parsed from a source repository.
type AppDefinition struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	SourceID    uint      `gorm:"index;not null" json:"source_id"`
	AppID       string    `gorm:"size:128;not null;index" json:"app_id"` // e.g. "nextcloud"
	Name        string    `gorm:"size:255;not null" json:"name"`
	ShortDesc   string    `gorm:"size:512" json:"short_desc"`
	Description string    `gorm:"type:text" json:"description,omitempty"` // markdown from description.md
	Version     string    `gorm:"size:64" json:"version"`
	Author      string    `gorm:"size:128" json:"author"`
	Categories  string    `gorm:"size:512" json:"categories"`  // JSON array: ["media","cloud"]
	Port        int       `json:"port"`                        // default exposed port
	Exposable   bool      `gorm:"default:true" json:"exposable"`
	ComposeFile string    `gorm:"type:text" json:"-"`          // raw docker-compose.yml (hidden from list)
	ConfigJSON  string    `gorm:"type:text" json:"-"`          // raw config.json (hidden from list)
	FormFields  string    `gorm:"type:text" json:"form_fields"` // JSON array of FormField
	LogoPath    string    `gorm:"size:512" json:"logo_path"`   // relative path in source dir
	Website     string    `gorm:"size:512" json:"website"`
	Source      string    `gorm:"size:512" json:"source_url"`  // upstream source code URL
	Available   bool      `gorm:"default:true" json:"available"`
	UrlSuffix   string    `gorm:"size:128" json:"url_suffix"`  // e.g. "/admin" for Pi-hole
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (AppDefinition) TableName() string { return "plugin_appstore_apps" }

// ── Installed App ──

// InstalledApp represents an installed instance of an app.
type InstalledApp struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	AppID      string    `gorm:"size:128;index" json:"app_id"`
	AppName    string    `gorm:"size:255" json:"app_name"`    // display name from AppDefinition
	Name       string    `gorm:"size:128;not null" json:"name"` // user-chosen instance name
	StackName  string    `gorm:"size:128" json:"stack_name"`  // Docker compose project name
	HostID     uint      `gorm:"default:0" json:"host_id"`    // reverse proxy host (0 = none)
	Domain     string    `gorm:"size:255" json:"domain"`
	Port       int       `json:"port"`
	FormValues string    `gorm:"type:text" json:"-"`           // JSON map of user-entered values
	Version    string    `gorm:"size:64" json:"version"`
	Status     string    `gorm:"size:16;default:installing" json:"status"` // installing, running, stopped, error
	ComposeDir string    `gorm:"size:512" json:"-"`            // path to rendered compose files
	AutoUpdate bool      `gorm:"default:false" json:"auto_update"`
	UrlSuffix  string    `gorm:"size:128" json:"url_suffix"`   // e.g. "/admin"
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (InstalledApp) TableName() string { return "plugin_appstore_installed" }

// ── Project Template ──

// ProjectTemplate represents a project starter template (e.g. Next.js boilerplate).
type ProjectTemplate struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	SourceID    uint      `gorm:"index" json:"source_id"`
	TemplateID  string    `gorm:"size:128;not null;index" json:"template_id"` // e.g. "nextjs-starter"
	Name        string    `gorm:"size:255;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	Framework   string    `gorm:"size:64" json:"framework"` // nextjs, nuxt, go, laravel, etc.
	GitURL      string    `gorm:"size:512;not null" json:"git_url"`
	Branch      string    `gorm:"size:64;default:main" json:"branch"`
	Tags        string    `gorm:"size:512" json:"tags"` // JSON array
	LogoURL     string    `gorm:"size:512" json:"logo_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ProjectTemplate) TableName() string { return "plugin_appstore_templates" }
