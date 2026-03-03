package plugin

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Manager manages the lifecycle of all registered plugins.
type Manager struct {
	mu       sync.RWMutex
	plugins  map[string]Plugin   // id → plugin
	contexts map[string]*Context // id → context
	order    []string            // topological load order
	disabled sync.Map            // id → true for disabled plugins (runtime guard)

	db           *gorm.DB
	router       *gin.RouterGroup // /api/plugins (protected)
	adminRouter  *gin.RouterGroup // /api/plugins (admin only)
	publicRouter *gin.RouterGroup // /api/plugins (public, no JWT)
	eventBus     *EventBus
	coreAPI      CoreAPI
	dataDir      string // base data directory
	logger       *slog.Logger
}

// NewManager creates a plugin Manager.
func NewManager(db *gorm.DB, router *gin.RouterGroup, adminRouter *gin.RouterGroup, publicRouter *gin.RouterGroup, coreAPI CoreAPI, dataDir string) *Manager {
	logger := slog.Default().With("module", "plugin")
	return &Manager{
		plugins:      make(map[string]Plugin),
		contexts:     make(map[string]*Context),
		db:           db,
		router:       router,
		adminRouter:  adminRouter,
		publicRouter: publicRouter,
		eventBus:     NewEventBus(logger),
		coreAPI:      coreAPI,
		dataDir:      dataDir,
		logger:       logger,
	}
}

// EventBus returns the shared event bus.
func (m *Manager) EventBus() *EventBus {
	return m.eventBus
}

// Register adds a plugin to the manager. Call this before InitAll.
func (m *Manager) Register(p Plugin) error {
	meta := p.Metadata()
	if meta.ID == "" {
		return fmt.Errorf("plugin has empty ID")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.plugins[meta.ID]; exists {
		return fmt.Errorf("plugin %q already registered", meta.ID)
	}

	m.plugins[meta.ID] = p
	m.logger.Info("plugin registered", "id", meta.ID, "version", meta.Version)
	return nil
}

// InitAll resolves dependencies, runs migrations, and calls Init on every
// registered plugin in topological order. Disabled plugins are still initialised
// (routes registered, DB migrated) but marked in the disabled map so that the
// guard middleware blocks their API requests at runtime.
func (m *Manager) InitAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Topological sort by dependencies.
	order, err := m.topoSort()
	if err != nil {
		return fmt.Errorf("dependency resolution: %w", err)
	}
	m.order = order

	// 2. Ensure the plugin-state table exists.
	if err := m.db.AutoMigrate(&PluginState{}); err != nil {
		return fmt.Errorf("migrate plugin_states: %w", err)
	}

	// 3. Seed default states on fresh installs.
	m.seedDefaultStates()

	// 4. Init each plugin (all plugins, including disabled).
	for _, id := range m.order {
		p := m.plugins[id]
		meta := p.Metadata()
		enabled := m.isEnabled(id)

		if !enabled {
			m.disabled.Store(id, true)
		}

		// Prepare plugin data directory.
		pluginDataDir := filepath.Join(m.dataDir, "plugins", id)
		if err := os.MkdirAll(pluginDataDir, 0755); err != nil {
			return fmt.Errorf("create data dir for plugin %q: %w", id, err)
		}

		// Create a sub-router under /api/plugins/{id}
		pluginRouter := m.router.Group("/" + id)
		adminPluginRouter := m.adminRouter.Group("/" + id)
		publicPluginRouter := m.publicRouter.Group("/" + id)

		ctx := &Context{
			DB:           m.db,
			Router:       pluginRouter,
			AdminRouter:  adminPluginRouter,
			PublicRouter: publicPluginRouter,
			EventBus:     m.eventBus,
			Logger:       m.logger.With("plugin", id),
			DataDir:      pluginDataDir,
			ConfigStore:  NewConfigStore(m.db, id),
			CoreAPI:      m.coreAPI,
		}
		m.contexts[id] = ctx

		if err := p.Init(ctx); err != nil {
			if enabled {
				return fmt.Errorf("init plugin %q (v%s): %w", id, meta.Version, err)
			}
			// Disabled plugins failing Init is non-fatal; just log and skip.
			m.logger.Warn("init failed for disabled plugin", "id", id, "err", err)
			continue
		}
		if enabled {
			m.logger.Info("plugin initialised", "id", id)
		} else {
			m.logger.Info("plugin initialised (disabled)", "id", id)
		}
	}

	return nil
}

