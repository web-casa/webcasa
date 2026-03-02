package appstore

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

// SourceManager handles Git repo syncing and app directory scanning.
type SourceManager struct {
	db      *gorm.DB
	dataDir string // data/plugins/appstore/
	logger  *slog.Logger
}

// NewSourceManager creates a SourceManager.
func NewSourceManager(db *gorm.DB, dataDir string, logger *slog.Logger) *SourceManager {
	return &SourceManager{db: db, dataDir: dataDir, logger: logger}
}

// ListSources returns all configured sources.
func (sm *SourceManager) ListSources() ([]AppSource, error) {
	var sources []AppSource
	if err := sm.db.Order("is_default DESC, id ASC").Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

// AddSource creates a new source and triggers initial sync.
func (sm *SourceManager) AddSource(name, url, branch, kind string) (*AppSource, error) {
	if branch == "" {
		branch = "main"
	}
	if kind == "" {
		kind = "app"
	}

	src := &AppSource{
		Name:       name,
		URL:        url,
		Branch:     branch,
		Kind:       kind,
		SyncStatus: "pending",
	}
	if err := sm.db.Create(src).Error; err != nil {
		return nil, fmt.Errorf("create source: %w", err)
	}

	// Trigger async sync
	go func() {
		if err := sm.SyncSource(src.ID); err != nil {
			sm.logger.Error("initial sync failed", "source_id", src.ID, "err", err)
		}
	}()

	return src, nil
}

// RemoveSource removes a source and all its app/template definitions.
func (sm *SourceManager) RemoveSource(id uint) error {
	var src AppSource
	if err := sm.db.First(&src, id).Error; err != nil {
		return err
	}
	if src.IsDefault {
		return fmt.Errorf("cannot remove default source")
	}

	// Delete associated apps/templates
	sm.db.Where("source_id = ?", id).Delete(&AppDefinition{})
	sm.db.Where("source_id = ?", id).Delete(&ProjectTemplate{})

	// Remove local clone
	srcDir := sm.sourceDir(id)
	os.RemoveAll(srcDir)

	return sm.db.Delete(&AppSource{}, id).Error
}

// SyncSource clones or pulls a Git repo and parses all apps/templates within it.
func (sm *SourceManager) SyncSource(sourceID uint) error {
	var src AppSource
	if err := sm.db.First(&src, sourceID).Error; err != nil {
		return err
	}

	// Update status to syncing
	sm.db.Model(&src).Updates(map[string]interface{}{
		"sync_status": "syncing",
		"sync_error":  "",
	})

	srcDir := sm.sourceDir(sourceID)

	// Clone or pull
	if err := sm.gitSync(src.URL, src.Branch, srcDir); err != nil {
		sm.db.Model(&src).Updates(map[string]interface{}{
			"sync_status": "error",
			"sync_error":  err.Error(),
		})
		return fmt.Errorf("git sync: %w", err)
	}

	// Parse based on kind
	var syncErr error
	switch src.Kind {
	case "template":
		syncErr = sm.syncTemplates(sourceID, srcDir)
	default: // "app"
		syncErr = sm.syncApps(sourceID, srcDir)
	}

	if syncErr != nil {
		sm.db.Model(&src).Updates(map[string]interface{}{
			"sync_status": "error",
			"sync_error":  syncErr.Error(),
		})
		return syncErr
	}

	now := time.Now()
	sm.db.Model(&src).Updates(map[string]interface{}{
		"sync_status": "synced",
		"sync_error":  "",
		"last_sync_at": &now,
	})

	sm.logger.Info("source synced", "source_id", sourceID, "name", src.Name)
	return nil
}

// SyncAllSources syncs all active sources.
func (sm *SourceManager) SyncAllSources() {
	var sources []AppSource
	sm.db.Find(&sources)
	for _, src := range sources {
		if err := sm.SyncSource(src.ID); err != nil {
			sm.logger.Error("sync source failed", "id", src.ID, "name", src.Name, "err", err)
		}
	}
}

// GetAppLogoPath returns the filesystem path for an app's logo.
func (sm *SourceManager) GetAppLogoPath(sourceID uint, logoPath string) string {
	if logoPath == "" {
		return ""
	}
	// logoPath is stored as absolute path during parsing
	if filepath.IsAbs(logoPath) {
		return logoPath
	}
	return filepath.Join(sm.sourceDir(sourceID), logoPath)
}

// sourceDir returns the local directory for a source's cloned repo.
func (sm *SourceManager) sourceDir(sourceID uint) string {
	return filepath.Join(sm.dataDir, "sources", fmt.Sprintf("%d", sourceID))
}

// gitSync clones or pulls a Git repo.
func (sm *SourceManager) gitSync(url, branch, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// Already cloned — pull
		cmd := exec.Command("git", "pull", "--ff-only")
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git pull: %s", strings.TrimSpace(string(output)))
		}
		return nil
	}

	// Clone (shallow for speed)
	if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
		return err
	}
	cmd := exec.Command("git", "clone", "--depth", "1", "--branch", branch, url, dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// syncApps parses all app directories in a cloned repo and upserts AppDefinition records.
func (sm *SourceManager) syncApps(sourceID uint, repoPath string) error {
	apps, warnings, err := ParseSourceRepo(repoPath)
	if err != nil {
		return err
	}

	for _, w := range warnings {
		sm.logger.Warn("parse warning", "source_id", sourceID, "warning", w)
	}

	// Track seen app IDs for cleanup
	seenIDs := make(map[string]bool)

	for _, app := range apps {
		seenIDs[app.Config.ID] = true

		categoriesJSON, _ := json.Marshal(app.Config.Categories)
		formFieldsJSON, _ := json.Marshal(app.Config.FormFields)

		exposable := true
		if app.Config.Exposable != nil {
			exposable = *app.Config.Exposable
		}
		available := true
		if app.Config.Available != nil {
			available = *app.Config.Available
		}

		def := AppDefinition{
			SourceID:    sourceID,
			AppID:       app.Config.ID,
			Name:        app.Config.Name,
			ShortDesc:   app.Config.ShortDesc,
			Description: app.Description,
			Version:     app.Config.Version,
			Author:      app.Config.Author,
			Categories:  string(categoriesJSON),
			Port:        app.Config.Port,
			Exposable:   exposable,
			ComposeFile: app.ComposeFile,
			ConfigJSON:  string(formFieldsJSON),
			FormFields:  string(formFieldsJSON),
			LogoPath:    app.LogoPath,
			Website:     app.Config.Website,
			Source:       app.Config.Source,
			Available:   available,
		}

		// Upsert by source_id + app_id
		var existing AppDefinition
		if err := sm.db.Where("source_id = ? AND app_id = ?", sourceID, app.Config.ID).First(&existing).Error; err == nil {
			def.ID = existing.ID
			sm.db.Save(&def)
		} else {
			sm.db.Create(&def)
		}
	}

	// Remove apps that no longer exist in the source
	var existingApps []AppDefinition
	sm.db.Where("source_id = ?", sourceID).Find(&existingApps)
	for _, a := range existingApps {
		if !seenIDs[a.AppID] {
			sm.db.Delete(&a)
		}
	}

	sm.logger.Info("synced apps", "source_id", sourceID, "count", len(apps))
	return nil
}

// syncTemplates parses all template directories in a cloned repo.
func (sm *SourceManager) syncTemplates(sourceID uint, repoPath string) error {
	templates, warnings, err := ParseTemplateRepo(repoPath)
	if err != nil {
		return err
	}

	for _, w := range warnings {
		sm.logger.Warn("template parse warning", "source_id", sourceID, "warning", w)
	}

	seenIDs := make(map[string]bool)

	for _, tpl := range templates {
		seenIDs[tpl.ID] = true

		tagsJSON, _ := json.Marshal(tpl.Tags)
		branch := tpl.Branch
		if branch == "" {
			branch = "main"
		}

		pt := ProjectTemplate{
			SourceID:    sourceID,
			TemplateID:  tpl.ID,
			Name:        tpl.Name,
			Description: tpl.Description,
			Framework:   tpl.Framework,
			GitURL:      tpl.GitURL,
			Branch:      branch,
			Tags:        string(tagsJSON),
			LogoURL:     tpl.LogoURL,
		}

		var existing ProjectTemplate
		if err := sm.db.Where("source_id = ? AND template_id = ?", sourceID, tpl.ID).First(&existing).Error; err == nil {
			pt.ID = existing.ID
			sm.db.Save(&pt)
		} else {
			sm.db.Create(&pt)
		}
	}

	// Remove templates that no longer exist
	var existingTpls []ProjectTemplate
	sm.db.Where("source_id = ?", sourceID).Find(&existingTpls)
	for _, t := range existingTpls {
		if !seenIDs[t.TemplateID] {
			sm.db.Delete(&t)
		}
	}

	sm.logger.Info("synced templates", "source_id", sourceID, "count", len(templates))
	return nil
}

// SeedOfficialSources creates default sources if none exist.
func SeedOfficialSources(db *gorm.DB) {
	var count int64
	db.Model(&AppSource{}).Count(&count)
	if count > 0 {
		return
	}

	// Official Runtipi-compatible app source
	db.Create(&AppSource{
		Name:       "Runtipi App Store",
		URL:        "https://github.com/runtipi/runtipi-appstore",
		Branch:     "master",
		Kind:       "app",
		IsDefault:  true,
		SyncStatus: "pending",
	})
}
