package deploy

import (
	"time"
)

// Project represents a deployable project (Node.js, Go, PHP, etc.).
type Project struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Name          string    `gorm:"size:255;not null" json:"name"`
	Domain        string    `gorm:"size:255" json:"domain"`
	GitURL        string    `gorm:"size:512" json:"git_url"`
	GitBranch     string    `gorm:"size:128;default:main" json:"git_branch"`
	DeployKey     string    `gorm:"type:text" json:"-"` // SSH deploy key (private), encrypted, never exposed
	Framework     string    `gorm:"size:64" json:"framework"`   // nextjs, nuxt, vite, go, laravel, custom
	BuildCommand  string    `gorm:"size:512" json:"build_command"`
	StartCommand  string    `gorm:"size:512" json:"start_command"`
	InstallCmd    string    `gorm:"size:512" json:"install_command"`
	Port          int       `gorm:"default:0" json:"port"`    // app listen port (auto-assigned if 0)
	Status        string    `gorm:"size:32;default:pending" json:"status"` // pending, building, running, stopped, error
	CurrentBuild  int       `gorm:"default:0" json:"current_build"`
	AutoDeploy    bool      `gorm:"default:false" json:"auto_deploy"`
	WebhookToken  string    `gorm:"size:64;uniqueIndex" json:"-"` // never exposed via API
	HostID        uint      `gorm:"default:0" json:"host_id"` // associated reverse proxy host
	EnvVars       string    `gorm:"type:text" json:"-"`        // JSON-encoded env vars (encrypted)
	ErrorMsg      string    `gorm:"type:text" json:"error_msg"`

	// Build type: auto-detect or explicit builder selection
	BuildType     string `gorm:"size:32;default:''" json:"build_type"` // dockerfile, nixpacks, paketo, railpack, static, auto, "" (legacy)

	// Deploy mode: bare (systemd) or docker (container)
	DeployMode    string `gorm:"size:16;default:bare" json:"deploy_mode"` // bare | docker
	DockerImage   string `gorm:"size:255" json:"docker_image,omitempty"` // e.g. webcasa-project-5:3
	ContainerID   string `gorm:"size:128" json:"container_id,omitempty"`
	ContainerName string `gorm:"size:128" json:"container_name,omitempty"`

	// Health check settings
	HealthCheckPath        string `gorm:"size:255;default:/" json:"health_check_path"`
	HealthCheckTimeout     int    `gorm:"default:30" json:"health_check_timeout"`      // seconds
	HealthCheckRetries     int    `gorm:"default:3" json:"health_check_retries"`
	HealthCheckMethod      string `gorm:"size:8;default:GET" json:"health_check_method"`       // GET, HEAD, POST
	HealthCheckExpectCode  int    `gorm:"default:0" json:"health_check_expect_code"`            // 0 = any 2xx
	HealthCheckExpectBody  string `gorm:"size:512" json:"health_check_expect_body"`             // response must contain this text
	HealthCheckStartPeriod int    `gorm:"default:0" json:"health_check_start_period"`           // seconds to wait before first check

	// Resource limits
	MemoryLimit  int `gorm:"default:0" json:"memory_limit"`  // MB, 0 = unlimited
	CPULimit     int `gorm:"default:0" json:"cpu_limit"`     // percentage (100 = 1 core), 0 = unlimited
	BuildTimeout int `gorm:"default:30" json:"build_timeout"` // minutes

	// GitHub App authentication fields
	AuthMethod           string `gorm:"size:32;default:ssh_key" json:"auth_method"` // ssh_key | github_app | github_oauth
	GitHubAppID          int64  `gorm:"default:0" json:"github_app_id"`
	GitHubPrivateKey     string `gorm:"type:text" json:"-"`     // encrypted PEM, never exposed
	GitHubInstallationID int64  `gorm:"default:0" json:"github_installation_id"`

	// GitHub OAuth fields (used when auth_method = "github_oauth")
	GitHubOAuthInstallID uint   `gorm:"default:0" json:"github_oauth_install_id"` // FK to GitHubInstallation
	GitHubRepoFullName   string `gorm:"size:255" json:"github_repo_full_name"`    // e.g. "owner/repo"

	// Webhook HMAC secret for signature verification
	WebhookSecret string `gorm:"size:128" json:"-"` // HMAC secret, never exposed

	// Preview deployment settings
	PreviewEnabled bool   `gorm:"default:false" json:"preview_enabled"`
	PreviewExpiry  int    `gorm:"default:7" json:"preview_expiry"` // days
	GitHubToken    string `gorm:"size:512" json:"-"`               // encrypted, for PR comments

	// Git polling — periodic `git ls-remote` to detect new commits without
	// requiring a webhook. Complements (does not replace) AutoDeploy/webhooks;
	// SingleFlight in Build() ensures concurrent webhook + poll triggers
	// collapse into a single build. Disabled by default.
	GitPollEnabled     bool       `gorm:"default:false" json:"git_poll_enabled"`
	GitPollIntervalSec int        `gorm:"default:300" json:"git_poll_interval_sec"` // minimum enforced at 60
	LastDeployedCommit string     `gorm:"size:64" json:"last_deployed_commit"`
	LastPolledAt       *time.Time `json:"last_polled_at,omitempty"`

	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Transient fields (not stored)
	EnvVarList   []EnvVar `gorm:"-" json:"env_vars,omitempty"`
	HasDeployKey bool     `gorm:"-" json:"has_deploy_key"`          // indicates if deploy key is set
	HasGitHubKey bool     `gorm:"-" json:"has_github_private_key"`  // indicates if GitHub App key is set
	WebhookURL   string   `gorm:"-" json:"webhook_url,omitempty"`   // populated only for admin detail view
}

