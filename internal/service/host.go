package service

import (
	"fmt"
	"log"
	"time"

	"github.com/caddypanel/caddypanel/internal/caddy"
	"github.com/caddypanel/caddypanel/internal/config"
	"github.com/caddypanel/caddypanel/internal/model"
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

// List returns all hosts with their associations
func (s *HostService) List() ([]model.Host, error) {
	var hosts []model.Host
	err := s.db.Preload("Upstreams").Preload("CustomHeaders").Preload("AccessRules").Preload("Routes").Preload("BasicAuths").
		Order("id ASC").Find(&hosts).Error
	return hosts, err
}

// Get returns a single host by ID
func (s *HostService) Get(id uint) (*model.Host, error) {
	var host model.Host
	err := s.db.Preload("Upstreams").Preload("CustomHeaders").Preload("AccessRules").Preload("Routes").Preload("BasicAuths").
		First(&host, id).Error
	if err != nil {
		return nil, err
	}
	return &host, nil
}

// Create creates a new host and applies the configuration
func (s *HostService) Create(req *model.HostCreateRequest) (*model.Host, error) {
	var count int64
	s.db.Model(&model.Host{}).Where("domain = ?", req.Domain).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("domain '%s' already exists", req.Domain)
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
		DnsProviderID:    req.DnsProviderID,
		CustomDirectives: req.CustomDirectives,
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

	if err := s.ApplyConfig(); err != nil {
		log.Printf("Warning: failed to apply config after create: %v", err)
	}

	return host, nil
}

// Update modifies an existing host
func (s *HostService) Update(id uint, req *model.HostCreateRequest) (*model.Host, error) {
	host, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	var count int64
	s.db.Model(&model.Host{}).Where("domain = ? AND id != ?", req.Domain, id).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("domain '%s' already exists", req.Domain)
	}

	hostType := stringOrDefault(req.HostType, host.HostType)
	if hostType == "redirect" && req.RedirectURL == "" && host.RedirectURL == "" {
		return nil, fmt.Errorf("redirect_url is required for redirect hosts")
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
	host.DnsProviderID = req.DnsProviderID

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

	for i := range host.Upstreams {
		s.db.Create(&host.Upstreams[i])
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

	if err := s.ApplyConfig(); err != nil {
		log.Printf("Warning: failed to apply config after update: %v", err)
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
		log.Printf("Warning: failed to apply config after delete: %v", err)
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
		log.Printf("Warning: failed to apply config after toggle: %v", err)
	}
	return host, nil
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
			if err := s.caddyMgr.Reload(); err != nil {
				return fmt.Errorf("failed to reload Caddy: %w", err)
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
	s.db.Exec("DELETE FROM basic_auths")
	s.db.Exec("DELETE FROM access_rules")
	s.db.Exec("DELETE FROM custom_headers")
	s.db.Exec("DELETE FROM routes")
	s.db.Exec("DELETE FROM upstreams")
	s.db.Exec("DELETE FROM hosts")

	for _, host := range data.Hosts {
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
		for i := range host.Routes {
			host.Routes[i].ID = 0
			host.Routes[i].HostID = 0
		}
		for i := range host.BasicAuths {
			host.BasicAuths[i].ID = 0
			host.BasicAuths[i].HostID = 0
		}
		if err := s.db.Create(&host).Error; err != nil {
			return fmt.Errorf("failed to import host %s: %w", host.Domain, err)
		}
	}

	return s.ApplyConfig()
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
