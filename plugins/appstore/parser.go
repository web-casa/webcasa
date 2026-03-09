package appstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppConfig represents the parsed config.json for a Runtipi-compatible app.
type AppConfig struct {
	ID                     string      `json:"id"`
	Name                   string      `json:"name"`
	Version                string      `json:"version"`
	TipiVersion            int         `json:"tipi_version"`
	ShortDesc              string      `json:"short_desc"`
	Description            string      `json:"description"`
	Author                 string      `json:"author"`
	Port                   int         `json:"port"`
	Categories             []string    `json:"categories"`
	Source                 string      `json:"source"`
	Website                string      `json:"website"`
	Exposable              *bool       `json:"exposable"`
	Available              *bool       `json:"available"`
	FormFields             []FormField `json:"form_fields"`
	SupportedArchitectures []string    `json:"supported_architectures"`
	UrlSuffix              string      `json:"url_suffix"`
	Deprecated             bool        `json:"deprecated"`
	NoGUI                  bool        `json:"no_gui"`
	ForceExpose            bool        `json:"force_expose"`
	GenerateVapidKeys      bool        `json:"generate_vapid_keys"`
}

// FormField defines a user-configurable parameter in an app's install form.
type FormField struct {
	Type         string       `json:"type"`                    // text, password, email, number, fqdn, ip, fqdnip, random, boolean
	Label        string       `json:"label"`
	Hint         string       `json:"hint,omitempty"`
	Placeholder  string       `json:"placeholder,omitempty"`
	Default      interface{}  `json:"default,omitempty"`
	Required     bool         `json:"required"`
	EnvVariable  string       `json:"env_variable"`
	Min          *int         `json:"min,omitempty"`
	Max          *int         `json:"max,omitempty"`
	Regex        string       `json:"regex,omitempty"`
	PatternError string       `json:"pattern_error,omitempty"`
	Options      []FormOption `json:"options,omitempty"`
	Encoding     string       `json:"encoding,omitempty"`      // "base64" for base64-encoded random values
}

// FormOption is a selectable option for dropdown form fields.
type FormOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// AppI18n holds localized strings for an app (from metadata/i18n/{lang}.json).
type AppI18n struct {
	Name      string                       `json:"name"`
	ShortDesc string                       `json:"short_desc"`
	Fields    map[string]FormFieldI18n     `json:"form_fields,omitempty"`
}

// FormFieldI18n holds localized label/hint for a form field.
type FormFieldI18n struct {
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// ParsedApp is the result of parsing one app directory.
type ParsedApp struct {
	Config      *AppConfig
	ComposeFile string            // raw docker-compose.yml content
	Description string            // markdown from metadata/description.md
	DescZh      string            // markdown from metadata/description.zh.md
	LogoPath    string            // relative path to logo (e.g. "nextcloud/metadata/logo.jpg")
	I18n        map[string]*AppI18n // lang -> translations (e.g. "zh" -> {...})
}

// ParseAppDir parses a single app directory containing config.json + docker-compose.yml.
func ParseAppDir(dirPath string) (*ParsedApp, error) {
	// Parse config.json
	configPath := filepath.Join(dirPath, "config.json")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config.json: %w", err)
	}

	var config AppConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("parse config.json: %w", err)
	}

	if config.ID == "" || config.Name == "" {
		return nil, fmt.Errorf("config.json missing required fields (id, name)")
	}

	// Read docker-compose.yml
	composePath := filepath.Join(dirPath, "docker-compose.yml")
	composeData, err := os.ReadFile(composePath)
	if err != nil {
		return nil, fmt.Errorf("read docker-compose.yml: %w", err)
	}

	result := &ParsedApp{
		Config:      &config,
		ComposeFile: string(composeData),
	}

	// Read optional metadata/description.md
	descPath := filepath.Join(dirPath, "metadata", "description.md")
	if data, err := os.ReadFile(descPath); err == nil {
		result.Description = string(data)
	}

	// Read optional metadata/description.zh.md
	descZhPath := filepath.Join(dirPath, "metadata", "description.zh.md")
	if data, err := os.ReadFile(descZhPath); err == nil {
		result.DescZh = string(data)
	}

	// Check for logo image
	for _, ext := range []string{".jpg", ".png", ".svg", ".webp"} {
		logoPath := filepath.Join(dirPath, "metadata", "logo"+ext)
		if _, err := os.Stat(logoPath); err == nil {
			// Store relative to the source root
			result.LogoPath = logoPath
			break
		}
	}

	// Load i18n translations (metadata/i18n/*.json)
	i18nDir := filepath.Join(dirPath, "metadata", "i18n")
	if entries, err := os.ReadDir(i18nDir); err == nil {
		result.I18n = make(map[string]*AppI18n)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			lang := strings.TrimSuffix(entry.Name(), ".json")
			data, err := os.ReadFile(filepath.Join(i18nDir, entry.Name()))
			if err != nil {
				continue
			}
			var i18n AppI18n
			if err := json.Unmarshal(data, &i18n); err == nil {
				result.I18n[lang] = &i18n
			}
		}
	}

	return result, nil
}

// ParseSourceRepo walks a cloned repo and returns all parsed apps.
// It checks both the repo root and the "apps/" subdirectory (Runtipi convention).
func ParseSourceRepo(repoPath string) ([]*ParsedApp, []string, error) {
	// Runtipi repos put apps under "apps/"; also support flat root layout.
	scanDir := repoPath
	if info, err := os.Stat(filepath.Join(repoPath, "apps")); err == nil && info.IsDir() {
		scanDir = filepath.Join(repoPath, "apps")
	}

	entries, err := os.ReadDir(scanDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read repo dir: %w", err)
	}

	var apps []*ParsedApp
	var warnings []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Skip hidden dirs, .git, docs, etc.
		if strings.HasPrefix(name, ".") || name == "docs" || name == "scripts" {
			continue
		}

		appDir := filepath.Join(scanDir, name)

		// Check if this dir has a config.json (is it an app?)
		if _, err := os.Stat(filepath.Join(appDir, "config.json")); os.IsNotExist(err) {
			continue
		}

		app, err := ParseAppDir(appDir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip %s: %v", name, err))
			continue
		}

		apps = append(apps, app)
	}

	return apps, warnings, nil
}

// ParseTemplateRepo walks a cloned repo and returns project template definitions.
// Each subdirectory is expected to have a template.json.
func ParseTemplateRepo(repoPath string) ([]*TemplateConfig, []string, error) {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read repo dir: %w", err)
	}

	var templates []*TemplateConfig
	var warnings []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		tplPath := filepath.Join(repoPath, name, "template.json")
		data, err := os.ReadFile(tplPath)
		if err != nil {
			continue // not a template dir
		}

		var tpl TemplateConfig
		if err := json.Unmarshal(data, &tpl); err != nil {
			warnings = append(warnings, fmt.Sprintf("skip %s: %v", name, err))
			continue
		}

		if tpl.ID == "" {
			tpl.ID = name
		}
		templates = append(templates, &tpl)
	}

	return templates, warnings, nil
}

// TemplateConfig represents the parsed template.json for a project template.
type TemplateConfig struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Framework   string   `json:"framework"`
	GitURL      string   `json:"git_url"`
	Branch      string   `json:"branch"`
	Tags        []string `json:"tags"`
	LogoURL     string   `json:"logo_url"`
}