// StartAll calls Start on every enabled plugin in load order.
func (m *Manager) StartAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, id := range m.order {
		if !m.isEnabled(id) {
			continue
		}
		if err := m.plugins[id].Start(); err != nil {
			return fmt.Errorf("start plugin %q: %w", id, err)
		}
		m.logger.Info("plugin started", "id", id)
	}
	return nil
}

// StopAll calls Stop on every enabled plugin in reverse order.
func (m *Manager) StopAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := len(m.order) - 1; i >= 0; i-- {
		id := m.order[i]
		if !m.isEnabled(id) {
			continue
		}
		if err := m.plugins[id].Stop(); err != nil {
			m.logger.Error("failed to stop plugin", "id", id, "err", err)
		} else {
			m.logger.Info("plugin stopped", "id", id)
		}
	}
	return nil
}

// List returns info about all registered plugins.
func (m *Manager) List() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]PluginInfo, 0, len(m.plugins))
	for _, id := range m.sortedIDs() {
		p := m.plugins[id]
		list = append(list, PluginInfo{
			Metadata:      p.Metadata(),
			Enabled:       m.isEnabled(id),
			ShowInSidebar: m.isSidebarVisible(id),
		})
	}
	return list
}

// Enable enables a plugin. Takes effect immediately: updates the disabled map
// and starts the plugin's background tasks. Idempotent: no-op if already enabled.
func (m *Manager) Enable(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %q not found", id)
	}

	// Idempotent: skip if already enabled.
	if m.isEnabled(id) {
		return nil
	}

	if err := m.setState(id, true); err != nil {
		return err
	}

	// Remove from disabled map so guard middleware allows requests.
	m.disabled.Delete(id)

	// Start the plugin's background tasks.
	if err := p.Start(); err != nil {
		m.logger.Error("failed to start plugin on enable", "id", id, "err", err)
		// Non-fatal: plugin is enabled but Start failed.
	} else {
		m.logger.Info("plugin enabled and started", "id", id)
	}
	return nil
}

// Disable disables a plugin. Takes effect immediately: stops the plugin and
// blocks its API routes via the guard middleware. Idempotent: no-op if already disabled.
func (m *Manager) Disable(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %q not found", id)
	}

	// Idempotent: skip if already disabled.
	if !m.isEnabled(id) {
		return nil
	}

	// Stop background tasks first.
	if err := p.Stop(); err != nil {
		m.logger.Error("failed to stop plugin on disable", "id", id, "err", err)
	}

	// Mark as disabled in memory (guard middleware blocks immediately).
	m.disabled.Store(id, true)

	// Persist to DB.
	if err := m.setState(id, false); err != nil {
		return err
	}

	m.logger.Info("plugin disabled", "id", id)
	return nil
}

// FrontendManifests collects manifests from all enabled plugins that implement
// FrontendProvider.
func (m *Manager) FrontendManifests() []FrontendManifest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var manifests []FrontendManifest
	for _, id := range m.order {
		if !m.isEnabled(id) {
			continue
		}
		if fp, ok := m.plugins[id].(FrontendProvider); ok {
			manifests = append(manifests, fp.FrontendManifest())
		}
	}
	return manifests
}

// ──────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────

// PluginState persists enabled/disabled and sidebar visibility state per plugin.
type PluginState struct {
	ID            string `gorm:"primaryKey;size:64"`
	Enabled       *bool  `gorm:"default:true"`
	ShowInSidebar *bool  `gorm:"default:true"`
}

func (PluginState) TableName() string { return "plugin_states" }

func (m *Manager) isEnabled(id string) bool {
	var state PluginState
	if err := m.db.Where("id = ?", id).First(&state).Error; err != nil {
		// No record — only AI plugin enabled by default on fresh installs.
		return id == "ai"
	}
	if state.Enabled == nil {
		return id == "ai"
	}
	return *state.Enabled
}

