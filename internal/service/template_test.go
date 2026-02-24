package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/caddypanel/caddypanel/internal/model"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// setupTestTemplateService creates a TemplateService backed by a test DB.
func setupTestTemplateService(t *testing.T) (*TemplateService, *HostService) {
	t.Helper()
	db := setupTestDB(t)
	// Also migrate Template model
	db.AutoMigrate(&model.Template{})
	hostSvc := setupTestHostService(t, db)
	tplSvc := NewTemplateService(db, hostSvc)
	return tplSvc, hostSvc
}

// Feature: phase6-enhancements, Property 16: Host-Template-Host round-trip — For any Host
// (with sub-table data), saving as template then creating from template produces a new Host
// with all config fields (except ID, Domain) and sub-table data equivalent to the original.
// **Validates: Requirements 6.2, 6.4**
func TestProperty16_HostTemplateHostRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("host-template-host round-trip preserves config", prop.ForAll(
		func(numUpstreams, numHeaders, numAccessRules, numBasicAuths int, domainSuffix int) bool {
			tplSvc, hostSvc := setupTestTemplateService(t)

			sourceDomain := fmt.Sprintf("tpl-src-%d.example.com", domainSuffix)
			newDomain := fmt.Sprintf("tpl-new-%d.example.com", domainSuffix)

			source := createTestHost(t, hostSvc, sourceDomain, numUpstreams, numHeaders, numAccessRules, numBasicAuths, 0)

			// Save as template
			tpl, err := tplSvc.SaveAsTemplate(source.ID, "Test Template", "test desc")
			if err != nil {
				t.Logf("SaveAsTemplate failed: %v", err)
				return false
			}

			// Create from template
			newHost, err := tplSvc.CreateFromTemplate(tpl.ID, newDomain)
			if err != nil {
				t.Logf("CreateFromTemplate failed: %v", err)
				return false
			}

			// Verify main table fields match (except ID, Domain)
			if newHost.Domain != newDomain {
				return false
			}
			if newHost.HostType != source.HostType {
				return false
			}
			if boolVal(newHost.Compression) != boolVal(source.Compression) {
				return false
			}
			if boolVal(newHost.CorsEnabled) != boolVal(source.CorsEnabled) {
				return false
			}
			if boolVal(newHost.SecurityHeaders) != boolVal(source.SecurityHeaders) {
				return false
			}
			if newHost.CorsOrigins != source.CorsOrigins {
				return false
			}
			if newHost.CorsMethods != source.CorsMethods {
				return false
			}
			if newHost.CorsHeaders != source.CorsHeaders {
				return false
			}
			if newHost.ErrorPagePath != source.ErrorPagePath {
				return false
			}
			if newHost.CustomDirectives != source.CustomDirectives {
				return false
			}
			if newHost.TLSMode != source.TLSMode {
				return false
			}

			// Verify sub-table counts
			if len(newHost.Upstreams) != len(source.Upstreams) {
				return false
			}
			if len(newHost.CustomHeaders) != len(source.CustomHeaders) {
				return false
			}
			if len(newHost.AccessRules) != len(source.AccessRules) {
				return false
			}
			if len(newHost.BasicAuths) != len(source.BasicAuths) {
				return false
			}

			// Verify upstream content
			for i, u := range newHost.Upstreams {
				if u.Address != source.Upstreams[i].Address || u.Weight != source.Upstreams[i].Weight {
					return false
				}
			}
			// Verify header content
			for i, h := range newHost.CustomHeaders {
				if h.Name != source.CustomHeaders[i].Name || h.Value != source.CustomHeaders[i].Value {
					return false
				}
			}
			// Verify access rule content
			for i, a := range newHost.AccessRules {
				if a.RuleType != source.AccessRules[i].RuleType || a.IPRange != source.AccessRules[i].IPRange {
					return false
				}
			}
			// Verify basic auth content (username + hash preserved)
			for i, ba := range newHost.BasicAuths {
				if ba.Username != source.BasicAuths[i].Username {
					return false
				}
				if ba.PasswordHash != source.BasicAuths[i].PasswordHash {
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 3),     // numUpstreams (min 1 for proxy)
		gen.IntRange(0, 3),     // numHeaders
		gen.IntRange(0, 3),     // numAccessRules
		gen.IntRange(0, 2),     // numBasicAuths
		gen.IntRange(1, 99999), // domainSuffix
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 17: 模板导出导入 round-trip — For any valid Template
// config snapshot JSON, export then import produces an equivalent Template record.
// **Validates: Requirements 6.11**
func TestProperty17_TemplateExportImportRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("template export-import round-trip preserves data", prop.ForAll(
		func(numUpstreams, numHeaders int, suffix int) bool {
			tplSvc, hostSvc := setupTestTemplateService(t)

			// Create a host and save as template
			domain := fmt.Sprintf("export-src-%d.example.com", suffix)
			source := createTestHost(t, hostSvc, domain, numUpstreams, numHeaders, 0, 0, 0)

			tpl, err := tplSvc.SaveAsTemplate(source.ID, fmt.Sprintf("Export Test %d", suffix), "export test desc")
			if err != nil {
				t.Logf("SaveAsTemplate failed: %v", err)
				return false
			}

			// Export
			exportData, err := tplSvc.Export(tpl.ID)
			if err != nil {
				t.Logf("Export failed: %v", err)
				return false
			}

			// Import
			imported, err := tplSvc.Import(exportData)
			if err != nil {
				t.Logf("Import failed: %v", err)
				return false
			}

			// Verify name and description match
			if imported.Name != tpl.Name {
				t.Logf("Name mismatch: %q vs %q", imported.Name, tpl.Name)
				return false
			}
			if imported.Description != tpl.Description {
				t.Logf("Description mismatch: %q vs %q", imported.Description, tpl.Description)
				return false
			}
			// Imported templates are always custom type
			if imported.Type != "custom" {
				return false
			}

			// Verify config content is equivalent
			var origCfg, importedCfg TemplateConfig
			if err := json.Unmarshal([]byte(tpl.Config), &origCfg); err != nil {
				return false
			}
			if err := json.Unmarshal([]byte(imported.Config), &importedCfg); err != nil {
				return false
			}

			if origCfg.HostType != importedCfg.HostType {
				return false
			}
			if len(origCfg.Upstreams) != len(importedCfg.Upstreams) {
				return false
			}
			for i, u := range origCfg.Upstreams {
				if u.Address != importedCfg.Upstreams[i].Address {
					return false
				}
			}
			if len(origCfg.CustomHeaders) != len(importedCfg.CustomHeaders) {
				return false
			}
			for i, h := range origCfg.CustomHeaders {
				if h.Name != importedCfg.CustomHeaders[i].Name || h.Value != importedCfg.CustomHeaders[i].Value {
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 3),     // numUpstreams
		gen.IntRange(0, 3),     // numHeaders
		gen.IntRange(1, 99999), // suffix
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 18: 预设模板不可变 — For any type="preset" template,
// delete and update operations should be rejected with an error.
// **Validates: Requirements 6.9**
func TestProperty18_PresetTemplatesImmutable(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("preset templates reject delete and update", prop.ForAll(
		func(nameIdx int) bool {
			tplSvc, _ := setupTestTemplateService(t)

			// Seed presets
			tplSvc.SeedPresets()

			// Get all templates (should be 6 presets)
			templates, err := tplSvc.List()
			if err != nil || len(templates) == 0 {
				t.Logf("List failed or empty: %v", err)
				return false
			}

			// Pick a preset template
			idx := nameIdx % len(templates)
			preset := templates[idx]
			if preset.Type != "preset" {
				return false
			}

			// Attempt to delete — should fail
			err = tplSvc.Delete(preset.ID)
			if err == nil || err.Error() != "error.preset_immutable" {
				t.Logf("Delete should have returned error.preset_immutable, got: %v", err)
				return false
			}

			// Attempt to update — should fail
			_, err = tplSvc.Update(preset.ID, "New Name", "New Desc", "")
			if err == nil || err.Error() != "error.preset_immutable" {
				t.Logf("Update should have returned error.preset_immutable, got: %v", err)
				return false
			}

			// Verify preset still exists unchanged
			after, err := tplSvc.Get(preset.ID)
			if err != nil {
				return false
			}
			if after.Name != preset.Name || after.Description != preset.Description {
				return false
			}

			return true
		},
		gen.IntRange(0, 5), // nameIdx to pick different presets
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 19: 无效模板 JSON 拒绝导入 — For any invalid JSON string
// (syntax errors, missing required fields like host_type), template import should return an error.
// **Validates: Requirements 6.7**
func TestProperty19_InvalidTemplateJSONRejected(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Generator for invalid JSON inputs
	invalidJSONGen := gen.OneConstOf(
		// Completely invalid JSON
		"not json at all",
		"{invalid json}",
		"",
		"[]",
		// Valid JSON but wrong structure
		`{"foo": "bar"}`,
		// Missing template field
		`{"version": "1.0", "exported_at": "2024-01-01T00:00:00Z"}`,
		// Missing config
		`{"version": "1.0", "exported_at": "2024-01-01T00:00:00Z", "template": {"name": "test"}}`,
		// Empty config
		`{"version": "1.0", "exported_at": "2024-01-01T00:00:00Z", "template": {"name": "test", "config": {}}}`,
		// Config missing host_type
		`{"version": "1.0", "exported_at": "2024-01-01T00:00:00Z", "template": {"name": "test", "config": {"tls_mode": "auto"}}}`,
		// Missing name
		`{"version": "1.0", "exported_at": "2024-01-01T00:00:00Z", "template": {"name": "", "config": {"host_type": "proxy"}}}`,
	)

	properties.Property("invalid template JSON is rejected on import", prop.ForAll(
		func(invalidJSON string) bool {
			tplSvc, _ := setupTestTemplateService(t)

			_, err := tplSvc.Import([]byte(invalidJSON))
			if err == nil {
				t.Logf("Import should have failed for input: %s", invalidJSON)
				return false
			}

			// Error should be one of the expected error keys
			errMsg := err.Error()
			validErrors := map[string]bool{
				"error.invalid_template_json":    true,
				"error.template_missing_fields":  true,
			}
			if !validErrors[errMsg] {
				t.Logf("Unexpected error: %s for input: %s", errMsg, invalidJSON)
				return false
			}

			return true
		},
		invalidJSONGen,
	))

	properties.TestingRun(t)
}