func (Project) TableName() string {
	return "plugin_deploy_projects"
}

// EnvVar is a key-value pair for project environment variables.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Deployment records one build/deploy attempt.
type Deployment struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	ProjectID       uint      `gorm:"index;not null" json:"project_id"`
	BuildNum        int       `gorm:"not null" json:"build_num"`
	GitCommit       string    `gorm:"size:64" json:"git_commit"`
	Status          string    `gorm:"size:32;default:building" json:"status"` // building, success, failed, rolled_back
	LogFile         string    `gorm:"size:512" json:"log_file"`
	Duration        int       `json:"duration"` // seconds
	DiagnosisResult string    `gorm:"type:text" json:"diagnosis_result,omitempty"` // AI diagnosis of build failure
	ImageTag        string    `gorm:"size:128" json:"image_tag,omitempty"`          // Docker image tag for rollback
	CreatedAt       time.Time `json:"created_at"`
}

func (Deployment) TableName() string {
	return "plugin_deploy_deployments"
}

// PreviewDeployment tracks an ephemeral deployment created from a GitHub PR.
//
// Uniqueness: (project_id, pr_number) must be unique. Enforced via a composite
// unique index so the `GitHub sends two synchronize webhooks in ms` race can't
// create duplicate rows for the same PR (Codex Round: C2).
//
// Port: persisted per-preview so a successful rebuild (PR synchronize) reuses
// the same host port, keeping the Caddy upstream stable and avoiding port
// overflow from a naive `20000 + previewID` formula (Codex Round: H5).
type PreviewDeployment struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	ProjectID     uint   `gorm:"index;not null;uniqueIndex:ux_preview_project_pr" json:"project_id"`
	PRNumber      int    `gorm:"not null;uniqueIndex:ux_preview_project_pr" json:"pr_number"`
	Branch        string `gorm:"size:128" json:"branch"`
	Domain        string `gorm:"size:255" json:"domain"`
	ContainerName string `gorm:"size:128" json:"container_name"` // reflects the currently-active slot container name
	ImageTag      string `gorm:"size:128" json:"image_tag"`      // webcasa-preview-<id>
	// Port: the currently-serving host port. Equals BasePort when Slot==0,
	// BasePort+5000 when Slot==1. Updated atomically with ContainerName +
	// Slot on each successful swap.
	Port int `gorm:"default:0" json:"port"`
	// BasePort: the port slot-0 container binds to. Allocated once at
	// create-time in [20000, 25000); the "alt" slot-1 container binds to
	// BasePort+5000 (lands in [25000, 30000)). Never changes after
	// allocation. Two slots let us alternate each deploy without ever
	// rebinding the same port while both containers briefly coexist
	// during the Caddy upstream swap (Codex R6-C1 fix).
	BasePort int `gorm:"default:0;uniqueIndex:ux_preview_base_port" json:"base_port"`
	// Slot: which slot is currently serving — 0 or 1, or -1 before the
	// first successful deploy.
	Slot          int       `gorm:"default:-1" json:"slot"`
	HostID        uint      `gorm:"default:0" json:"host_id"`
	Status        string    `gorm:"size:16;default:pending" json:"status"` // pending | building | running | failed | cleanup_failed
	FailureReason string    `gorm:"size:512" json:"failure_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (PreviewDeployment) TableName() string {
	return "plugin_deploy_preview_deployments"
}

// FrameworkPreset holds auto-detected build configuration for known frameworks.
type FrameworkPreset struct {
	Name       string `json:"name"`
	Framework  string `json:"framework"`
	InstallCmd string `json:"install_command"`
	BuildCmd   string `json:"build_command"`
	StartCmd   string `json:"start_command"`
	Port       int    `json:"port"`
}

// Known framework presets.
var frameworkPresets = map[string]FrameworkPreset{
	"nextjs": {
		Name: "Next.js", Framework: "nextjs",
		InstallCmd: "npm install", BuildCmd: "npm run build", StartCmd: "npm start", Port: 3000,
	},
	"nuxt": {
		Name: "Nuxt", Framework: "nuxt",
		InstallCmd: "npm install", BuildCmd: "npm run build", StartCmd: "node .output/server/index.mjs", Port: 3000,
	},
	"vite": {
		Name: "Vite (SPA)", Framework: "vite",
		InstallCmd: "npm install", BuildCmd: "npm run build", StartCmd: "", Port: 0, // static, no start cmd
	},
	"remix": {
		Name: "Remix", Framework: "remix",
		InstallCmd: "npm install", BuildCmd: "npm run build", StartCmd: "npm start", Port: 3000,
	},
	"express": {
		Name: "Express.js", Framework: "express",
		InstallCmd: "npm install", BuildCmd: "", StartCmd: "node index.js", Port: 3000,
	},
	"go": {
		Name: "Go", Framework: "go",
		InstallCmd: "", BuildCmd: "go build -o app .", StartCmd: "./app", Port: 8080,
	},
	"laravel": {
		Name: "Laravel", Framework: "laravel",
		InstallCmd: "composer install --no-dev", BuildCmd: "php artisan optimize", StartCmd: "php-fpm", Port: 9000,
	},
	"flask": {
		Name: "Flask", Framework: "flask",
		InstallCmd: "pip install -r requirements.txt", BuildCmd: "", StartCmd: "gunicorn app:app", Port: 8000,
	},
	"django": {
		Name: "Django", Framework: "django",
		InstallCmd: "pip install -r requirements.txt", BuildCmd: "python manage.py collectstatic --noinput", StartCmd: "gunicorn config.wsgi:application", Port: 8000,
	},
	"dockerfile": {
		Name: "Dockerfile", Framework: "dockerfile",
		InstallCmd: "", BuildCmd: "", StartCmd: "", Port: 0, // handled by Docker
	},
	"custom": {
		Name: "Custom", Framework: "custom",
	},
}

// CronJob represents a scheduled task for a project.
type CronJob struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	ProjectID  uint       `gorm:"index;not null" json:"project_id"`
	Name       string     `gorm:"size:255;not null" json:"name"`
	Schedule   string     `gorm:"size:128;not null" json:"schedule"` // cron expression
	Command    string     `gorm:"size:1024;not null" json:"command"`
	Enabled    bool       `gorm:"default:true" json:"enabled"`
	LastRunAt  *time.Time `json:"last_run_at"`
	LastStatus string     `gorm:"size:32" json:"last_status"` // success, failed, ""
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (CronJob) TableName() string {
	return "plugin_deploy_cron_jobs"
}

// ExtraProcess represents an additional process (worker, queue consumer, etc.) for a project.
type ExtraProcess struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProjectID uint      `gorm:"index;not null" json:"project_id"`
	Name      string    `gorm:"size:255;not null" json:"name"`
	Command   string    `gorm:"size:1024;not null" json:"command"`
	Instances int       `gorm:"default:1" json:"instances"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ExtraProcess) TableName() string {
	return "plugin_deploy_extra_processes"
}

