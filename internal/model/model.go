package model

import (
	"time"
)

// User represents a panel administrator
type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"uniqueIndex;not null;size:64" json:"username"`
	Password  string    `gorm:"not null" json:"-"` // bcrypt hash, never exposed in JSON
	Role      string    `gorm:"not null;size:16;default:admin" json:"role"` // "admin" or "viewer"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DnsProvider represents a DNS API provider for ACME DNS challenge
type DnsProvider struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null;size:64" json:"name"`              // display name
	Provider  string    `gorm:"not null;size:32" json:"provider"`          // "cloudflare", "alidns", "tencentcloud", "route53"
	Config    string    `gorm:"type:text;not null" json:"config"`          // JSON config (API tokens/keys)
	IsDefault *bool     `gorm:"default:false" json:"is_default"`           // default provider
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Setting stores panel configuration as key-value pairs
type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

// Certificate represents a managed SSL certificate
type Certificate struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null;size:128" json:"name"`    // display name (e.g. "example.com wildcard")
	Domains   string    `gorm:"type:text" json:"domains"`         // comma-separated domains from cert
	CertPath  string    `gorm:"type:text" json:"cert_path"`       // path to cert.pem
	KeyPath   string    `gorm:"type:text" json:"key_path"`        // path to key.pem
	ExpiresAt *time.Time `json:"expires_at"`                      // cert expiry (parsed from PEM)
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Host represents a reverse proxy or redirect host configuration
type Host struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Domain         string         `gorm:"not null;uniqueIndex;size:255" json:"domain"`
	HostType       string         `gorm:"not null;size:16;default:proxy" json:"host_type"` // "proxy", "redirect", "static", "php"
	Enabled        *bool          `gorm:"default:true" json:"enabled"`
	TLSEnabled     *bool          `gorm:"default:true" json:"tls_enabled"`
	HTTPRedirect   *bool          `gorm:"default:true" json:"http_redirect"`
	WebSocket      *bool          `gorm:"default:false" json:"websocket"`
	RedirectURL    string         `gorm:"size:1024" json:"redirect_url"`    // target URL for redirect hosts
	RedirectCode   int            `gorm:"default:301" json:"redirect_code"` // 301 (permanent) or 302 (temporary)
	CustomCertPath string         `gorm:"size:512" json:"custom_cert_path"` // path to custom TLS cert
	CustomKeyPath  string         `gorm:"size:512" json:"custom_key_path"`  // path to custom TLS key
	// Phase 4 batch 1: TLS mode and DNS provider
	TLSMode        string `gorm:"size:16;default:auto" json:"tls_mode"` // auto, dns, wildcard, custom, off
	DnsProviderID  *uint  `json:"dns_provider_id"`                      // FK to DnsProvider
	CertificateID  *uint  `json:"certificate_id"`                       // FK to Certificate
	// Phase 4 batch 2: per-host options
	Compression     *bool  `gorm:"default:false" json:"compression"`       // encode gzip zstd
	CacheEnabled    *bool  `gorm:"default:false" json:"cache_enabled"`     // response cache
	CacheTTL        int    `gorm:"default:300" json:"cache_ttl"`           // cache TTL in seconds
	CorsEnabled     *bool  `gorm:"default:false" json:"cors_enabled"`      // CORS
	CorsOrigins     string `gorm:"size:1024" json:"cors_origins"`          // allowed origins, comma-separated
	CorsMethods     string `gorm:"size:256" json:"cors_methods"`           // allowed methods
	CorsHeaders     string `gorm:"size:512" json:"cors_headers"`           // allowed headers
	SecurityHeaders *bool  `gorm:"default:false" json:"security_headers"`  // one-click security headers
	ErrorPagePath   string `gorm:"size:512" json:"error_page_path"`        // custom error page directory
	CustomDirectives string         `gorm:"type:text" json:"custom_directives"` // raw Caddy directives
	// Phase 4 batch 3: new host types
	RootPath        string `gorm:"size:512" json:"root_path"`          // root directory for static/PHP hosts
	DirectoryBrowse *bool  `gorm:"default:false" json:"directory_browse"` // enable directory listing
	PHPFastCGI      string `gorm:"size:255" json:"php_fastcgi"`        // PHP-FPM address e.g. "localhost:9000"
	IndexFiles      string `gorm:"size:255" json:"index_files"`        // custom index files e.g. "index.html index.php"
	Upstreams        []Upstream     `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"upstreams"`
	CustomHeaders  []CustomHeader `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"custom_headers"`
	AccessRules    []AccessRule   `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"access_rules"`
	Routes         []Route        `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"routes"`
	BasicAuths     []BasicAuth    `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"basic_auths"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// Upstream represents a backend server for reverse proxying
type Upstream struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	HostID    uint   `gorm:"index;not null" json:"host_id"`
	Address   string `gorm:"not null;size:255" json:"address"` // e.g. "localhost:3000" or "https://eol.wiki"
	Weight    int    `gorm:"default:1" json:"weight"`
	SortOrder int    `gorm:"default:0" json:"sort_order"`
}

// Route represents a path-based route within a host
type Route struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	HostID     uint   `gorm:"index;not null" json:"host_id"`
	Path       string `gorm:"not null;size:255;default:/" json:"path"` // e.g. "/api/*"
	UpstreamID *uint  `json:"upstream_id"`
	SortOrder  int    `gorm:"default:0" json:"sort_order"`
}

// CustomHeader represents a custom HTTP header to add/remove
type CustomHeader struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	HostID    uint   `gorm:"index;not null" json:"host_id"`
	Direction string `gorm:"not null;size:16;default:request" json:"direction"` // "request" or "response"
	Operation string `gorm:"not null;size:16;default:set" json:"operation"`     // "set", "add", "delete"
	Name      string `gorm:"not null;size:255" json:"name"`
	Value     string `gorm:"size:1024" json:"value"`
	SortOrder int    `gorm:"default:0" json:"sort_order"`
}

// AccessRule represents an IP allow/deny rule
type AccessRule struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	HostID    uint   `gorm:"index;not null" json:"host_id"`
	RuleType  string `gorm:"not null;size:16" json:"rule_type"` // "allow" or "deny"
	IPRange   string `gorm:"not null;size:64" json:"ip_range"`  // IP or CIDR like "192.168.1.0/24"
	SortOrder int    `gorm:"default:0" json:"sort_order"`
}

// BasicAuth represents a username/password for HTTP basic authentication
type BasicAuth struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	HostID       uint   `gorm:"index;not null" json:"host_id"`
	Username     string `gorm:"not null;size:64" json:"username"`
	PasswordHash string `gorm:"not null;size:255" json:"-"` // bcrypt hash, never exposed
}

// HostCreateRequest is the request body for creating/updating a host
type HostCreateRequest struct {
	Domain           string           `json:"domain" binding:"required"`
	HostType         string           `json:"host_type"`
	Enabled          *bool            `json:"enabled"`
	TLSEnabled       *bool            `json:"tls_enabled"`
	HTTPRedirect     *bool            `json:"http_redirect"`
	WebSocket        *bool            `json:"websocket"`
	RedirectURL      string           `json:"redirect_url"`
	RedirectCode     int              `json:"redirect_code"`
	// Batch 2
	Compression     *bool  `json:"compression"`
	CacheEnabled    *bool  `json:"cache_enabled"`
	CacheTTL        int    `json:"cache_ttl"`
	CorsEnabled     *bool  `json:"cors_enabled"`
	CorsOrigins     string `json:"cors_origins"`
	CorsMethods     string `json:"cors_methods"`
	CorsHeaders     string `json:"cors_headers"`
	SecurityHeaders *bool  `json:"security_headers"`
	ErrorPagePath   string `json:"error_page_path"`
	// Batch 3
	RootPath        string `json:"root_path"`
	DirectoryBrowse *bool  `json:"directory_browse"`
	PHPFastCGI      string `json:"php_fastcgi"`
	IndexFiles      string `json:"index_files"`
	TLSMode         string `json:"tls_mode"`
	DnsProviderID   *uint  `json:"dns_provider_id"`
	CustomDirectives string           `json:"custom_directives"`
	Upstreams        []UpstreamInput  `json:"upstreams"`
	CustomHeaders    []HeaderInput    `json:"custom_headers"`
	AccessRules      []AccessInput    `json:"access_rules"`
	BasicAuths       []BasicAuthInput `json:"basic_auths"`
}

// UpstreamInput is input for creating an upstream
type UpstreamInput struct {
	Address string `json:"address" binding:"required"`
	Weight  int    `json:"weight"`
}

// HeaderInput is input for creating a custom header
type HeaderInput struct {
	Direction string `json:"direction"`
	Operation string `json:"operation"`
	Name      string `json:"name" binding:"required"`
	Value     string `json:"value"`
}

// AccessInput is input for creating an access rule
type AccessInput struct {
	RuleType string `json:"rule_type" binding:"required"`
	IPRange  string `json:"ip_range" binding:"required"`
}

// BasicAuthInput is input for creating a basic auth credential
type BasicAuthInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"` // plain text, will be hashed
}

// ExportData represents the full export of all hosts
type ExportData struct {
	Version    string `json:"version"`
	ExportedAt string `json:"exported_at"`
	Hosts      []Host `json:"hosts"`
}

// AuditLog records admin actions for auditing
type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	Username  string    `gorm:"not null;size:64" json:"username"`
	Action    string    `gorm:"not null;size:16" json:"action"` // CREATE, UPDATE, DELETE, TOGGLE, START, STOP, etc.
	Target    string    `gorm:"not null;size:64" json:"target"` // e.g. "host", "caddy", "user"
	TargetID  string    `gorm:"size:32" json:"target_id"`       // ID of the affected resource
	Detail    string    `gorm:"type:text" json:"detail"`        // human-readable description
	IP        string    `gorm:"size:45" json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}
