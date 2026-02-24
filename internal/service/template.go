package service

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/web-casa/webcasa/internal/model"
	"gorm.io/gorm"
)

// TemplateConfig represents the JSON snapshot of a host configuration stored in a template.
type TemplateConfig struct {
	HostType         string              `json:"host_type"`
	TLSMode          string              `json:"tls_mode"`
	TLSEnabled       *bool               `json:"tls_enabled"`
	HTTPRedirect     *bool               `json:"http_redirect"`
	WebSocket        *bool               `json:"websocket"`
	Compression      *bool               `json:"compression"`
	CorsEnabled      *bool               `json:"cors_enabled"`
	CorsOrigins      string              `json:"cors_origins"`
	CorsMethods      string              `json:"cors_methods"`
	CorsHeaders      string              `json:"cors_headers"`
	SecurityHeaders  *bool               `json:"security_headers"`
	ErrorPagePath    string              `json:"error_page_path"`
	CacheEnabled     *bool               `json:"cache_enabled"`
	CacheTTL         int                 `json:"cache_ttl"`
	RootPath         string              `json:"root_path"`
	DirectoryBrowse  *bool               `json:"directory_browse"`
	PHPFastCGI       string              `json:"php_fastcgi"`
	IndexFiles       string              `json:"index_files"`
	CustomDirectives string              `json:"custom_directives"`
	RedirectURL      string              `json:"redirect_url"`
	RedirectCode     int                 `json:"redirect_code"`
	Upstreams        []model.UpstreamInput  `json:"upstreams"`
	CustomHeaders    []model.HeaderInput    `json:"custom_headers"`
	AccessRules      []model.AccessInput    `json:"access_rules"`
	BasicAuths       []TemplateBasicAuth    `json:"basic_auths"`
}

// TemplateBasicAuth stores basic auth with the password hash directly (snapshot).
type TemplateBasicAuth struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

// TemplateExport is the JSON format for exporting a template.
type TemplateExport struct {
	Version    string               `json:"version"`
	ExportedAt string               `json:"exported_at"`
	Template   TemplateExportData   `json:"template"`
}

// TemplateExportData is the template portion of the export JSON.
type TemplateExportData struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Config      json.RawMessage `json:"config"`
}

// TemplateService handles business logic for host configuration templates.
type TemplateService struct {
	db      *gorm.DB
	hostSvc *HostService
}

// NewTemplateService creates a new TemplateService.
func NewTemplateService(db *gorm.DB, hostSvc *HostService) *TemplateService {
	return &TemplateService{db: db, hostSvc: hostSvc}
}

// List returns all templates.
func (s *TemplateService) List() ([]model.Template, error) {
	var templates []model.Template
	err := s.db.Order("id ASC").Find(&templates).Error
	return templates, err
}

// Get returns a single template by ID.
func (s *TemplateService) Get(id uint) (*model.Template, error) {
	var tpl model.Template
	if err := s.db.First(&tpl, id).Error; err != nil {
		return nil, err
	}
	return &tpl, nil
}

// Create creates a new custom template.
func (s *TemplateService) Create(name, description, configJSON string) (*model.Template, error) {
	// Validate config JSON
	var cfg TemplateConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("error.invalid_template_json")
	}
	if cfg.HostType == "" {
		return nil, fmt.Errorf("error.template_missing_fields")
	}

	tpl := &model.Template{
		Name:        name,
		Description: description,
		Type:        "custom",
		Config:      configJSON,
	}
	if err := s.db.Create(tpl).Error; err != nil {
		return nil, fmt.Errorf("failed to create template: %w", err)
	}
	return tpl, nil
}

// Update modifies an existing custom template. Preset templates cannot be modified.
func (s *TemplateService) Update(id uint, name, description, configJSON string) (*model.Template, error) {
	tpl, err := s.Get(id)
	if err != nil {
		return nil, fmt.Errorf("error.template_not_found")
	}
	if tpl.Type == "preset" {
		return nil, fmt.Errorf("error.preset_immutable")
	}

	// Validate config JSON if provided
	if configJSON != "" {
		var cfg TemplateConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("error.invalid_template_json")
		}
		if cfg.HostType == "" {
			return nil, fmt.Errorf("error.template_missing_fields")
		}
		tpl.Config = configJSON
	}

	tpl.Name = name
	tpl.Description = description
	if err := s.db.Save(tpl).Error; err != nil {
		return nil, fmt.Errorf("failed to update template: %w", err)
	}
	return tpl, nil
}

