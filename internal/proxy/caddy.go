package proxy

import (
	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
)

// CaddyBackend implements Backend using the existing caddy.Manager.
type CaddyBackend struct {
	mgr *caddy.Manager
}

// NewCaddyBackend wraps an existing caddy.Manager as a Backend.
func NewCaddyBackend(mgr *caddy.Manager) *CaddyBackend {
	return &CaddyBackend{mgr: mgr}
}

// Manager returns the underlying caddy.Manager for direct access when needed.
func (c *CaddyBackend) Manager() *caddy.Manager {
	return c.mgr
}

func (c *CaddyBackend) Name() string { return "caddy" }

func (c *CaddyBackend) GenerateConfig(hosts []model.Host, cfg *config.Config, dnsProviders map[uint]model.DnsProvider) string {
	return caddy.RenderCaddyfile(hosts, cfg, dnsProviders)
}

func (c *CaddyBackend) WriteConfig(content string) error {
	return c.mgr.WriteCaddyfile(content)
}

func (c *CaddyBackend) Reload() error {
	return c.mgr.Reload()
}

func (c *CaddyBackend) RequestReload() error {
	return c.mgr.RequestReload()
}

func (c *CaddyBackend) Validate(content string) error {
	return c.mgr.Validate(content)
}

func (c *CaddyBackend) IsRunning() bool {
	return c.mgr.IsRunning()
}

func (c *CaddyBackend) Start() error {
	return c.mgr.Start()
}

func (c *CaddyBackend) Stop() error {
	return c.mgr.Stop()
}

func (c *CaddyBackend) GetConfigContent() (string, error) {
	return c.mgr.GetCaddyfileContent()
}

func (c *CaddyBackend) Status() map[string]interface{} {
	return c.mgr.Status()
}

func (c *CaddyBackend) EnsureConfig() error {
	return c.mgr.EnsureCaddyfile()
}

func (c *CaddyBackend) Format(content string) (string, error) {
	return c.mgr.Format(content)
}

func (c *CaddyBackend) Version() string {
	return c.mgr.Version()
}

// Compile-time interface check.
var _ Backend = (*CaddyBackend)(nil)
