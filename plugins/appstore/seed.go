package appstore

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"log/slog"

	"gorm.io/gorm"
)

//go:embed seed_apps.json.gz
var seedAppsGZ []byte

// seedAppData mirrors the JSON structure produced by the seedgen tool.
type seedAppData struct {
	AppID             string   `json:"app_id"`
	Name              string   `json:"name"`
	ShortDesc         string   `json:"short_desc"`
	Description       string   `json:"description"`
	DescZh            string   `json:"desc_zh"`
	Version           string   `json:"version"`
	Author            string   `json:"author"`
	Categories        []string `json:"categories"`
	Port              int      `json:"port"`
	Exposable         bool     `json:"exposable"`
	Available         bool     `json:"available"`
	ComposeFile       string   `json:"compose_file"`
	ConfigJSON        string   `json:"config_json"`
	FormFields        string   `json:"form_fields"`
	Website           string   `json:"website"`
	Source            string   `json:"source"`
	UrlSuffix         string   `json:"url_suffix"`
	Deprecated        bool     `json:"deprecated"`
	NoGUI             bool     `json:"no_gui"`
	ForceExpose       bool     `json:"force_expose"`
	I18nJSON          string   `json:"i18n_json"`
	GenerateVapidKeys bool     `json:"generate_vapid_keys"`
}

// SeedAppsFromEmbedded populates the app_definitions table from embedded seed
// data. It only runs when the given source has zero apps (idempotent).
func SeedAppsFromEmbedded(db *gorm.DB, sourceID uint, logger *slog.Logger) {
	if len(seedAppsGZ) == 0 {
		return
	}

	// Skip if source already has apps (e.g. already synced from git)
	var count int64
	db.Model(&AppDefinition{}).Where("source_id = ?", sourceID).Count(&count)
	if count > 0 {
		return
	}

	// Decompress
	gz, err := gzip.NewReader(bytes.NewReader(seedAppsGZ))
	if err != nil {
		logger.Error("seed: decompress failed", "err", err)
		return
	}
	defer gz.Close()

	var seeds []seedAppData
	if err := json.NewDecoder(gz).Decode(&seeds); err != nil {
		logger.Error("seed: decode failed", "err", err)
		return
	}

	// Batch insert into DB
	for _, s := range seeds {
		categoriesJSON, _ := json.Marshal(s.Categories)

		def := AppDefinition{
			SourceID:    sourceID,
			AppID:       s.AppID,
			Name:        s.Name,
			ShortDesc:   s.ShortDesc,
			Description: s.Description,
			DescZh:      s.DescZh,
			Version:     s.Version,
			Author:      s.Author,
			Categories:  string(categoriesJSON),
			Port:        s.Port,
			Exposable:   s.Exposable,
			Available:   s.Available,
			ComposeFile: s.ComposeFile,
			ConfigJSON:  s.ConfigJSON,
			FormFields:  s.FormFields,
			Website:     s.Website,
			Source:      s.Source,
			UrlSuffix:   s.UrlSuffix,
			Deprecated:  s.Deprecated,
			NoGUI:       s.NoGUI,
			ForceExpose: s.ForceExpose,
			I18nJSON:    s.I18nJSON,
		}
		db.Create(&def)
	}

	logger.Info("seeded apps from embedded data", "source_id", sourceID, "count", len(seeds))
}
