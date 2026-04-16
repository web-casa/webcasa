package service

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// HostService handles business logic for proxy hosts
type HostService struct {
	db       *gorm.DB
	caddyMgr *caddy.Manager
	cfg      *config.Config
}

// NewHostService creates a new HostService
func NewHostService(db *gorm.DB, caddyMgr *caddy.Manager, cfg *config.Config) *HostService {
	return &HostService{db: db, caddyMgr: caddyMgr, cfg: cfg}
}

// HostListFilter holds optional filter parameters for listing hosts
type HostListFilter struct {
	GroupID *uint
	TagID   *uint
}

// List returns all hosts with their associations, optionally filtered by group_id and/or tag_id
func (s *HostService) List(filters ...HostListFilter) ([]model.Host, error) {
	var hosts []model.Host
	query := s.db.Preload("Upstreams").Preload("CustomHeaders").Preload("AccessRules").Preload("Routes").Preload("BasicAuths").
		Preload("Group").Preload("Tags")

	var filter HostListFilter
	if len(filters) > 0 {
		filter = filters[0]
	}

	if filter.GroupID != nil {
		query = query.Where("hosts.group_id = ?", *filter.GroupID)
	}

	if filter.TagID != nil {
		query = query.Joins("JOIN host_tags ON host_tags.host_id = hosts.id").
			Where("host_tags.tag_id = ?", *filter.TagID)
	}

	err := query.Order("hosts.id ASC").Find(&hosts).Error
	return hosts, err
}

// Get returns a single host by ID
func (s *HostService) Get(id uint) (*model.Host, error) {
	var host model.Host
	err := s.db.Preload("Upstreams").Preload("CustomHeaders").Preload("AccessRules").Preload("Routes").Preload("BasicAuths").
		Preload("Group").Preload("Tags").
		First(&host, id).Error
	if err != nil {
		return nil, err
	}
	return &host, nil
}

