// seedgen generates a gzipped JSON file of app store seed data from a local
// Runtipi-compatible app repository. The output is embedded into the WebCasa
// binary via go:embed so the app store is immediately populated on first run.
//
// Usage: go run ./plugins/appstore/cmd/seedgen <repo-path> <output.json.gz>
package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"

	"github.com/web-casa/webcasa/plugins/appstore"
)

// SeedApp is the JSON-serializable format for embedded seed data.
// Fields mirror AppDefinition columns populated by syncApps.
type SeedApp struct {
	AppID       string               `json:"app_id"`
	Name        string               `json:"name"`
	ShortDesc   string               `json:"short_desc"`
	Description string               `json:"description,omitempty"`
	DescZh      string               `json:"desc_zh,omitempty"`
	Version     string               `json:"version"`
	Author      string               `json:"author"`
	Categories  []string             `json:"categories"`
	Port        int                  `json:"port"`
	Exposable   bool                 `json:"exposable"`
	Available   bool                 `json:"available"`
	ComposeFile string               `json:"compose_file"`
	ConfigJSON  string               `json:"config_json"`
	FormFields  string               `json:"form_fields"`
	Website     string               `json:"website"`
	Source      string               `json:"source"`
	UrlSuffix   string               `json:"url_suffix,omitempty"`
	Deprecated  bool                 `json:"deprecated,omitempty"`
	NoGUI       bool                 `json:"no_gui,omitempty"`
	ForceExpose bool                 `json:"force_expose,omitempty"`
	I18nJSON    string               `json:"i18n_json,omitempty"`
	GenerateVapidKeys bool           `json:"generate_vapid_keys,omitempty"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: seedgen <repo-path> <output.json.gz>\n")
		os.Exit(1)
	}
	repoPath := os.Args[1]
	outPath := os.Args[2]

	apps, warnings, err := appstore.ParseSourceRepo(repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing repo: %v\n", err)
		os.Exit(1)
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}

	var seeds []SeedApp
	for _, app := range apps {
		exposable := true
		if app.Config.Exposable != nil {
			exposable = *app.Config.Exposable
		}
		available := true
		if app.Config.Available != nil {
			available = *app.Config.Available
		}

		formFieldsJSON, _ := json.Marshal(app.Config.FormFields)
		configJSON, _ := json.Marshal(app.Config)

		var i18nJSON string
		if len(app.I18n) > 0 {
			if b, err := json.Marshal(app.I18n); err == nil {
				i18nJSON = string(b)
			}
		}

		seeds = append(seeds, SeedApp{
			AppID:             app.Config.ID,
			Name:              app.Config.Name,
			ShortDesc:         app.Config.ShortDesc,
			Description:       app.Description,
			DescZh:            app.DescZh,
			Version:           app.Config.Version,
			Author:            app.Config.Author,
			Categories:        app.Config.Categories,
			Port:              app.Config.Port,
			Exposable:         exposable,
			Available:         available,
			ComposeFile:       app.ComposeFile,
			ConfigJSON:        string(configJSON),
			FormFields:        string(formFieldsJSON),
			Website:           app.Config.Website,
			Source:             app.Config.Source,
			UrlSuffix:         app.Config.UrlSuffix,
			Deprecated:        app.Config.Deprecated,
			NoGUI:             app.Config.NoGUI,
			ForceExpose:       app.Config.ForceExpose,
			I18nJSON:          i18nJSON,
			GenerateVapidKeys: app.Config.GenerateVapidKeys,
		})
	}

	f, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	gz, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	defer gz.Close()

	enc := json.NewEncoder(gz)
	if err := enc.Encode(seeds); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Generated seed data: %d apps → %s\n", len(seeds), outPath)
}