// Delete removes a custom template. Preset templates cannot be deleted.
func (s *TemplateService) Delete(id uint) error {
	tpl, err := s.Get(id)
	if err != nil {
		return fmt.Errorf("error.template_not_found")
	}
	if tpl.Type == "preset" {
		return fmt.Errorf("error.preset_immutable")
	}
	if err := s.db.Delete(&model.Template{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete template: %w", err)
	}
	return nil
}

// SaveAsTemplate creates a template from an existing host's configuration snapshot.
func (s *TemplateService) SaveAsTemplate(hostID uint, name, description string) (*model.Template, error) {
	host, err := s.hostSvc.Get(hostID)
	if err != nil {
		return nil, fmt.Errorf("error.host_not_found")
	}

	cfg := s.hostToTemplateConfig(host)
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize host config: %w", err)
	}

	tpl := &model.Template{
		Name:        name,
		Description: description,
		Type:        "custom",
		Config:      string(configJSON),
	}
	if err := s.db.Create(tpl).Error; err != nil {
		return nil, fmt.Errorf("failed to create template: %w", err)
	}
	return tpl, nil
}

// CreateFromTemplate creates a new host from a template configuration.
func (s *TemplateService) CreateFromTemplate(templateID uint, domain string) (*model.Host, error) {
	tpl, err := s.Get(templateID)
	if err != nil {
		return nil, fmt.Errorf("error.template_not_found")
	}

	var cfg TemplateConfig
	if err := json.Unmarshal([]byte(tpl.Config), &cfg); err != nil {
		return nil, fmt.Errorf("error.invalid_template_json")
	}

	// Check domain uniqueness
	var count int64
	s.db.Model(&model.Host{}).Where("domain = ?", domain).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("error.domain_exists")
	}

	host := &model.Host{
		Domain:           domain,
		HostType:         stringOrDefault(cfg.HostType, "proxy"),
		Enabled:          boolPtr(true),
		TLSEnabled:       copyBoolPtrOrDefault(cfg.TLSEnabled, true),
		HTTPRedirect:     copyBoolPtrOrDefault(cfg.HTTPRedirect, true),
		WebSocket:        copyBoolPtrOrDefault(cfg.WebSocket, false),
		Compression:      copyBoolPtrOrDefault(cfg.Compression, false),
		CorsEnabled:      copyBoolPtrOrDefault(cfg.CorsEnabled, false),
		CorsOrigins:      cfg.CorsOrigins,
		CorsMethods:      cfg.CorsMethods,
		CorsHeaders:      cfg.CorsHeaders,
		SecurityHeaders:  copyBoolPtrOrDefault(cfg.SecurityHeaders, false),
		ErrorPagePath:    cfg.ErrorPagePath,
		CacheEnabled:     copyBoolPtrOrDefault(cfg.CacheEnabled, false),
		CacheTTL:         intOrDefault(cfg.CacheTTL, 300),
		RootPath:         cfg.RootPath,
		DirectoryBrowse:  copyBoolPtrOrDefault(cfg.DirectoryBrowse, false),
		PHPFastCGI:       cfg.PHPFastCGI,
		IndexFiles:       cfg.IndexFiles,
		CustomDirectives: cfg.CustomDirectives,
		RedirectURL:      cfg.RedirectURL,
		RedirectCode:     intOrDefault(cfg.RedirectCode, 301),
		TLSMode:          stringOrDefault(cfg.TLSMode, "auto"),
	}

	// Add upstreams
	for i, u := range cfg.Upstreams {
		weight := u.Weight
		if weight < 1 {
			weight = 1
		}
		host.Upstreams = append(host.Upstreams, model.Upstream{
			Address:   u.Address,
			Weight:    weight,
			SortOrder: i,
		})
	}

	// Add custom headers
	for i, h := range cfg.CustomHeaders {
		host.CustomHeaders = append(host.CustomHeaders, model.CustomHeader{
			Direction: stringOrDefault(h.Direction, "response"),
			Operation: stringOrDefault(h.Operation, "set"),
			Name:      h.Name,
			Value:     h.Value,
			SortOrder: i,
		})
	}

	// Add access rules
	for i, a := range cfg.AccessRules {
		host.AccessRules = append(host.AccessRules, model.AccessRule{
			RuleType:  a.RuleType,
			IPRange:   a.IPRange,
			SortOrder: i,
		})
	}

	// Add basic auths — store hash directly from template snapshot
	for _, ba := range cfg.BasicAuths {
		host.BasicAuths = append(host.BasicAuths, model.BasicAuth{
			Username:     ba.Username,
			PasswordHash: ba.PasswordHash,
		})
	}

	if err := s.db.Create(host).Error; err != nil {
		return nil, fmt.Errorf("failed to create host from template: %w", err)
	}

	if err := s.hostSvc.ApplyConfig(); err != nil {
		log.Printf("Warning: failed to apply config after creating host from template: %v", err)
	}

	return s.hostSvc.Get(host.ID)
}