// Create creates a new host and applies the configuration
func (s *HostService) Create(req *model.HostCreateRequest) (*model.Host, error) {
	// Validate domain for Caddyfile safety
	if err := caddy.ValidateDomain(req.Domain); err != nil {
		return nil, fmt.Errorf("invalid domain: %w", err)
	}

	// Validate upstreams
	for _, u := range req.Upstreams {
		if err := caddy.ValidateUpstream(u.Address); err != nil {
			return nil, fmt.Errorf("invalid upstream '%s': %w", u.Address, err)
		}
	}

	// Validate access rule IPs
	for _, r := range req.AccessRules {
		if err := caddy.ValidateIPRange(r.IPRange); err != nil {
			return nil, fmt.Errorf("invalid access rule IP: %w", err)
		}
	}

	// Validate custom directives
	if err := caddy.SanitizeCustomDirectives(req.CustomDirectives); err != nil {
		return nil, fmt.Errorf("invalid custom directives: %w", err)
	}

	// Validate all string fields that get embedded in Caddyfile
	for label, val := range map[string]string{
		"redirect_url":   req.RedirectURL,
		"root_path":      req.RootPath,
		"error_page_path": req.ErrorPagePath,
		"php_fastcgi":    req.PHPFastCGI,
		"index_files":    req.IndexFiles,
		"cors_origins":   req.CorsOrigins,
		"cors_methods":   req.CorsMethods,
		"cors_headers":   req.CorsHeaders,
	} {
		if err := caddy.ValidateCaddyValue(label, val); err != nil {
			return nil, err
		}
	}
	for _, h := range req.CustomHeaders {
		if err := caddy.ValidateCaddyValue("header name", h.Name); err != nil {
			return nil, err
		}
		if err := caddy.ValidateCaddyValue("header value", h.Value); err != nil {
			return nil, err
		}
	}

	var count int64
	s.db.Model(&model.Host{}).Where("domain = ?", req.Domain).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("domain '%s' already exists", req.Domain)
	}

	// Optional DNS pre-validation: warn if domain doesn't resolve to this server.
	// Runs in a goroutine to avoid blocking the request on slow DNS lookups.
	var dnsVerify model.Setting
	if s.db.Where("key = ?", "dns_verify_on_create").First(&dnsVerify).Error == nil && dnsVerify.Value == "true" {
		go func(domain string) {
			dnsChecker := NewDnsCheckService(s.db)
			dnsResult, _ := dnsChecker.Check(domain)
			if dnsResult != nil && dnsResult.Status == "mismatched" {
				log.Printf("DNS warning: domain '%s' does not resolve to this server (records: %v)", domain, dnsResult.ARecords)
			}
		}(req.Domain)
	}

	hostType := stringOrDefault(req.HostType, "proxy")
	if hostType != "proxy" && hostType != "redirect" && hostType != "static" && hostType != "php" {
		return nil, fmt.Errorf("invalid host_type: %s (must be 'proxy', 'redirect', 'static', or 'php')", hostType)
	}

	// Validate based on type
	switch hostType {
	case "redirect":
		if req.RedirectURL == "" {
			return nil, fmt.Errorf("redirect_url is required for redirect hosts")
		}
	case "proxy":
		if len(req.Upstreams) == 0 {
			return nil, fmt.Errorf("at least one upstream is required for proxy hosts")
		}
	case "static":
		if req.RootPath == "" {
			return nil, fmt.Errorf("root_path is required for static hosts")
		}
	case "php":
		if req.RootPath == "" {
			return nil, fmt.Errorf("root_path is required for PHP hosts")
		}
	}

	host := &model.Host{
		Domain:           req.Domain,
		HostType:         hostType,
		Enabled:          boolPtr(boolOrDefault(req.Enabled, true)),
		TLSEnabled:       boolPtr(boolOrDefault(req.TLSEnabled, true)),
		HTTPRedirect:     boolPtr(boolOrDefault(req.HTTPRedirect, true)),
		WebSocket:        boolPtr(boolOrDefault(req.WebSocket, false)),
		RedirectURL:      req.RedirectURL,
		RedirectCode:     intOrDefault(req.RedirectCode, 301),
		Compression:      boolPtr(boolOrDefault(req.Compression, false)),
		CacheEnabled:     boolPtr(boolOrDefault(req.CacheEnabled, false)),
		CacheTTL:         intOrDefault(req.CacheTTL, 300),
		CorsEnabled:      boolPtr(boolOrDefault(req.CorsEnabled, false)),
		CorsOrigins:      req.CorsOrigins,
		CorsMethods:      req.CorsMethods,
		CorsHeaders:      req.CorsHeaders,
		SecurityHeaders:  boolPtr(boolOrDefault(req.SecurityHeaders, false)),
		ErrorPagePath:    req.ErrorPagePath,
		RootPath:         req.RootPath,
		DirectoryBrowse:  boolPtr(boolOrDefault(req.DirectoryBrowse, false)),
		PHPFastCGI:       req.PHPFastCGI,
		IndexFiles:       req.IndexFiles,
		TLSMode:          stringOrDefault(req.TLSMode, "auto"),
		DnsProviderID:    uintPtrOrNil(req.DnsProviderID),
		CustomDirectives: req.CustomDirectives,
		GroupID:          uintPtrOrNil(req.GroupID),
	}

	for i, u := range req.Upstreams {
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

	for i, h := range req.CustomHeaders {
		host.CustomHeaders = append(host.CustomHeaders, model.CustomHeader{
			Direction: stringOrDefault(h.Direction, "response"),
			Operation: stringOrDefault(h.Operation, "set"),
			Name:      h.Name,
			Value:     h.Value,
			SortOrder: i,
		})
	}

	for i, a := range req.AccessRules {
		host.AccessRules = append(host.AccessRules, model.AccessRule{
			RuleType:  a.RuleType,
			IPRange:   a.IPRange,
			SortOrder: i,
		})
	}

	// Hash basic auth passwords
	for _, ba := range req.BasicAuths {
		if err := caddy.ValidateCaddyValue("basicauth username", ba.Username); err != nil {
			return nil, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(ba.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password for user '%s': %w", ba.Username, err)
		}
		host.BasicAuths = append(host.BasicAuths, model.BasicAuth{
			Username:     ba.Username,
			PasswordHash: string(hash),
		})
	}

	if err := s.db.Create(host).Error; err != nil {
		return nil, fmt.Errorf("failed to create host: %w", err)
	}

	// Sync tag associations
	if len(req.TagIDs) > 0 {
		for _, tagID := range req.TagIDs {
			s.db.Create(&model.HostTag{HostID: host.ID, TagID: tagID})
		}
	}

	if err := s.ApplyConfig(); err != nil {
		return nil, fmt.Errorf("host created but Caddy config failed: %w", err)
	}

	return s.Get(host.ID)
}

// Update modifies an existing host
func (s *HostService) Update(id uint, req *model.HostCreateRequest) (*model.Host, error) {
	host, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	// Validate domain for Caddyfile safety
	if err := caddy.ValidateDomain(req.Domain); err != nil {
		return nil, fmt.Errorf("invalid domain: %w", err)
	}

	// Validate upstreams
	for _, u := range req.Upstreams {
		if err := caddy.ValidateUpstream(u.Address); err != nil {
			return nil, fmt.Errorf("invalid upstream '%s': %w", u.Address, err)
		}
	}

	// Validate access rule IPs
	for _, r := range req.AccessRules {
		if err := caddy.ValidateIPRange(r.IPRange); err != nil {
			return nil, fmt.Errorf("invalid access rule IP: %w", err)
		}
	}

	// Validate custom directives
	if err := caddy.SanitizeCustomDirectives(req.CustomDirectives); err != nil {
		return nil, fmt.Errorf("invalid custom directives: %w", err)
	}

	// Validate all string fields that get embedded in Caddyfile
	for label, val := range map[string]string{
		"redirect_url":   req.RedirectURL,
		"root_path":      req.RootPath,
		"error_page_path": req.ErrorPagePath,
		"php_fastcgi":    req.PHPFastCGI,
		"index_files":    req.IndexFiles,
		"cors_origins":   req.CorsOrigins,
		"cors_methods":   req.CorsMethods,
		"cors_headers":   req.CorsHeaders,
	} {
		if err := caddy.ValidateCaddyValue(label, val); err != nil {
			return nil, err
		}
	}
	for _, h := range req.CustomHeaders {
		if err := caddy.ValidateCaddyValue("header name", h.Name); err != nil {
			return nil, err
		}
		if err := caddy.ValidateCaddyValue("header value", h.Value); err != nil {
			return nil, err
		}
	}

	var count int64
	s.db.Model(&model.Host{}).Where("domain = ? AND id != ?", req.Domain, id).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("domain '%s' already exists", req.Domain)
	}

	hostType := stringOrDefault(req.HostType, host.HostType)
	if hostType != "proxy" && hostType != "redirect" && hostType != "static" && hostType != "php" {
		return nil, fmt.Errorf("invalid host_type: %s (must be 'proxy', 'redirect', 'static', or 'php')", hostType)
	}

	// Validate required fields based on host type (same rules as Create).
	switch hostType {
	case "redirect":
		if req.RedirectURL == "" && host.RedirectURL == "" {
			return nil, fmt.Errorf("redirect_url is required for redirect hosts")
		}
	case "proxy":
		if len(req.Upstreams) == 0 {
			return nil, fmt.Errorf("at least one upstream is required for proxy hosts")
		}
	case "static":
		effectiveRoot := req.RootPath
		if effectiveRoot == "" {
			effectiveRoot = host.RootPath
		}
		if effectiveRoot == "" {
			return nil, fmt.Errorf("root_path is required for static hosts")
		}
	case "php":
		effectiveRoot := req.RootPath
		if effectiveRoot == "" {
			effectiveRoot = host.RootPath
		}
		if effectiveRoot == "" {
			return nil, fmt.Errorf("root_path is required for PHP hosts")
		}
	}

	host.Domain = req.Domain
	host.HostType = hostType
	host.Enabled = boolPtr(boolOrDefault(req.Enabled, boolVal(host.Enabled)))
	host.TLSEnabled = boolPtr(boolOrDefault(req.TLSEnabled, boolVal(host.TLSEnabled)))
	host.HTTPRedirect = boolPtr(boolOrDefault(req.HTTPRedirect, boolVal(host.HTTPRedirect)))
	host.WebSocket = boolPtr(boolOrDefault(req.WebSocket, boolVal(host.WebSocket)))
	if req.RedirectURL != "" {
		host.RedirectURL = req.RedirectURL
	}
	if req.RedirectCode > 0 {
		host.RedirectCode = req.RedirectCode
	}
	host.CustomDirectives = req.CustomDirectives
	host.Compression = boolPtr(boolOrDefault(req.Compression, boolVal(host.Compression)))
	host.CacheEnabled = boolPtr(boolOrDefault(req.CacheEnabled, boolVal(host.CacheEnabled)))
	if req.CacheTTL > 0 {
		host.CacheTTL = req.CacheTTL
	}
	host.CorsEnabled = boolPtr(boolOrDefault(req.CorsEnabled, boolVal(host.CorsEnabled)))
	host.CorsOrigins = req.CorsOrigins
	host.CorsMethods = req.CorsMethods
	host.CorsHeaders = req.CorsHeaders
	host.SecurityHeaders = boolPtr(boolOrDefault(req.SecurityHeaders, boolVal(host.SecurityHeaders)))
	host.ErrorPagePath = req.ErrorPagePath
	host.RootPath = req.RootPath
	host.DirectoryBrowse = boolPtr(boolOrDefault(req.DirectoryBrowse, boolVal(host.DirectoryBrowse)))
	host.PHPFastCGI = req.PHPFastCGI
	host.IndexFiles = req.IndexFiles
	if req.TLSMode != "" {
		host.TLSMode = req.TLSMode
	}
	host.DnsProviderID = uintPtrOrNil(req.DnsProviderID)
	host.GroupID = uintPtrOrNil(req.GroupID)

	// Save old upstream IDs before deletion (for route remapping).
	var oldUpstreams []model.Upstream
	s.db.Where("host_id = ?", id).Order("sort_order ASC").Find(&oldUpstreams)

	// Replace associations
	s.db.Where("host_id = ?", id).Delete(&model.Upstream{})
	s.db.Where("host_id = ?", id).Delete(&model.CustomHeader{})
	s.db.Where("host_id = ?", id).Delete(&model.AccessRule{})
	s.db.Where("host_id = ?", id).Delete(&model.BasicAuth{})

	host.Upstreams = nil
	host.CustomHeaders = nil
	host.AccessRules = nil
	host.BasicAuths = nil

	for i, u := range req.Upstreams {
		weight := u.Weight
		if weight < 1 {
			weight = 1
		}
		host.Upstreams = append(host.Upstreams, model.Upstream{
			HostID:    id,
			Address:   u.Address,
			Weight:    weight,
			SortOrder: i,
		})
	}

	for i, h := range req.CustomHeaders {
		host.CustomHeaders = append(host.CustomHeaders, model.CustomHeader{
			HostID:    id,
			Direction: stringOrDefault(h.Direction, "response"),
			Operation: stringOrDefault(h.Operation, "set"),
			Name:      h.Name,
			Value:     h.Value,
			SortOrder: i,
		})
	}

	for i, a := range req.AccessRules {
		host.AccessRules = append(host.AccessRules, model.AccessRule{
			HostID:    id,
			RuleType:  a.RuleType,
			IPRange:   a.IPRange,
			SortOrder: i,
		})
	}

	// Hash basic auth passwords
	for _, ba := range req.BasicAuths {
		if err := caddy.ValidateCaddyValue("basicauth username", ba.Username); err != nil {
			return nil, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(ba.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password for user '%s': %w", ba.Username, err)
		}
		host.BasicAuths = append(host.BasicAuths, model.BasicAuth{
			HostID:       id,
			Username:     ba.Username,
			PasswordHash: string(hash),
		})
	}

	if err := s.db.Save(host).Error; err != nil {
		return nil, fmt.Errorf("failed to update host: %w", err)
	}

	// Explicitly clear group_id if nil (GORM Save ignores nil pointer fields)
	if req.GroupID == nil {
		s.db.Model(&model.Host{}).Where("id = ?", id).Update("group_id", nil)
	}

	for i := range host.Upstreams {
		s.db.Create(&host.Upstreams[i])
	}

	// Remap route UpstreamIDs: old upstream at sort_order N → new upstream at sort_order N.
	if len(oldUpstreams) > 0 && len(host.Upstreams) > 0 {
		oldIDMap := make(map[uint]int) // old upstream ID → sort_order index
		for i, u := range oldUpstreams {
			oldIDMap[u.ID] = i
		}
		var routes []model.Route
		s.db.Where("host_id = ?", id).Find(&routes)
		for _, r := range routes {
			if r.UpstreamID != nil {
				if idx, ok := oldIDMap[*r.UpstreamID]; ok && idx < len(host.Upstreams) {
					newID := host.Upstreams[idx].ID
					s.db.Model(&r).Update("upstream_id", newID)
				}
			}
		}
	}

	for i := range host.CustomHeaders {
		s.db.Create(&host.CustomHeaders[i])
	}
	for i := range host.AccessRules {
		s.db.Create(&host.AccessRules[i])
	}
	for i := range host.BasicAuths {
		s.db.Create(&host.BasicAuths[i])
	}

	// Sync tag associations: replace all
	s.db.Where("host_id = ?", id).Delete(&model.HostTag{})
	for _, tagID := range req.TagIDs {
		s.db.Create(&model.HostTag{HostID: id, TagID: tagID})
	}

	if err := s.ApplyConfig(); err != nil {
		return nil, fmt.Errorf("host updated but Caddy config failed: %w", err)
	}

	return s.Get(id)
}

// Delete removes a host
func (s *HostService) Delete(id uint) error {
	result := s.db.Delete(&model.Host{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("host not found")
	}

	if err := s.ApplyConfig(); err != nil {
		return fmt.Errorf("host deleted but Caddy config failed: %w", err)
	}
	return nil
}

// Toggle enables/disables a host
func (s *HostService) Toggle(id uint) (*model.Host, error) {
	host, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	newVal := !boolVal(host.Enabled)
	host.Enabled = &newVal
	if err := s.db.Save(host).Error; err != nil {
		return nil, err
	}

	if err := s.ApplyConfig(); err != nil {
		return nil, fmt.Errorf("host toggled but Caddy config failed: %w", err)
	}
	// Return a fresh read so all associations and *bool fields are properly loaded.
	return s.Get(id)
}

// ApplyConfig regenerates the Caddyfile and reloads Caddy
func (s *HostService) ApplyConfig() error {
	hosts, err := s.List()
	if err != nil {
		return fmt.Errorf("failed to list hosts: %w", err)
	}

	// Preload DNS providers for TLS rendering
	var providers []model.DnsProvider
	s.db.Find(&providers)
	dnsMap := make(map[uint]model.DnsProvider, len(providers))
	for _, p := range providers {
		dnsMap[p.ID] = p
	}

	// Resolve CertificateID → CustomCertPath/CustomKeyPath
	var certs []model.Certificate
	s.db.Find(&certs)
	certMap := make(map[uint]model.Certificate, len(certs))
	for _, c := range certs {
		certMap[c.ID] = c
	}
	for i := range hosts {
		if hosts[i].CertificateID != nil && *hosts[i].CertificateID > 0 {
			if cert, ok := certMap[*hosts[i].CertificateID]; ok {
				hosts[i].CustomCertPath = cert.CertPath
				hosts[i].CustomKeyPath = cert.KeyPath
			}
		}
	}

	content := caddy.RenderCaddyfile(hosts, s.cfg, dnsMap)

	// Read old Caddyfile for rollback if reload fails.
	oldContent, _ := s.caddyMgr.GetCaddyfileContent()

	if err := s.caddyMgr.WriteCaddyfile(content); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}

	// Check auto_reload setting
	var setting model.Setting
	autoReload := true // default to true
	if s.db.Where("key = ?", "auto_reload").First(&setting).Error == nil {
		autoReload = setting.Value == "true"
	}

	if autoReload {
		if s.caddyMgr.IsRunning() {
			if err := s.caddyMgr.RequestReload(); err != nil {
				// Rollback: restore old Caddyfile so Caddy stays on the last known-good config.
				if oldContent != "" {
					if wErr := s.caddyMgr.WriteCaddyfile(oldContent); wErr != nil {
						log.Printf("CRITICAL: failed to rollback Caddyfile: %v", wErr)
					}
				}
				return fmt.Errorf("failed to reload Caddy (config rolled back): %w", err)
			}
			log.Println("Caddy reloaded after config change")
		} else {
			if err := s.caddyMgr.Start(); err != nil {
				log.Printf("⚠️  Failed to auto-start Caddy: %v", err)
				// Don't return error — config was written successfully
			} else {
				log.Println("Caddy auto-started after config change")
			}
		}
	}

	return nil
}

// UpdateCertPaths updates the custom certificate paths for a host
func (s *HostService) UpdateCertPaths(id uint, certPath, keyPath string) error {
	host, err := s.Get(id)
	if err != nil {
		return err
	}
	host.CustomCertPath = certPath
	host.CustomKeyPath = keyPath
	if err := s.db.Save(host).Error; err != nil {
		return err
	}
	return s.ApplyConfig()
}

// ExportAll returns all hosts for export
func (s *HostService) ExportAll() (*model.ExportData, error) {
	hosts, err := s.List()
	if err != nil {
		return nil, err
	}
	return &model.ExportData{
		Version:    "1.0",
		ExportedAt: time.Now().Format(time.RFC3339),
		Hosts:      hosts,
	}, nil
}

// ImportAll replaces all hosts with imported data
func (s *HostService) ImportAll(data *model.ExportData) error {
	// Validate ALL imported hosts before deleting anything.
	for _, host := range data.Hosts {
		if err := caddy.ValidateDomain(host.Domain); err != nil {
			return fmt.Errorf("import validation failed for '%s': %w", host.Domain, err)
		}
		for _, u := range host.Upstreams {
			if err := caddy.ValidateUpstream(u.Address); err != nil {
				return fmt.Errorf("import validation failed for upstream '%s' on '%s': %w", u.Address, host.Domain, err)
			}
		}
		for _, r := range host.AccessRules {
			if err := caddy.ValidateIPRange(r.IPRange); err != nil {
				return fmt.Errorf("import validation failed for access rule on '%s': %w", host.Domain, err)
			}
		}
		if err := caddy.SanitizeCustomDirectives(host.CustomDirectives); err != nil {
			return fmt.Errorf("import validation failed for custom directives on '%s': %w", host.Domain, err)
		}
		// Validate all Caddyfile-embedded string fields.
		for label, val := range map[string]string{
			"redirect_url": host.RedirectURL, "root_path": host.RootPath,
			"error_page_path": host.ErrorPagePath, "php_fastcgi": host.PHPFastCGI,
			"index_files": host.IndexFiles, "cors_origins": host.CorsOrigins,
			"cors_methods": host.CorsMethods, "cors_headers": host.CorsHeaders,
		} {
			if err := caddy.ValidateCaddyValue(label, val); err != nil {
				return fmt.Errorf("import validation failed for %s on '%s': %w", label, host.Domain, err)
			}
		}
		for _, h := range host.CustomHeaders {
			if err := caddy.ValidateCaddyValue("header name", h.Name); err != nil {
				return fmt.Errorf("import validation failed for header on '%s': %w", host.Domain, err)
			}
			if err := caddy.ValidateCaddyValue("header value", h.Value); err != nil {
				return fmt.Errorf("import validation failed for header value on '%s': %w", host.Domain, err)
			}
		}
		for _, r := range host.Routes {
			if err := caddy.ValidateCaddyValue("route path", r.Path); err != nil {
				return fmt.Errorf("import validation failed for route on '%s': %w", host.Domain, err)
			}
		}
	}

	// Wrap the entire delete + insert in a transaction so a mid-import
	// failure doesn't leave the system with no hosts at all.
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		tx.Exec("DELETE FROM host_tags")
		tx.Exec("DELETE FROM basic_auths")
		tx.Exec("DELETE FROM access_rules")
		tx.Exec("DELETE FROM custom_headers")
		tx.Exec("DELETE FROM routes")
		tx.Exec("DELETE FROM upstreams")
		tx.Exec("DELETE FROM hosts")

		for _, host := range data.Hosts {
			// Save original upstream IDs for route remapping.
			origUpstreams := make([]model.Upstream, len(host.Upstreams))
			copy(origUpstreams, host.Upstreams)

			// Detach routes and tags — we'll insert them separately.
			routes := host.Routes
			host.Routes = nil
			tags := host.Tags
			host.Tags = nil

			host.ID = 0
			for i := range host.Upstreams {
				host.Upstreams[i].ID = 0
				host.Upstreams[i].HostID = 0
			}
			for i := range host.CustomHeaders {
				host.CustomHeaders[i].ID = 0
				host.CustomHeaders[i].HostID = 0
			}
			for i := range host.AccessRules {
				host.AccessRules[i].ID = 0
				host.AccessRules[i].HostID = 0
			}
			for i := range host.BasicAuths {
				host.BasicAuths[i].ID = 0
				host.BasicAuths[i].HostID = 0
			}

			if err := tx.Create(&host).Error; err != nil {
				return fmt.Errorf("failed to import host %s: %w", host.Domain, err)
			}

			// Rebuild tag associations: look up each tag by name, create if missing.
			for _, tag := range tags {
				var existing model.Tag
				if err := tx.Where("name = ?", tag.Name).First(&existing).Error; err != nil {
					// Tag doesn't exist — create it.
					existing = model.Tag{Name: tag.Name, Color: tag.Color}
					if err := tx.Create(&existing).Error; err != nil {
						return fmt.Errorf("failed to create tag %s: %w", tag.Name, err)
					}
				}
				if err := tx.Exec("INSERT INTO host_tags (host_id, tag_id) VALUES (?, ?)", host.ID, existing.ID).Error; err != nil {
					return fmt.Errorf("failed to associate tag %s with host %s: %w", tag.Name, host.Domain, err)
				}
			}

			// Build old→new upstream ID mapping.
			upstreamIDMap := make(map[uint]uint)
			for i, orig := range origUpstreams {
				if i < len(host.Upstreams) {
					upstreamIDMap[orig.ID] = host.Upstreams[i].ID
				}
			}

			// Insert routes with remapped UpstreamIDs.
			for _, r := range routes {
				r.ID = 0
				r.HostID = host.ID
				if r.UpstreamID != nil {
					if newID, ok := upstreamIDMap[*r.UpstreamID]; ok {
						r.UpstreamID = &newID
					} else {
						r.UpstreamID = nil // orphan reference — clear it
					}
				}
				if err := tx.Create(&r).Error; err != nil {
					return fmt.Errorf("failed to import route for %s: %w", host.Domain, err)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}

	return s.ApplyConfig()
}
// CloneHost creates a deep copy of an existing host with a new domain.
// It copies all main table fields (except ID, Domain, CreatedAt, UpdatedAt)
// and all sub-table records (upstreams, custom_headers, access_rules, basic_auths, routes).
func (s *HostService) CloneHost(sourceID uint, newDomain string) (*model.Host, error) {
	// Validate domain for Caddyfile safety.
	if err := caddy.ValidateDomain(newDomain); err != nil {
		return nil, fmt.Errorf("invalid domain: %w", err)
	}

	// Domain uniqueness check
	var count int64
	s.db.Model(&model.Host{}).Where("domain = ?", newDomain).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("error.domain_exists")
	}

	// Fetch source host with all associations
	source, err := s.Get(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error.host_not_found")
	}

	var newHost *model.Host

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		// Deep copy main table fields
		newHost = &model.Host{
			Domain:           newDomain,
			HostType:         source.HostType,
			Enabled:          copyBoolPtr(source.Enabled),
			TLSEnabled:       copyBoolPtr(source.TLSEnabled),
			HTTPRedirect:     copyBoolPtr(source.HTTPRedirect),
			WebSocket:        copyBoolPtr(source.WebSocket),
			RedirectURL:      source.RedirectURL,
			RedirectCode:     source.RedirectCode,
			CustomCertPath:   source.CustomCertPath,
			CustomKeyPath:    source.CustomKeyPath,
			TLSMode:          source.TLSMode,
			DnsProviderID:    source.DnsProviderID,
			CertificateID:    source.CertificateID,
			Compression:      copyBoolPtr(source.Compression),
			CacheEnabled:     copyBoolPtr(source.CacheEnabled),
			CacheTTL:         source.CacheTTL,
			CorsEnabled:      copyBoolPtr(source.CorsEnabled),
			CorsOrigins:      source.CorsOrigins,
			CorsMethods:      source.CorsMethods,
			CorsHeaders:      source.CorsHeaders,
			SecurityHeaders:  copyBoolPtr(source.SecurityHeaders),
			ErrorPagePath:    source.ErrorPagePath,
			CustomDirectives: source.CustomDirectives,
			RootPath:         source.RootPath,
			DirectoryBrowse:  copyBoolPtr(source.DirectoryBrowse),
			PHPFastCGI:       source.PHPFastCGI,
			IndexFiles:       source.IndexFiles,
			GroupID:          source.GroupID,
		}

		// Deep copy upstreams first (routes reference them by ID).
		for _, u := range source.Upstreams {
			newHost.Upstreams = append(newHost.Upstreams, model.Upstream{
				Address:   u.Address,
				Weight:    u.Weight,
				SortOrder: u.SortOrder,
			})
		}

		for _, h := range source.CustomHeaders {
			newHost.CustomHeaders = append(newHost.CustomHeaders, model.CustomHeader{
				Direction: h.Direction,
				Operation: h.Operation,
				Name:      h.Name,
				Value:     h.Value,
				SortOrder: h.SortOrder,
			})
		}

		for _, a := range source.AccessRules {
			newHost.AccessRules = append(newHost.AccessRules, model.AccessRule{
				RuleType:  a.RuleType,
				IPRange:   a.IPRange,
				SortOrder: a.SortOrder,
			})
		}

		for _, ba := range source.BasicAuths {
			newHost.BasicAuths = append(newHost.BasicAuths, model.BasicAuth{
				Username:     ba.Username,
				PasswordHash: ba.PasswordHash,
			})
		}

		// Create host + upstreams first so upstreams get new IDs.
		if err := tx.Create(newHost).Error; err != nil {
			return fmt.Errorf("failed to create cloned host: %w", err)
		}

		// Build old→new upstream ID mapping for route remapping.
		upstreamIDMap := make(map[uint]uint) // source upstream ID → cloned upstream ID
		for i, srcUp := range source.Upstreams {
			if i < len(newHost.Upstreams) {
				upstreamIDMap[srcUp.ID] = newHost.Upstreams[i].ID
			}
		}

		// Now create routes with remapped UpstreamIDs.
		for _, r := range source.Routes {
			newRoute := model.Route{
				HostID:    newHost.ID,
				Path:      r.Path,
				SortOrder: r.SortOrder,
			}
			if r.UpstreamID != nil {
				if newID, ok := upstreamIDMap[*r.UpstreamID]; ok {
					newRoute.UpstreamID = &newID
				}
				// If old ID not found in map, leave UpstreamID nil (orphan route).
			}
			if err := tx.Create(&newRoute).Error; err != nil {
				return fmt.Errorf("failed to create cloned route: %w", err)
			}
		}

		// Copy tag associations.
		for _, tag := range source.Tags {
			if err := tx.Create(&model.HostTag{HostID: newHost.ID, TagID: tag.ID}).Error; err != nil {
				return fmt.Errorf("failed to clone tag: %w", err)
			}
		}

		return nil
	})

	if txErr != nil {
		return nil, txErr
	}

	// Apply config after successful clone
	if err := s.ApplyConfig(); err != nil {
		log.Printf("Warning: failed to apply config after clone: %v", err)
	}

	// Return the full host with associations
	return s.Get(newHost.ID)
}