func (m *Manager) isSidebarVisible(id string) bool {
	var state PluginState
	if err := m.db.Where("id = ?", id).First(&state).Error; err != nil {
		return true // visible by default
	}
	if state.ShowInSidebar == nil {
		return true
	}
	return *state.ShowInSidebar
}

// SetSidebarVisible sets whether a plugin appears in the sidebar.
func (m *Manager) SetSidebarVisible(id string, visible bool) error {
	if _, ok := m.plugins[id]; !ok {
		return fmt.Errorf("plugin %q not found", id)
	}
	return m.db.Where("id = ?", id).
		Assign(map[string]interface{}{"show_in_sidebar": visible}).
		FirstOrCreate(&PluginState{ID: id}).Error
}

func (m *Manager) setState(id string, enabled bool) error {
	return m.db.Where("id = ?", id).
		Assign(PluginState{ID: id, Enabled: &enabled}).
		FirstOrCreate(&PluginState{}).Error
}

// seedDefaultStates creates default plugin_states rows on fresh installs.
// Only AI is enabled by default; all others are disabled.
func (m *Manager) seedDefaultStates() {
	var count int64
	m.db.Model(&PluginState{}).Count(&count)
	if count > 0 {
		return // existing install — respect current state
	}

	m.logger.Info("fresh install detected, seeding default plugin states")
	enabled := true
	disabled := false
	for id := range m.plugins {
		if id == "ai" {
			m.db.Create(&PluginState{ID: id, Enabled: &enabled})
		} else {
			m.db.Create(&PluginState{ID: id, Enabled: &disabled})
		}
	}
}

// PluginGuardMiddleware returns a Gin middleware that blocks requests to
// disabled plugins. Apply this to the plugin router groups.
func (m *Manager) PluginGuardMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Path format: /api/plugins/{pluginID}/...
		// The router group already strips the prefix up to /plugins/, so we
		// need to extract from the full URL path.
		pluginID := extractPluginID(c.Request.URL.Path)
		if pluginID != "" {
			if _, isDisabled := m.disabled.Load(pluginID); isDisabled {
				c.JSON(http.StatusNotFound, gin.H{"error": "Plugin not available"})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// extractPluginID extracts the plugin ID from a URL path like
// /api/plugins/{id}/... Returns empty string if not found.
func extractPluginID(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	// Look for "plugins" segment, then the next segment is the ID.
	for i, p := range parts {
		if p == "plugins" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// topoSort returns plugin IDs in dependency order using Kahn's algorithm.
func (m *Manager) topoSort() ([]string, error) {
	// Build adjacency and in-degree maps.
	inDeg := make(map[string]int)
	adj := make(map[string][]string)

	for id := range m.plugins {
		if _, ok := inDeg[id]; !ok {
			inDeg[id] = 0
		}
		for _, dep := range m.plugins[id].Metadata().Dependencies {
			if _, ok := m.plugins[dep]; !ok {
				return nil, fmt.Errorf("plugin %q depends on unknown plugin %q", id, dep)
			}
			adj[dep] = append(adj[dep], id)
			inDeg[id]++
		}
	}

	// Priority-aware BFS: among nodes with in-degree 0, pick lowest priority first.
	type entry struct {
		id       string
		priority int
	}

	var queue []entry
	for id, d := range inDeg {
		if d == 0 {
			queue = append(queue, entry{id, m.plugins[id].Metadata().Priority})
		}
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].priority < queue[j].priority })

	var order []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur.id)

		for _, next := range adj[cur.id] {
			inDeg[next]--
			if inDeg[next] == 0 {
				queue = append(queue, entry{next, m.plugins[next].Metadata().Priority})
			}
		}
		sort.Slice(queue, func(i, j int) bool { return queue[i].priority < queue[j].priority })
	}

	if len(order) != len(m.plugins) {
		return nil, fmt.Errorf("circular dependency detected among plugins")
	}
	return order, nil
}

func (m *Manager) sortedIDs() []string {
	ids := make([]string, 0, len(m.plugins))
	for id := range m.plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
