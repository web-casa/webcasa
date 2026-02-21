package model

import (
	"time"
)

// User represents a panel administrator
type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"uniqueIndex;not null;size:64" json:"username"`
	Password  string    `gorm:"not null" json:"-"` // bcrypt hash, never exposed in JSON
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Host represents a reverse proxy host configuration
type Host struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	Domain        string         `gorm:"not null;uniqueIndex;size:255" json:"domain"`
	Enabled       bool           `gorm:"default:true" json:"enabled"`
	TLSEnabled    bool           `gorm:"default:true" json:"tls_enabled"`
	HTTPRedirect  bool           `gorm:"default:true" json:"http_redirect"`
	WebSocket     bool           `gorm:"default:false" json:"websocket"`
	Upstreams     []Upstream     `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"upstreams"`
	CustomHeaders []CustomHeader `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"custom_headers"`
	AccessRules   []AccessRule   `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"access_rules"`
	Routes        []Route        `gorm:"foreignKey:HostID;constraint:OnDelete:CASCADE" json:"routes"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// Upstream represents a backend server for reverse proxying
type Upstream struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	HostID    uint   `gorm:"index;not null" json:"host_id"`
	Address   string `gorm:"not null;size:255" json:"address"` // e.g. "localhost:3000" or "192.168.1.10:8080"
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

// HostCreateRequest is the request body for creating/updating a host
type HostCreateRequest struct {
	Domain        string           `json:"domain" binding:"required"`
	Enabled       *bool            `json:"enabled"`
	TLSEnabled    *bool            `json:"tls_enabled"`
	HTTPRedirect  *bool            `json:"http_redirect"`
	WebSocket     *bool            `json:"websocket"`
	Upstreams     []UpstreamInput  `json:"upstreams" binding:"required,min=1"`
	CustomHeaders []HeaderInput    `json:"custom_headers"`
	AccessRules   []AccessInput    `json:"access_rules"`
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

// ExportData represents the full export of all hosts
type ExportData struct {
	Version   string `json:"version"`
	ExportedAt string `json:"exported_at"`
	Hosts     []Host `json:"hosts"`
}