func boolOrDefault(ptr *bool, defaultVal bool) bool {
	if ptr != nil {
		return *ptr
	}
	return defaultVal
}

func boolPtr(v bool) *bool {
	return &v
}

func boolVal(ptr *bool) bool {
	if ptr != nil {
		return *ptr
	}
	return false
}

func intOrDefault(v, defaultVal int) int {
	if v > 0 {
		return v
	}
	return defaultVal
}

func stringOrDefault(s, defaultVal string) string {
	if s != "" {
		return s
	}
	return defaultVal
}
func copyBoolPtr(ptr *bool) *bool {
	if ptr == nil {
		return nil
	}
	v := *ptr
	return &v
}

// GenerateWildcardDomain creates a subdomain under the configured wildcard domain.
// Returns empty string if wildcard_domain is not configured.
// Sanitizes appName to be a valid DNS label (lowercase, alphanumeric + hyphens).
func (s *HostService) GenerateWildcardDomain(appName string) string {
	var setting model.Setting
	if s.db.Where("key = ?", "wildcard_domain").First(&setting).Error != nil || setting.Value == "" {
		return ""
	}
	// Sanitize appName as DNS label: lowercase, only [a-z0-9-], max 63 chars.
	label := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32 // lowercase
		}
		return '-'
	}, appName)
	if len(label) > 63 {
		label = label[:63]
	}
	label = strings.Trim(label, "-") // trim after truncation to avoid trailing hyphens
	if label == "" {
		return ""
	}
	return label + "." + setting.Value
}

// uintPtrOrNil returns nil if the pointer is nil or points to 0 (treat 0 as "no value").
func uintPtrOrNil(ptr *uint) *uint {
	if ptr == nil || *ptr == 0 {
		return nil
	}
	return ptr
}
