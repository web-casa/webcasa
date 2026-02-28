package plugin

import (
	"testing"

	"github.com/web-casa/webcasa/internal/model"
)

func TestConfigStoreGetSetDelete(t *testing.T) {
	db := setupTestDB(t)
	db.AutoMigrate(&model.Setting{})

	cs := NewConfigStore(db, "test-plugin")

	// Get non-existent key returns empty.
	if v := cs.Get("foo"); v != "" {
		t.Fatalf("expected empty, got %q", v)
	}

	// Set and Get.
	if err := cs.Set("foo", "bar"); err != nil {
		t.Fatal(err)
	}
	if v := cs.Get("foo"); v != "bar" {
		t.Fatalf("expected bar, got %q", v)
	}

	// Update existing.
	if err := cs.Set("foo", "baz"); err != nil {
		t.Fatal(err)
	}
	if v := cs.Get("foo"); v != "baz" {
		t.Fatalf("expected baz, got %q", v)
	}

	// Delete.
	if err := cs.Delete("foo"); err != nil {
		t.Fatal(err)
	}
	if v := cs.Get("foo"); v != "" {
		t.Fatalf("expected empty after delete, got %q", v)
	}
}

func TestConfigStoreIsolation(t *testing.T) {
	db := setupTestDB(t)
	db.AutoMigrate(&model.Setting{})

	cs1 := NewConfigStore(db, "plugin-a")
	cs2 := NewConfigStore(db, "plugin-b")

	cs1.Set("key", "from-a")
	cs2.Set("key", "from-b")

	if v := cs1.Get("key"); v != "from-a" {
		t.Fatalf("expected from-a, got %q", v)
	}
	if v := cs2.Get("key"); v != "from-b" {
		t.Fatalf("expected from-b, got %q", v)
	}
}

func TestConfigStoreAll(t *testing.T) {
	db := setupTestDB(t)
	db.AutoMigrate(&model.Setting{})

	cs := NewConfigStore(db, "multi")
	cs.Set("a", "1")
	cs.Set("b", "2")

	all := cs.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if all["a"] != "1" || all["b"] != "2" {
		t.Fatalf("unexpected values: %v", all)
	}
}
