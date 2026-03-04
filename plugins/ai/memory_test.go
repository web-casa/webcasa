package ai

import (
	"log/slog"
	"math"
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&Memory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ── Vector helpers ──

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	sim := cosineSimilarity(a, a)
	if sim < 0.999 {
		t.Errorf("identical vectors should have similarity ~1.0, got %f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	sim := cosineSimilarity(a, b)
	if sim > 0.001 {
		t.Errorf("orthogonal vectors should have similarity ~0.0, got %f", sim)
	}
}

func TestCosineSimilarity_Known(t *testing.T) {
	a := []float32{1, 1, 0}
	b := []float32{1, 0, 1}
	// Expected: 1 / (sqrt(2) * sqrt(2)) = 0.5
	sim := cosineSimilarity(a, b)
	if math.Abs(float64(sim)-0.5) > 0.001 {
		t.Errorf("expected ~0.5, got %f", sim)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	sim := cosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("different-length vectors should return 0, got %f", sim)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	sim := cosineSimilarity(nil, nil)
	if sim != 0 {
		t.Errorf("empty vectors should return 0, got %f", sim)
	}
}

// ── Serialization ──

func TestSerializeDeserializeEmbedding(t *testing.T) {
	original := []float32{0.1, -0.5, 3.14, 0, -1.0}
	bytes := serializeEmbedding(original)
	roundtrip := deserializeEmbedding(bytes)

	if len(roundtrip) != len(original) {
		t.Fatalf("length mismatch: %d != %d", len(roundtrip), len(original))
	}
	for i := range original {
		if original[i] != roundtrip[i] {
			t.Errorf("index %d: %f != %f", i, original[i], roundtrip[i])
		}
	}
}

func TestDeserializeEmbedding_BadLength(t *testing.T) {
	result := deserializeEmbedding([]byte{1, 2, 3}) // not multiple of 4
	if result != nil {
		t.Errorf("expected nil for bad length, got %v", result)
	}
}

// ── Tokenize ──

func TestTokenize(t *testing.T) {
	tokens := tokenize("How to fix MySQL OOM crash?")
	expected := []string{"how", "to", "fix", "mysql", "oom", "crash"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}

func TestTokenize_ShortWords(t *testing.T) {
	tokens := tokenize("I a do it")
	// "I" and "a" are 1 char, filtered out; "do" and "it" are 2 chars
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d: %v", len(tokens), tokens)
	}
}

// ── MemoryService ──

func TestMemoryService_SaveAndSearch(t *testing.T) {
	db := setupTestDB(t)
	ms := NewMemoryService(db, testLogger())

	// Save some memories.
	_, err := ms.SaveMemory("This server runs Next.js on port 3000 with PM2", "server_config", 0.8, nil)
	if err != nil {
		t.Fatalf("save memory 1: %v", err)
	}
	_, err = ms.SaveMemory("MySQL OOM was fixed by setting innodb_buffer_pool_size=256M", "troubleshooting", 0.9, nil)
	if err != nil {
		t.Fatalf("save memory 2: %v", err)
	}
	_, err = ms.SaveMemory("User prefers Docker Compose for deployments", "user_preference", 0.7, nil)
	if err != nil {
		t.Fatalf("save memory 3: %v", err)
	}

	// Verify count.
	count, err := ms.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 memories, got %d", count)
	}

	// Search by keyword.
	results, err := ms.SearchMemories("MySQL OOM", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	found := false
	for _, r := range results {
		if r.Category == "troubleshooting" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find the troubleshooting memory")
	}
}

func TestMemoryService_KeywordFallback(t *testing.T) {
	db := setupTestDB(t)
	ms := NewMemoryService(db, testLogger())
	// No embedding client — should use keyword search.

	ms.SaveMemory("Redis is running on port 6379", "server_config", 0.5, nil)
	ms.SaveMemory("Nginx was removed, using Caddy now", "server_config", 0.6, nil)

	results, err := ms.SearchMemories("Redis port", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestMemoryService_Deduplication(t *testing.T) {
	db := setupTestDB(t)
	ms := NewMemoryService(db, testLogger())

	// Save identical content twice.
	m1, err := ms.SaveMemory("Server has 4 CPU cores and 8GB RAM", "server_config", 0.5, nil)
	if err != nil {
		t.Fatalf("save 1: %v", err)
	}
	m2, err := ms.SaveMemory("Server has 4 CPU cores and 8GB RAM", "server_config", 0.8, nil)
	if err != nil {
		t.Fatalf("save 2: %v", err)
	}

	// Should be the same record (deduplicated).
	if m1.ID != m2.ID {
		t.Errorf("expected deduplication, got different IDs: %d vs %d", m1.ID, m2.ID)
	}

	// Importance should be upgraded to the higher value.
	if m2.Importance < 0.8 {
		t.Errorf("importance should be upgraded to 0.8, got %f", m2.Importance)
	}

	// Should still be only 1 memory.
	count, _ := ms.Count()
	if count != 1 {
		t.Errorf("expected 1 memory after dedup, got %d", count)
	}
}

func TestMemoryService_BuildMemoryContext(t *testing.T) {
	db := setupTestDB(t)
	ms := NewMemoryService(db, testLogger())

	ms.SaveMemory("Port 3000 is used by the Next.js app", "server_config", 0.8, nil)
	ms.SaveMemory("The MySQL root password was changed last week", "troubleshooting", 0.6, nil)

	ctx, err := ms.BuildMemoryContext("Next.js port", 5)
	if err != nil {
		t.Fatalf("build context: %v", err)
	}
	if ctx == "" {
		t.Fatal("expected non-empty context")
	}
	if !contains(ctx, "Server Memory") {
		t.Error("expected context to contain 'Server Memory' header")
	}
	if !contains(ctx, "server_config") {
		t.Error("expected context to contain category")
	}
}

func TestMemoryService_DeleteAndClear(t *testing.T) {
	db := setupTestDB(t)
	ms := NewMemoryService(db, testLogger())

	m1, _ := ms.SaveMemory("fact one", "general", 0.5, nil)
	ms.SaveMemory("fact two", "general", 0.5, nil)

	// Delete one.
	if err := ms.DeleteMemory(m1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	count, _ := ms.Count()
	if count != 1 {
		t.Errorf("expected 1 after delete, got %d", count)
	}

	// Clear all.
	if err := ms.ClearAll(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	count, _ = ms.Count()
	if count != 0 {
		t.Errorf("expected 0 after clear, got %d", count)
	}
}

func TestMemoryService_ListPagination(t *testing.T) {
	db := setupTestDB(t)
	ms := NewMemoryService(db, testLogger())

	for i := 0; i < 25; i++ {
		ms.SaveMemory("memory "+string(rune('A'+i)), "general", 0.5, nil)
	}

	// Page 1.
	memories, total, err := ms.ListMemories(1, 10, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 25 {
		t.Errorf("expected total 25, got %d", total)
	}
	if len(memories) != 10 {
		t.Errorf("expected 10 on page 1, got %d", len(memories))
	}

	// Page 3 (should have 5).
	memories, _, _ = ms.ListMemories(3, 10, "")
	if len(memories) != 5 {
		t.Errorf("expected 5 on page 3, got %d", len(memories))
	}
}

func TestMemoryService_EmptyContent(t *testing.T) {
	db := setupTestDB(t)
	ms := NewMemoryService(db, testLogger())

	_, err := ms.SaveMemory("", "general", 0.5, nil)
	if err == nil {
		t.Error("expected error for empty content")
	}

	_, err = ms.SaveMemory("  \t\n  ", "general", 0.5, nil)
	if err == nil {
		t.Error("expected error for whitespace-only content")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
