package plugin

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
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

	db           *gorm.DB
	router       *gin.RouterGroup // /api/plugins (protected)
	publicRouter *gin.RouterGroup // /api/plugins (public, no JWT)
	eventBus     *EventBus
	coreAPI      CoreAPI
	dataDir      string // base data directory
	logger       *slog.Logger
}

// NewManager creates a plugin Manager.
func NewManager(db *gorm.DB, router *gin.RouterGroup, publicRouter *gin.RouterGroup, coreAPI CoreAPI, dataDir string) *Manager {
	logger := slog.Default().With("module", "plugin")
	return &Manager{
		plugins:      make(map[string]Plugin),
		contexts:     make(map[string]*Context),
		db:           db,
		router:       router,
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
// enabled plugin in topological order.
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

	// 3. Init each plugin.
	for _, id := range m.order {
		p := m.plugins[id]
		meta := p.Metadata()

		if !m.isEnabled(id) {
			m.logger.Info("plugin disabled, skipping", "id", id)
			continue
		}

		// Prepare plugin data directory.
		pluginDataDir := filepath.Join(m.dataDir, "plugins", id)
		if err := os.MkdirAll(pluginDataDir, 0755); err != nil {
			return fmt.Errorf("create data dir for plugin %q: %w", id, err)
		}

		// Create a sub-router under /api/plugins/{id}
		pluginRouter := m.router.Group("/" + id)
		publicPluginRouter := m.publicRouter.Group("/" + id)

		ctx := &Context{
			DB:           m.db,
			Router:       pluginRouter,
			PublicRouter: publicPluginRouter,
			EventBus:     m.eventBus,
			Logger:       m.logger.With("plugin", id),
			DataDir:      pluginDataDir,
			ConfigStore:  NewConfigStore(m.db, id),
			CoreAPI:      m.coreAPI,
		}
		m.contexts[id] = ctx

		if err := p.Init(ctx); err != nil {
			return fmt.Errorf("init plugin %q (v%s): %w", id, meta.Version, err)
		}
		m.logger.Info("plugin initialised", "id", id)
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
			Metadata: p.Metadata(),
			Enabled:  m.isEnabled(id),
		})
	}
	return list
}

// Enable enables a plugin (takes effect on next restart for now).
func (m *Manager) Enable(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.plugins[id]; !ok {
		return fmt.Errorf("plugin %q not found", id)
	}
	return m.setState(id, true)
}

// Disable disables a plugin (takes effect on next restart for now).
func (m *Manager) Disable(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.plugins[id]; !ok {
		return fmt.Errorf("plugin %q not found", id)
	}
	return m.setState(id, false)
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

// PluginState persists enabled/disabled state per plugin.
type PluginState struct {
	ID      string `gorm:"primaryKey;size:64"`
	Enabled *bool  `gorm:"default:true"`
}

func (PluginState) TableName() string { return "plugin_states" }

func (m *Manager) isEnabled(id string) bool {
	var state PluginState
	if err := m.db.Where("id = ?", id).First(&state).Error; err != nil {
		return true // enabled by default if no record exists
	}
	if state.Enabled == nil {
		return true
	}
	return *state.Enabled
}

func (m *Manager) setState(id string, enabled bool) error {
	return m.db.Where("id = ?", id).
		Assign(PluginState{ID: id, Enabled: &enabled}).
		FirstOrCreate(&PluginState{}).Error
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

// migratePluginState ensures every registered plugin has a state row.
func init() {
	// PluginState uses gorm tag defaults, nothing else needed here.
}
