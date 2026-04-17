package backup

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRetentionTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&BackupConfig{}, &BackupSnapshot{}, &BackupLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// kopia is intentionally nil: test snapshots carry empty SnapshotID so
	// enforceRetention's `if snap.SnapshotID != ""` guard skips DeleteSnapshot
	// entirely, keeping this unit test independent of the Kopia binary.
	return &Service{
		db:     db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func seedSnapshot(t *testing.T, db *gorm.DB, ageDays int, sizeMB int64) *BackupSnapshot {
	t.Helper()
	s := &BackupSnapshot{
		Status:    "completed",
		SizeBytes: sizeMB * 1024 * 1024,
		CreatedAt: time.Now().AddDate(0, 0, -ageDays),
	}
	if err := db.Create(s).Error; err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	return s
}

func countCompletedSnapshots(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var n int64
	db.Model(&BackupSnapshot{}).Where("status = ?", "completed").Count(&n)
	return int(n)
}

// TestEnforceRetention_CountBased verifies "keep latest N" works unchanged
// from pre-v0.11 behaviour.
func TestEnforceRetention_CountBased(t *testing.T) {
	svc := newRetentionTestService(t)
	for i := 0; i < 10; i++ {
		seedSnapshot(t, svc.db, i, 10)
	}
	cfg := &BackupConfig{RetainCount: 5, MinRetainCount: 0}
	svc.enforceRetention(cfg)

	if got := countCompletedSnapshots(t, svc.db); got != 5 {
		t.Errorf("count=5, expected 5 snapshots after retention, got %d", got)
	}
}

// TestEnforceRetention_MinFloor_PreventsTotalWipe verifies the v0.11 safety
// floor: when all rules combined would delete everything, the newest
// MinRetainCount snapshots are pinned.
func TestEnforceRetention_MinFloor_PreventsTotalWipe(t *testing.T) {
	svc := newRetentionTestService(t)
	// 3 snapshots, all more than 1 day old.
	for i := 1; i <= 3; i++ {
		seedSnapshot(t, svc.db, i+10, 10) // 11, 12, 13 days old
	}
	// Aggressive retention: days=1 would nuke everything.
	cfg := &BackupConfig{RetainCount: 0, RetainDays: 1, MinRetainCount: 1}
	svc.enforceRetention(cfg)

	if got := countCompletedSnapshots(t, svc.db); got != 1 {
		t.Errorf("MinRetainCount=1 must pin 1 snapshot, got %d remaining", got)
	}
}

// TestEnforceRetention_MinFloor_PinsNewest verifies the floor pins the
// NEWEST snapshots, not arbitrary ones.
func TestEnforceRetention_MinFloor_PinsNewest(t *testing.T) {
	svc := newRetentionTestService(t)
	// 5 snapshots at distinct ages.
	for i := 0; i < 5; i++ {
		seedSnapshot(t, svc.db, i*10, 10) // 0, 10, 20, 30, 40 days
	}
	// retain_days=5 flags ages 10/20/30/40; min_retain_count=3 pins the 3 newest (0, 10, 20).
	cfg := &BackupConfig{RetainCount: 0, RetainDays: 5, MinRetainCount: 3}
	svc.enforceRetention(cfg)

	var remaining []BackupSnapshot
	svc.db.Where("status = ?", "completed").Order("created_at DESC").Find(&remaining)
	if len(remaining) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(remaining))
	}
	now := time.Now()
	// All remaining should be age <= 20 days.
	for _, s := range remaining {
		if age := now.Sub(s.CreatedAt); age > 25*24*time.Hour {
			t.Errorf("oldest pinned snapshot too old: %v days", age.Hours()/24)
		}
	}
}

// TestEnforceRetention_MinFloor_ZeroIsInfinite verifies MinRetainCount=0
// preserves pre-v0.11 behaviour (no floor).
func TestEnforceRetention_MinFloor_ZeroIsInfinite(t *testing.T) {
	svc := newRetentionTestService(t)
	for i := 1; i <= 3; i++ {
		seedSnapshot(t, svc.db, i+10, 10)
	}
	// With floor=0 and retain_days=1, everything should go.
	cfg := &BackupConfig{RetainCount: 0, RetainDays: 1, MinRetainCount: 0}
	svc.enforceRetention(cfg)

	if got := countCompletedSnapshots(t, svc.db); got != 0 {
		t.Errorf("MinRetainCount=0 must not add a floor, expected 0 remaining, got %d", got)
	}
}

// TestEnforceRetention_CountAgeSize_Combined verifies the union semantics
// still apply pre-floor, then the floor trims the set.
func TestEnforceRetention_CountAgeSize_Combined(t *testing.T) {
	svc := newRetentionTestService(t)
	// 6 snapshots: 0, 5, 10, 15, 20, 25 days old; each 500MB.
	for i := 0; i < 6; i++ {
		seedSnapshot(t, svc.db, i*5, 500)
	}
	// retain_count=3 (delete 4,5,6 by count) + retain_max_size=1000MB (keep oldest allowed only 2×500=1000MB).
	cfg := &BackupConfig{RetainCount: 3, RetainDays: 0, RetainMaxSizeMB: 1000, MinRetainCount: 1}
	svc.enforceRetention(cfg)

	got := countCompletedSnapshots(t, svc.db)
	// Expect 2 (size cap trims to 2×500MB = 1000MB after count already dropped 3 oldest).
	if got != 2 {
		t.Errorf("combined count+size retention: expected 2 remaining, got %d", got)
	}
}
