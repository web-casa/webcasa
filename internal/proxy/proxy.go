// Package proxy defines the ProxyBackend interface for reverse proxy management.
// This abstraction allows WebCasa to support multiple proxy backends (Caddy, Nginx, etc.)
// while currently only implementing CaddyBackend.
package proxy

import (
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
)

// Backend is the interface for reverse proxy management.
// All proxy backends (Caddy, Nginx, HAProxy, etc.) must implement this.
type Backend interface {
	// Name returns the proxy backend identifier (e.g., "caddy", "nginx").
	Name() string

	// GenerateConfig renders a configuration string from the given hosts.
	GenerateConfig(hosts []model.Host, cfg *config.Config, dnsProviders map[uint]model.DnsProvider) string

	// WriteConfig atomically writes the configuration to disk.
	WriteConfig(content string) error

	// Reload tells the proxy to reload its configuration.
	Reload() error

	// RequestReload schedules a debounced reload (coalescing multiple rapid calls).
	RequestReload() error

	// Validate checks configuration syntax without applying it.
	Validate(content string) error

	// IsRunning returns whether the proxy process is currently running.
	IsRunning() bool

	// Start starts the proxy process.
	Start() error

	// Stop stops the proxy process.
	Stop() error

	// GetConfigContent returns the current configuration file content.
	GetConfigContent() (string, error)

	// Status returns proxy status information.
	Status() map[string]interface{}

	// EnsureConfig creates a default configuration if none exists.
	EnsureConfig() error

	// Format formats the configuration content.
	Format(content string) (string, error)

	// Version returns the proxy binary version string.
	Version() string
}
