package mcpserver

import "time"

// APIToken represents a long-lived API token for MCP and external integrations.
// Stored in the core "api_tokens" table (not plugin-prefixed, as auth middleware
// needs to access it at the framework level).
type APIToken struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	UserID      uint       `gorm:"index;not null" json:"user_id"`
	Name        string     `gorm:"not null;size:128" json:"name"`
	TokenHash   string     `gorm:"not null;size:64;uniqueIndex" json:"-"` // SHA-256 hex
	Prefix      string     `gorm:"not null;size:11;index" json:"prefix"`  // "wc_" + first 8 hex chars for fast lookup
	Permissions string     `gorm:"type:text;default:'[]'" json:"permissions"` // JSON array e.g. ["hosts:*","deploy:*"]
	LastUsedAt  *time.Time `json:"last_used_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (APIToken) TableName() string { return "api_tokens" }
