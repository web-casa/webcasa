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
	DeployKey     string    `gorm:"type:text" json:"-"` // SSH deploy key (private), never exposed
	Framework     string    `gorm:"size:64" json:"framework"`   // nextjs, nuxt, vite, go, laravel, custom
	BuildCommand  string    `gorm:"size:512" json:"build_command"`
	StartCommand  string    `gorm:"size:512" json:"start_command"`
	InstallCmd    string    `gorm:"size:512" json:"install_command"`
	Port          int       `gorm:"default:0" json:"port"`    // app listen port (auto-assigned if 0)
	Status        string    `gorm:"size:32;default:pending" json:"status"` // pending, building, running, stopped, error
	CurrentBuild  int       `gorm:"default:0" json:"current_build"`
	AutoDeploy    bool      `gorm:"default:false" json:"auto_deploy"`
	WebhookToken  string    `gorm:"size:64;uniqueIndex" json:"webhook_token"`
	HostID        uint      `gorm:"default:0" json:"host_id"` // associated reverse proxy host
	EnvVars       string    `gorm:"type:text" json:"-"`        // JSON-encoded env vars (encrypted)
	ErrorMsg      string    `gorm:"type:text" json:"error_msg"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Transient fields (not stored)
	EnvVarList []EnvVar `gorm:"-" json:"env_vars,omitempty"`
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
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProjectID uint      `gorm:"index;not null" json:"project_id"`
	BuildNum  int       `gorm:"not null" json:"build_num"`
	GitCommit string    `gorm:"size:64" json:"git_commit"`
	Status    string    `gorm:"size:32;default:building" json:"status"` // building, success, failed, rolled_back
	LogFile   string    `gorm:"size:512" json:"log_file"`
	Duration  int       `json:"duration"` // seconds
	CreatedAt time.Time `json:"created_at"`
}

func (Deployment) TableName() string {
	return "plugin_deploy_deployments"
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
	"custom": {
		Name: "Custom", Framework: "custom",
	},
}
