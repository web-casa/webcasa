package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ── test helpers ──

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })

	// Migrate core models that plugins depend on (Setting).
	db.AutoMigrate(&struct {
		Key   string `gorm:"primaryKey;size:64"`
		Value string `gorm:"type:text"`
	}{})
	db.Exec("CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT)")

	return db
}

func setupTestManager(t *testing.T) (*Manager, *gorm.DB) {
	t.Helper()
	db := setupTestDB(t)
	// Migrate PluginState table so Enable/Disable work before InitAll.
	db.AutoMigrate(&PluginState{})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/api/plugins")
	publicRg := r.Group("/api/plugins")
	dataDir := t.TempDir()
	mgr := NewManager(db, rg, publicRg, &stubCoreAPI{}, dataDir)
	return mgr, db
}

// stubCoreAPI implements CoreAPI for testing.
type stubCoreAPI struct{}

func (s *stubCoreAPI) CreateHost(req CreateHostRequest) (uint, error) { return 1, nil }
func (s *stubCoreAPI) DeleteHost(id uint) error                       { return nil }
func (s *stubCoreAPI) ReloadCaddy() error                             { return nil }
func (s *stubCoreAPI) GetSetting(key string) (string, error)          { return "", nil }
func (s *stubCoreAPI) SetSetting(key, value string) error             { return nil }
func (s *stubCoreAPI) GetDB() *gorm.DB                                { return nil }

// ── stub plugin ──

type stubPlugin struct {
	meta       Metadata
	initCalled bool
	startCalled bool
	stopCalled  bool
}

func newStubPlugin(id string, deps []string, priority int) *stubPlugin {
	return &stubPlugin{
		meta: Metadata{
			ID:           id,
			Name:         id,
			Version:      "1.0.0",
			Dependencies: deps,
			Priority:     priority,
		},
	}
}

func (p *stubPlugin) Metadata() Metadata    { return p.meta }
func (p *stubPlugin) Init(_ *Context) error  { p.initCalled = true; return nil }
func (p *stubPlugin) Start() error           { p.startCalled = true; return nil }
func (p *stubPlugin) Stop() error            { p.stopCalled = true; return nil }

// ── tests ──

func TestRegisterAndList(t *testing.T) {
	mgr, _ := setupTestManager(t)

	p := newStubPlugin("hello", nil, 0)
	if err := mgr.Register(p); err != nil {
		t.Fatal(err)
	}

	// Duplicate registration should fail.
	if err := mgr.Register(newStubPlugin("hello", nil, 0)); err == nil {
		t.Fatal("expected error for duplicate registration")
	}

	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}
	if list[0].ID != "hello" {
		t.Fatalf("expected id=hello, got %s", list[0].ID)
	}
	if !list[0].Enabled {
		t.Fatal("expected plugin to be enabled by default")
	}
}

func TestInitStartStop(t *testing.T) {
	mgr, _ := setupTestManager(t)

	p := newStubPlugin("test", nil, 0)
	mgr.Register(p)

	if err := mgr.InitAll(); err != nil {
		t.Fatal(err)
	}
	if !p.initCalled {
		t.Fatal("Init was not called")
	}

	if err := mgr.StartAll(); err != nil {
		t.Fatal(err)
	}
	if !p.startCalled {
		t.Fatal("Start was not called")
	}

	if err := mgr.StopAll(); err != nil {
		t.Fatal(err)
	}
	if !p.stopCalled {
		t.Fatal("Stop was not called")
	}
}

func TestDependencyOrder(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// B depends on A.
	a := newStubPlugin("a", nil, 10)
	b := newStubPlugin("b", []string{"a"}, 5)

	mgr.Register(b)
	mgr.Register(a)

	if err := mgr.InitAll(); err != nil {
		t.Fatal(err)
	}

	// A must come before B in the order.
	aIdx, bIdx := -1, -1
	for i, id := range mgr.order {
		if id == "a" {
			aIdx = i
		}
		if id == "b" {
			bIdx = i
		}
	}
	if aIdx >= bIdx {
		t.Fatalf("expected a (idx=%d) before b (idx=%d)", aIdx, bIdx)
	}
}

func TestCircularDependency(t *testing.T) {
	mgr, _ := setupTestManager(t)

	a := newStubPlugin("a", []string{"b"}, 0)
	b := newStubPlugin("b", []string{"a"}, 0)

	mgr.Register(a)
	mgr.Register(b)

	if err := mgr.InitAll(); err == nil {
		t.Fatal("expected circular dependency error")
	}
}

func TestMissingDependency(t *testing.T) {
	mgr, _ := setupTestManager(t)

	p := newStubPlugin("p", []string{"missing"}, 0)
	mgr.Register(p)

	if err := mgr.InitAll(); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestEnableDisable(t *testing.T) {
	mgr, _ := setupTestManager(t)

	p := newStubPlugin("test", nil, 0)
	mgr.Register(p)

	if err := mgr.Disable("test"); err != nil {
		t.Fatal(err)
	}

	if err := mgr.InitAll(); err != nil {
		t.Fatal(err)
	}

	if p.initCalled {
		t.Fatal("disabled plugin should not have been initialised")
	}

	if err := mgr.Enable("test"); err != nil {
		t.Fatal(err)
	}

	list := mgr.List()
	for _, info := range list {
		if info.ID == "test" && !info.Enabled {
			t.Fatal("plugin should be enabled after Enable()")
		}
	}
}

func TestPluginDataDir(t *testing.T) {
	mgr, _ := setupTestManager(t)

	p := newStubPlugin("test", nil, 0)
	mgr.Register(p)
	mgr.InitAll()

	expectedDir := filepath.Join(mgr.dataDir, "plugins", "test")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Fatalf("plugin data dir was not created: %s", expectedDir)
	}
}