// Export serializes a template to the export JSON format.
func (s *TemplateService) Export(templateID uint) ([]byte, error) {
	tpl, err := s.Get(templateID)
	if err != nil {
		return nil, fmt.Errorf("error.template_not_found")
	}

	export := TemplateExport{
		Version:    "1.0",
		ExportedAt: time.Now().Format(time.RFC3339),
		Template: TemplateExportData{
			Name:        tpl.Name,
			Description: tpl.Description,
			Type:        tpl.Type,
			Config:      json.RawMessage(tpl.Config),
		},
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize template: %w", err)
	}
	return data, nil
}

// Import parses and validates import JSON, then creates a new custom template.
func (s *TemplateService) Import(jsonData []byte) (*model.Template, error) {
	var export TemplateExport
	if err := json.Unmarshal(jsonData, &export); err != nil {
		return nil, fmt.Errorf("error.invalid_template_json")
	}

	// Validate required fields
	if export.Template.Name == "" {
		return nil, fmt.Errorf("error.template_missing_fields")
	}
	if len(export.Template.Config) == 0 {
		return nil, fmt.Errorf("error.template_missing_fields")
	}

	// Validate config JSON structure
	var cfg TemplateConfig
	if err := json.Unmarshal(export.Template.Config, &cfg); err != nil {
		return nil, fmt.Errorf("error.invalid_template_json")
	}
	if cfg.HostType == "" {
		return nil, fmt.Errorf("error.template_missing_fields")
	}

	// Always import as custom type
	tpl := &model.Template{
		Name:        export.Template.Name,
		Description: export.Template.Description,
		Type:        "custom",
		Config:      string(export.Template.Config),
	}
	if err := s.db.Create(tpl).Error; err != nil {
		return nil, fmt.Errorf("failed to import template: %w", err)
	}
	return tpl, nil
}

// SeedPresets creates the 6 built-in preset templates if the templates table is empty.
func (s *TemplateService) SeedPresets() {
	var count int64
	s.db.Model(&model.Template{}).Count(&count)
	if count > 0 {
		return
	}

	presets := []model.Template{
		{
			Name:        "WordPress Reverse Proxy",
			Description: "Reverse proxy for WordPress with compression enabled",
			Type:        "preset",
			Config:      mustJSON(TemplateConfig{
				HostType:    "proxy",
				TLSMode:     "auto",
				TLSEnabled:  boolPtr(true),
				HTTPRedirect: boolPtr(true),
				Compression: boolPtr(true),
				WebSocket:   boolPtr(false),
				CorsEnabled: boolPtr(false),
				SecurityHeaders: boolPtr(false),
				CacheEnabled: boolPtr(false),
				CacheTTL:    300,
				DirectoryBrowse: boolPtr(false),
				RedirectCode: 301,
				Upstreams: []model.UpstreamInput{
					{Address: "localhost:8080", Weight: 1},
				},
			}),
		},
		{
			Name:        "SPA Static Site",
			Description: "Static site for Single Page Applications with index.html fallback",
			Type:        "preset",
			Config:      mustJSON(TemplateConfig{
				HostType:    "static",
				TLSMode:     "auto",
				TLSEnabled:  boolPtr(true),
				HTTPRedirect: boolPtr(true),
				Compression: boolPtr(true),
				WebSocket:   boolPtr(false),
				CorsEnabled: boolPtr(false),
				SecurityHeaders: boolPtr(false),
				CacheEnabled: boolPtr(false),
				CacheTTL:    300,
				RootPath:    "/var/www/spa",
				IndexFiles:  "index.html",
				DirectoryBrowse: boolPtr(false),
				RedirectCode: 301,
			}),
		},
		{
			Name:        "API Reverse Proxy",
			Description: "Reverse proxy for API services with CORS and security headers",
			Type:        "preset",
			Config:      mustJSON(TemplateConfig{
				HostType:    "proxy",
				TLSMode:     "auto",
				TLSEnabled:  boolPtr(true),
				HTTPRedirect: boolPtr(true),
				Compression: boolPtr(false),
				WebSocket:   boolPtr(false),
				CorsEnabled: boolPtr(true),
				SecurityHeaders: boolPtr(true),
				CacheEnabled: boolPtr(false),
				CacheTTL:    300,
				DirectoryBrowse: boolPtr(false),
				RedirectCode: 301,
				Upstreams: []model.UpstreamInput{
					{Address: "localhost:3000", Weight: 1},
				},
			}),
		},
		{
			Name:        "PHP-FPM Site",
			Description: "PHP site with FastCGI process manager",
			Type:        "preset",
			Config:      mustJSON(TemplateConfig{
				HostType:    "php",
				TLSMode:     "auto",
				TLSEnabled:  boolPtr(true),
				HTTPRedirect: boolPtr(true),
				Compression: boolPtr(true),
				WebSocket:   boolPtr(false),
				CorsEnabled: boolPtr(false),
				SecurityHeaders: boolPtr(false),
				CacheEnabled: boolPtr(false),
				CacheTTL:    300,
				RootPath:    "/var/www/php",
				PHPFastCGI:  "localhost:9000",
				DirectoryBrowse: boolPtr(false),
				RedirectCode: 301,
			}),
		},
		{
			Name:        "Static File Download Site",
			Description: "Static file server with directory browsing enabled",
			Type:        "preset",
			Config:      mustJSON(TemplateConfig{
				HostType:    "static",
				TLSMode:     "auto",
				TLSEnabled:  boolPtr(true),
				HTTPRedirect: boolPtr(true),
				Compression: boolPtr(false),
				WebSocket:   boolPtr(false),
				CorsEnabled: boolPtr(false),
				SecurityHeaders: boolPtr(false),
				CacheEnabled: boolPtr(false),
				CacheTTL:    300,
				RootPath:    "/var/www/files",
				DirectoryBrowse: boolPtr(true),
				RedirectCode: 301,
			}),
		},
		{
			Name:        "WebSocket Application",
			Description: "Reverse proxy with WebSocket support enabled",
			Type:        "preset",
			Config:      mustJSON(TemplateConfig{
				HostType:    "proxy",
				TLSMode:     "auto",
				TLSEnabled:  boolPtr(true),
				HTTPRedirect: boolPtr(true),
				Compression: boolPtr(false),
				WebSocket:   boolPtr(true),
				CorsEnabled: boolPtr(false),
				SecurityHeaders: boolPtr(false),
				CacheEnabled: boolPtr(false),
				CacheTTL:    300,
				DirectoryBrowse: boolPtr(false),
				RedirectCode: 301,
				Upstreams: []model.UpstreamInput{
					{Address: "localhost:3000", Weight: 1},
				},
			}),
		},
	}

	for _, p := range presets {
		if err := s.db.Create(&p).Error; err != nil {
			log.Printf("Warning: failed to seed preset template '%s': %v", p.Name, err)
		}
	}
	log.Println("Seeded 6 preset templates")
}

