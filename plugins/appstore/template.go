package appstore

import (
	"fmt"
	"log/slog"
	"regexp"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

var domainRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

func validateTemplateDomain(domain string) error {
	if len(domain) > 253 {
		return fmt.Errorf("domain too long (max 253 chars)")
	}
	if !domainRe.MatchString(domain) {
		return fmt.Errorf("invalid domain format: %s", domain)
	}
	return nil
}

// TemplateService handles project template browsing and deployment.
type TemplateService struct {
	db      *gorm.DB
	sources *SourceManager
	logger  *slog.Logger
	dataDir string
	coreAPI pluginpkg.CoreAPI
}

// NewTemplateService creates a TemplateService.
func NewTemplateService(db *gorm.DB, sources *SourceManager, logger *slog.Logger, dataDir string, coreAPI pluginpkg.CoreAPI) *TemplateService {
	return &TemplateService{db: db, sources: sources, logger: logger, dataDir: dataDir, coreAPI: coreAPI}
}

// ListTemplates returns project templates with optional filtering (max 200).
func (ts *TemplateService) ListTemplates(framework, search string) ([]ProjectTemplate, error) {
	query := ts.db.Model(&ProjectTemplate{})

	if framework != "" {
		query = query.Where("framework = ?", framework)
	}

	if search != "" {
		like := "%" + search + "%"
		query = query.Where("LOWER(name) LIKE LOWER(?) OR LOWER(description) LIKE LOWER(?)", like, like)
	}

	var templates []ProjectTemplate
	if err := query.Order("name ASC").Limit(200).Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

// GetTemplate returns a single template by ID.
func (ts *TemplateService) GetTemplate(id uint) (*ProjectTemplate, error) {
	var tpl ProjectTemplate
	if err := ts.db.First(&tpl, id).Error; err != nil {
		return nil, err
	}
	return &tpl, nil
}

// GetFrameworks returns all unique framework values.
func (ts *TemplateService) GetFrameworks() []string {
	var results []string
	ts.db.Model(&ProjectTemplate{}).Distinct("framework").Where("framework != ''").Pluck("framework", &results)
	return results
}

// CreateFromTemplateRequest is the input for deploying from a template.
type CreateFromTemplateRequest struct {
	TemplateID uint   `json:"template_id" binding:"required"`
	Name       string `json:"name" binding:"required"`
	Domain     string `json:"domain,omitempty"`
}

// DeployFromTemplate creates a project from a template by inserting a record
// into plugin_deploy_projects. Returns the project ID for frontend redirect.
func (ts *TemplateService) DeployFromTemplate(req *CreateFromTemplateRequest) (uint, error) {
	// Check if deploy plugin is installed and enabled.
	if !ts.db.Migrator().HasTable("plugin_deploy_projects") {
		return 0, fmt.Errorf("deploy plugin is not installed — please enable it first")
	}
	// Verify the deploy plugin is currently enabled; the table may exist from a
	// prior activation but the plugin's API routes are gated by PluginGuardMiddleware,
	// so creating a project here would leave it unmanageable.
	var enabledVal *bool
	ts.db.Table("plugin_states").Where("id = ?", "deploy").Select("enabled").Row().Scan(&enabledVal)
	if enabledVal == nil || !*enabledVal {
		return 0, fmt.Errorf("deploy plugin is disabled — please enable it before deploying templates")
	}

	tpl, err := ts.GetTemplate(req.TemplateID)
	if err != nil {
		return 0, fmt.Errorf("template not found: %w", err)
	}

	branch := tpl.Branch
	if branch == "" {
		branch = "main"
	}

	// Validate domain if provided.
	if req.Domain != "" {
		if err := validateTemplateDomain(req.Domain); err != nil {
			return 0, err
		}
		// Check domain uniqueness against existing hosts so we don't create a
		// project that will fail silently when setting up the reverse proxy.
		var domainCount int64
		ts.db.Table("hosts").Where("domain = ?", req.Domain).Count(&domainCount)
		if domainCount > 0 {
			return 0, fmt.Errorf("domain %q is already in use by an existing host", req.Domain)
		}
	}

	// Look up framework preset commands so the project is deployable out of the box.
	installCmd, buildCmd, startCmd := frameworkPresetCommands(tpl.Framework)

	// Use CoreAPI.CreateProject so the deploy plugin's full initialization
	// runs (webhook token, auth method, deploy mode, port allocation).
	projectID, err := ts.coreAPI.CreateProject(pluginpkg.CreateProjectRequest{
		Name:           req.Name,
		GitURL:         tpl.GitURL,
		GitBranch:      branch,
		Domain:         req.Domain,
		Framework:      tpl.Framework,
		DeployMode:     "", // let CoreAPI infer from framework
		InstallCommand: installCmd,
		BuildCommand:   buildCmd,
		StartCommand:   startCmd,
	})
	if err != nil {
		return 0, fmt.Errorf("create project: %w", err)
	}

	ts.logger.Info("created project from template", "template", tpl.Name, "project_id", projectID)
	return projectID, nil
}

// frameworkPresetCommands returns install/build/start commands for known frameworks.
func frameworkPresetCommands(framework string) (install, build, start string) {
	presets := map[string][3]string{
		"nextjs":  {"npm install", "npm run build", "npm start"},
		"nuxt":    {"npm install", "npm run build", "node .output/server/index.mjs"},
		"vite":    {"npm install", "npm run build", ""},
		"remix":   {"npm install", "npm run build", "npm start"},
		"express": {"npm install", "", "node index.js"},
		"go":      {"", "go build -o app .", "./app"},
		"laravel": {"composer install --no-dev", "php artisan optimize", "php-fpm"},
		"flask":   {"pip install -r requirements.txt", "", "gunicorn app:app"},
		"django":  {"pip install -r requirements.txt", "python manage.py collectstatic --noinput", "gunicorn config.wsgi:application"},
	}
	if p, ok := presets[framework]; ok {
		return p[0], p[1], p[2]
	}
	return "", "", ""
}