// GitHubInstallation stores an authorized GitHub App installation.
type GitHubInstallation struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	InstallationID   int64     `gorm:"uniqueIndex;not null" json:"installation_id"`
	AccountLogin     string    `gorm:"size:255" json:"account_login"`       // GitHub org/user name
	AccountType      string    `gorm:"size:32" json:"account_type"`         // "User" or "Organization"
	AccountAvatarURL string    `gorm:"size:512" json:"account_avatar_url"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (GitHubInstallation) TableName() string {
	return "plugin_deploy_github_installations"
}

// EnvVarSuggestion represents a suggested environment variable for a framework.
type EnvVarSuggestion struct {
	Key          string `json:"key"`
	DefaultValue string `json:"default_value"`
	Description  string `json:"description"`
	Required     bool   `json:"required"`
}

// frameworkEnvSuggestions maps framework IDs to suggested environment variables.
var frameworkEnvSuggestions = map[string][]EnvVarSuggestion{
	"nextjs": {
		{Key: "NODE_ENV", DefaultValue: "production", Description: "Node.js environment mode", Required: true},
		{Key: "NEXT_TELEMETRY_DISABLED", DefaultValue: "1", Description: "Disable Next.js telemetry"},
		{Key: "NEXT_PUBLIC_API_URL", DefaultValue: "", Description: "Public API base URL for client-side requests"},
	},
	"nuxt": {
		{Key: "NODE_ENV", DefaultValue: "production", Description: "Node.js environment mode", Required: true},
		{Key: "NITRO_PRESET", DefaultValue: "node-server", Description: "Nitro server preset"},
		{Key: "NUXT_PUBLIC_API_BASE", DefaultValue: "", Description: "Public API base URL"},
	},
	"vite": {
		{Key: "NODE_ENV", DefaultValue: "production", Description: "Node.js environment mode", Required: true},
		{Key: "VITE_API_URL", DefaultValue: "", Description: "API base URL (exposed to client)"},
	},
	"remix": {
		{Key: "NODE_ENV", DefaultValue: "production", Description: "Node.js environment mode", Required: true},
		{Key: "SESSION_SECRET", DefaultValue: "", Description: "Session encryption secret", Required: true},
	},
	"express": {
		{Key: "NODE_ENV", DefaultValue: "production", Description: "Node.js environment mode", Required: true},
		{Key: "LOG_LEVEL", DefaultValue: "info", Description: "Application log level"},
	},
	"go": {
		{Key: "GIN_MODE", DefaultValue: "release", Description: "Gin framework mode (debug/release)"},
		{Key: "GO_ENV", DefaultValue: "production", Description: "Go environment mode"},
	},
	"laravel": {
		{Key: "APP_ENV", DefaultValue: "production", Description: "Application environment", Required: true},
		{Key: "APP_KEY", DefaultValue: "", Description: "Application encryption key (run: php artisan key:generate)", Required: true},
		{Key: "APP_DEBUG", DefaultValue: "false", Description: "Debug mode (disable in production)", Required: true},
		{Key: "DB_CONNECTION", DefaultValue: "mysql", Description: "Database driver"},
		{Key: "DB_HOST", DefaultValue: "127.0.0.1", Description: "Database host"},
		{Key: "DB_PORT", DefaultValue: "3306", Description: "Database port"},
		{Key: "DB_DATABASE", DefaultValue: "", Description: "Database name", Required: true},
		{Key: "DB_USERNAME", DefaultValue: "", Description: "Database username", Required: true},
		{Key: "DB_PASSWORD", DefaultValue: "", Description: "Database password", Required: true},
	},
	"flask": {
		{Key: "FLASK_ENV", DefaultValue: "production", Description: "Flask environment mode", Required: true},
		{Key: "FLASK_APP", DefaultValue: "app", Description: "Flask application module"},
		{Key: "SECRET_KEY", DefaultValue: "", Description: "Flask session secret key", Required: true},
	},
	"django": {
		{Key: "DJANGO_SETTINGS_MODULE", DefaultValue: "config.settings", Description: "Django settings module path", Required: true},
		{Key: "DEBUG", DefaultValue: "False", Description: "Debug mode (disable in production)", Required: true},
		{Key: "SECRET_KEY", DefaultValue: "", Description: "Django secret key", Required: true},
		{Key: "ALLOWED_HOSTS", DefaultValue: "*", Description: "Allowed host headers"},
		{Key: "DATABASE_URL", DefaultValue: "", Description: "Database connection URL"},
	},
}

// GetEnvSuggestions returns environment variable suggestions for a framework.
func GetEnvSuggestions(framework string) []EnvVarSuggestion {
	if suggestions, ok := frameworkEnvSuggestions[framework]; ok {
		return suggestions
	}
	return nil
}
