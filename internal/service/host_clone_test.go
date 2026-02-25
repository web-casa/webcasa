package service

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDBCounter uint64

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	id := atomic.AddUint64(&testDBCounter, 1)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:testdb_%d?mode=memory&cache=shared", id)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	err = db.AutoMigrate(
		&model.Host{},
		&model.Upstream{},
		&model.Route{},
		&model.CustomHeader{},
		&model.AccessRule{},
		&model.BasicAuth{},
		&model.AuditLog{},
		&model.Setting{},
		&model.Group{},
		&model.Tag{},
		&model.HostTag{},
	)
	if err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}
	// Seed auto_reload=false so ApplyConfig doesn't try to run caddy
	db.Create(&model.Setting{Key: "auto_reload", Value: "false"})
	return db
}

// setupTestHostService creates a HostService backed by the test DB
func setupTestHostService(t *testing.T, db *gorm.DB) *HostService {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "webcasa-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	cfg := &config.Config{
		DataDir:       tmpDir,
		CaddyfilePath: tmpDir + "/Caddyfile",
		CaddyBin:      "echo", // dummy binary that always succeeds
		LogDir:        tmpDir + "/logs",
		AdminAPI:      "http://localhost:2019",
	}
	os.MkdirAll(cfg.LogDir, 0755)
	caddyMgr := caddy.NewManager(cfg)
	return NewHostService(db, caddyMgr, cfg)
}

// createTestHost inserts a host with sub-table data into the DB via the service
func createTestHost(t *testing.T, svc *HostService, domain string, numUpstreams, numHeaders, numAccessRules, numBasicAuths, numRoutes int) *model.Host {
	t.Helper()

	var upstreams []model.UpstreamInput
	for i := 0; i < numUpstreams; i++ {
		upstreams = append(upstreams, model.UpstreamInput{
			Address: fmt.Sprintf("localhost:%d", 8080+i),
			Weight:  i + 1,
		})
	}

	var headers []model.HeaderInput
	for i := 0; i < numHeaders; i++ {
		headers = append(headers, model.HeaderInput{
			Direction: "response",
			Operation: "set",
			Name:      fmt.Sprintf("X-Custom-%d", i),
			Value:     fmt.Sprintf("value-%d", i),
		})
	}

	var accessRules []model.AccessInput
	for i := 0; i < numAccessRules; i++ {
		accessRules = append(accessRules, model.AccessInput{
			RuleType: "allow",
			IPRange:  fmt.Sprintf("192.168.%d.0/24", i),
		})
	}

	var basicAuths []model.BasicAuthInput
	for i := 0; i < numBasicAuths; i++ {
		basicAuths = append(basicAuths, model.BasicAuthInput{
			Username: fmt.Sprintf("user%d", i),
			Password: fmt.Sprintf("password%d", i),
		})
	}

	enabled := true
	compression := true
	corsEnabled := false
	secHeaders := true

	req := &model.HostCreateRequest{
		Domain:          domain,
		HostType:        "proxy",
		Enabled:         &enabled,
		Compression:     &compression,
		CorsEnabled:     &corsEnabled,
		SecurityHeaders: &secHeaders,
		CorsOrigins:     "https://example.com",
		CorsMethods:     "GET,POST",
		CorsHeaders:     "Authorization",
		ErrorPagePath:   "/errors",
		CustomDirectives: "log {\n\toutput stdout\n}",
		Upstreams:       upstreams,
		CustomHeaders:   headers,
		AccessRules:     accessRules,
		BasicAuths:      basicAuths,
	}

	host, err := svc.Create(req)
	if err != nil {
		t.Fatalf("failed to create test host: %v", err)
	}

	// Add routes directly to DB since HostCreateRequest doesn't have routes
	for i := 0; i < numRoutes; i++ {
		route := model.Route{
			HostID:    host.ID,
			Path:      fmt.Sprintf("/path%d/*", i),
			SortOrder: i,
		}
		if err := svc.db.Create(&route).Error; err != nil {
			t.Fatalf("failed to create test route: %v", err)
		}
	}

	// Re-fetch to get routes
	host, err = svc.Get(host.ID)
	if err != nil {
		t.Fatalf("failed to re-fetch host: %v", err)
	}
	return host
}