// hostToTemplateConfig converts a Host (with loaded associations) to a TemplateConfig.
func (s *TemplateService) hostToTemplateConfig(host *model.Host) TemplateConfig {
	cfg := TemplateConfig{
		HostType:         host.HostType,
		TLSMode:          host.TLSMode,
		TLSEnabled:       copyBoolPtr(host.TLSEnabled),
		HTTPRedirect:     copyBoolPtr(host.HTTPRedirect),
		WebSocket:        copyBoolPtr(host.WebSocket),
		Compression:      copyBoolPtr(host.Compression),
		CorsEnabled:      copyBoolPtr(host.CorsEnabled),
		CorsOrigins:      host.CorsOrigins,
		CorsMethods:      host.CorsMethods,
		CorsHeaders:      host.CorsHeaders,
		SecurityHeaders:  copyBoolPtr(host.SecurityHeaders),
		ErrorPagePath:    host.ErrorPagePath,
		CacheEnabled:     copyBoolPtr(host.CacheEnabled),
		CacheTTL:         host.CacheTTL,
		RootPath:         host.RootPath,
		DirectoryBrowse:  copyBoolPtr(host.DirectoryBrowse),
		PHPFastCGI:       host.PHPFastCGI,
		IndexFiles:       host.IndexFiles,
		CustomDirectives: host.CustomDirectives,
		RedirectURL:      host.RedirectURL,
		RedirectCode:     host.RedirectCode,
	}

	for _, u := range host.Upstreams {
		cfg.Upstreams = append(cfg.Upstreams, model.UpstreamInput{
			Address: u.Address,
			Weight:  u.Weight,
		})
	}

	for _, h := range host.CustomHeaders {
		cfg.CustomHeaders = append(cfg.CustomHeaders, model.HeaderInput{
			Direction: h.Direction,
			Operation: h.Operation,
			Name:      h.Name,
			Value:     h.Value,
		})
	}

	for _, a := range host.AccessRules {
		cfg.AccessRules = append(cfg.AccessRules, model.AccessInput{
			RuleType: a.RuleType,
			IPRange:  a.IPRange,
		})
	}

	// Store password hash directly for snapshot
	for _, ba := range host.BasicAuths {
		cfg.BasicAuths = append(cfg.BasicAuths, TemplateBasicAuth{
			Username:     ba.Username,
			PasswordHash: ba.PasswordHash,
		})
	}

	return cfg
}

// copyBoolPtrOrDefault copies a *bool pointer, returning a pointer to defaultVal if nil.
func copyBoolPtrOrDefault(ptr *bool, defaultVal bool) *bool {
	if ptr == nil {
		return boolPtr(defaultVal)
	}
	v := *ptr
	return &v
}

// mustJSON marshals a value to a JSON string, panicking on error (used for static preset data).
func mustJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal preset config: %v", err))
	}
	return string(data)
}
