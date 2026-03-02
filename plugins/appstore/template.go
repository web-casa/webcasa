package appstore

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"
)

// TemplateService handles project template browsing and deployment.
type TemplateService struct {
	db      *gorm.DB
	sources *SourceManager
	logger  *slog.Logger
	dataDir string
}

// NewTemplateService creates a TemplateService.
func NewTemplateService(db *gorm.DB, sources *SourceManager, logger *slog.Logger, dataDir string) *TemplateService {
	return &TemplateService{db: db, sources: sources, logger: logger, dataDir: dataDir}
}

// ListTemplates returns all project templates with optional filtering.
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
	if err := query.Order("name ASC").Find(&templates).Error; err != nil {
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
	tpl, err := ts.GetTemplate(req.TemplateID)
	if err != nil {
		return 0, fmt.Errorf("template not found: %w", err)
	}

	// Framework presets (matching deploy plugin's presets)
	type preset struct {
		InstallCmd string
		BuildCmd   string
		StartCmd   string
		Port       int
	}
	presets := map[string]preset{
		"nextjs":  {InstallCmd: "npm install", BuildCmd: "npm run build", StartCmd: "npm start", Port: 3000},
		"nuxt":    {InstallCmd: "npm install", BuildCmd: "npm run build", StartCmd: "node .output/server/index.mjs", Port: 3000},
		"vite":    {InstallCmd: "npm install", BuildCmd: "npm run build", Port: 0},
		"remix":   {InstallCmd: "npm install", BuildCmd: "npm run build", StartCmd: "npm start", Port: 3000},
		"express": {InstallCmd: "npm install", StartCmd: "node index.js", Port: 3000},
		"go":      {BuildCmd: "go build -o app .", StartCmd: "./app", Port: 8080},
		"laravel": {InstallCmd: "composer install --no-dev", BuildCmd: "php artisan optimize", StartCmd: "php-fpm", Port: 9000},
		"flask":   {InstallCmd: "pip install -r requirements.txt", StartCmd: "gunicorn app:app", Port: 8000},
		"django":  {InstallCmd: "pip install -r requirements.txt", BuildCmd: "python manage.py collectstatic --noinput", StartCmd: "gunicorn config.wsgi:application", Port: 8000},
	}

	p, ok := presets[tpl.Framework]
	if !ok {
		p = preset{Port: 3000}
	}

	branch := tpl.Branch
	if branch == "" {
		branch = "main"
	}

	// Insert directly into plugin_deploy_projects table
	record := map[string]interface{}{
		"name":            req.Name,
		"domain":          req.Domain,
		"git_url":         tpl.GitURL,
		"git_branch":      branch,
		"framework":       tpl.Framework,
		"install_command": p.InstallCmd,
		"build_command":   p.BuildCmd,
		"start_command":   p.StartCmd,
		"port":            p.Port,
		"status":          "pending",
		"auto_deploy":     false,
	}

	result := ts.db.Table("plugin_deploy_projects").Create(record)
	if result.Error != nil {
		return 0, fmt.Errorf("create project: %w", result.Error)
	}

	// Get the created project ID
	var projectID uint
	ts.db.Table("plugin_deploy_projects").Select("id").Where("name = ? AND git_url = ?", req.Name, tpl.GitURL).Order("id DESC").Limit(1).Scan(&projectID)

	ts.logger.Info("created project from template", "template", tpl.Name, "project_id", projectID)
	return projectID, nil
}