// Feature: phase6-enhancements, Property 1: 克隆产生等价 Host — For any Host with any combination
// of main table fields and sub-table data, clone should produce a new Host with all config fields
// (except ID, Domain, CreatedAt, UpdatedAt) identical, and all sub-table records count and content
// (except ID, HostID) identical.
// **Validates: Requirements 1.2, 1.3**
func TestProperty1_CloneProducesEquivalentHost(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("clone produces equivalent host", prop.ForAll(
		func(numUpstreams, numHeaders, numAccessRules, numBasicAuths, numRoutes int, domainSuffix int) bool {
			db := setupTestDB(t)
			svc := setupTestHostService(t, db)

			sourceDomain := fmt.Sprintf("source-%d.example.com", domainSuffix)
			cloneDomain := fmt.Sprintf("clone-%d.example.com", domainSuffix)

			source := createTestHost(t, svc, sourceDomain, numUpstreams, numHeaders, numAccessRules, numBasicAuths, numRoutes)

			cloned, err := svc.CloneHost(source.ID, cloneDomain)
			if err != nil {
				t.Logf("CloneHost failed: %v", err)
				return false
			}

			// Verify main table fields are identical (except ID, Domain, CreatedAt, UpdatedAt)
			if cloned.HostType != source.HostType {
				return false
			}
			if boolVal(cloned.Enabled) != boolVal(source.Enabled) {
				return false
			}
			if boolVal(cloned.TLSEnabled) != boolVal(source.TLSEnabled) {
				return false
			}
			if boolVal(cloned.HTTPRedirect) != boolVal(source.HTTPRedirect) {
				return false
			}
			if boolVal(cloned.WebSocket) != boolVal(source.WebSocket) {
				return false
			}
			if boolVal(cloned.Compression) != boolVal(source.Compression) {
				return false
			}
			if boolVal(cloned.CorsEnabled) != boolVal(source.CorsEnabled) {
				return false
			}
			if boolVal(cloned.SecurityHeaders) != boolVal(source.SecurityHeaders) {
				return false
			}
			if cloned.CorsOrigins != source.CorsOrigins {
				return false
			}
			if cloned.CorsMethods != source.CorsMethods {
				return false
			}
			if cloned.CorsHeaders != source.CorsHeaders {
				return false
			}
			if cloned.ErrorPagePath != source.ErrorPagePath {
				return false
			}
			if cloned.CustomDirectives != source.CustomDirectives {
				return false
			}
			if cloned.RedirectURL != source.RedirectURL {
				return false
			}
			if cloned.RedirectCode != source.RedirectCode {
				return false
			}
			if cloned.TLSMode != source.TLSMode {
				return false
			}
			if cloned.CacheTTL != source.CacheTTL {
				return false
			}

			// Verify sub-table record counts
			if len(cloned.Upstreams) != len(source.Upstreams) {
				return false
			}
			if len(cloned.CustomHeaders) != len(source.CustomHeaders) {
				return false
			}
			if len(cloned.AccessRules) != len(source.AccessRules) {
				return false
			}
			if len(cloned.BasicAuths) != len(source.BasicAuths) {
				return false
			}
			if len(cloned.Routes) != len(source.Routes) {
				return false
			}

			// Verify sub-table content (except ID and HostID)
			for i, u := range cloned.Upstreams {
				if u.Address != source.Upstreams[i].Address || u.Weight != source.Upstreams[i].Weight {
					return false
				}
			}
			for i, h := range cloned.CustomHeaders {
				if h.Name != source.CustomHeaders[i].Name || h.Value != source.CustomHeaders[i].Value ||
					h.Direction != source.CustomHeaders[i].Direction || h.Operation != source.CustomHeaders[i].Operation {
					return false
				}
			}
			for i, a := range cloned.AccessRules {
				if a.RuleType != source.AccessRules[i].RuleType || a.IPRange != source.AccessRules[i].IPRange {
					return false
				}
			}
			for i, ba := range cloned.BasicAuths {
				if ba.Username != source.BasicAuths[i].Username {
					return false
				}
			}
			for i, r := range cloned.Routes {
				if r.Path != source.Routes[i].Path || r.SortOrder != source.Routes[i].SortOrder {
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 3),  // numUpstreams (min 1 for proxy hosts)
		gen.IntRange(0, 3),  // numHeaders
		gen.IntRange(0, 3),  // numAccessRules
		gen.IntRange(0, 2),  // numBasicAuths
		gen.IntRange(0, 3),  // numRoutes
		gen.IntRange(1, 99999), // domainSuffix for uniqueness
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 2: 克隆产生独立记录 — For any clone operation, the new
// Host ID must differ from source, and all sub-table records' host_id must point to the new Host ID.
// **Validates: Requirements 1.4**
func TestProperty2_CloneProducesIndependentRecords(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("clone produces independent records", prop.ForAll(
		func(numUpstreams, numHeaders, numAccessRules, numBasicAuths, numRoutes int, domainSuffix int) bool {
			db := setupTestDB(t)
			svc := setupTestHostService(t, db)

			sourceDomain := fmt.Sprintf("source-%d.example.com", domainSuffix)
			cloneDomain := fmt.Sprintf("clone-%d.example.com", domainSuffix)

			source := createTestHost(t, svc, sourceDomain, numUpstreams, numHeaders, numAccessRules, numBasicAuths, numRoutes)

			cloned, err := svc.CloneHost(source.ID, cloneDomain)
			if err != nil {
				t.Logf("CloneHost failed: %v", err)
				return false
			}

			// New Host ID must differ from source
			if cloned.ID == source.ID {
				return false
			}

			// All sub-table records' host_id must point to the new Host ID
			for _, u := range cloned.Upstreams {
				if u.HostID != cloned.ID {
					return false
				}
				// Must also have its own unique ID (not same as source)
				if u.ID == 0 {
					return false
				}
			}
			for _, h := range cloned.CustomHeaders {
				if h.HostID != cloned.ID {
					return false
				}
			}
			for _, a := range cloned.AccessRules {
				if a.HostID != cloned.ID {
					return false
				}
			}
			for _, ba := range cloned.BasicAuths {
				if ba.HostID != cloned.ID {
					return false
				}
			}
			for _, r := range cloned.Routes {
				if r.HostID != cloned.ID {
					return false
				}
			}

			// Verify source sub-table records are unchanged
			sourceRefresh, err := svc.Get(source.ID)
			if err != nil {
				return false
			}
			if len(sourceRefresh.Upstreams) != numUpstreams {
				return false
			}
			for _, u := range sourceRefresh.Upstreams {
				if u.HostID != source.ID {
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 3),  // numUpstreams (min 1 for proxy hosts)
		gen.IntRange(0, 3),
		gen.IntRange(0, 3),
		gen.IntRange(0, 2),
		gen.IntRange(0, 3),
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}
