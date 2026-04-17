package database

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseMemoryLimitMB(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"512m", 512},
		{"1g", 1024},
		{"0.5g", 512},
		{"1.5g", 1536},
		{"256mb", 256},
		{"2GB", 2048},
		{"768K", 0}, // 768 KB rounds to 0 MB
		{"garbage", 0},
		{"-1g", 0},
		// IEC / container-style units added in Phase 3 review fix.
		{"1Gi", 1024},
		{"512Mi", 512},
		{"1Ti", 1024 * 1024},
		{"2048Ki", 2}, // 2048 KiB = 2 MiB
		// Plain terabyte.
		{"2T", 2048 * 1024},
	}
	for _, tc := range cases {
		if got := parseMemoryLimitMB(tc.in); got != tc.want {
			t.Errorf("parseMemoryLimitMB(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestParseMemoryLimitMBStrict verifies the strict parser used by preset
// application surfaces the failure reason instead of silently falling back.
func TestParseMemoryLimitMBStrict(t *testing.T) {
	good := map[string]int{
		"512m":  512,
		"1Gi":   1024,
		"0.5g":  512,
		"2T":    2048 * 1024,
		"2048":  2048, // no suffix = MB
	}
	for in, want := range good {
		got, err := parseMemoryLimitMBStrict(in)
		if err != nil {
			t.Errorf("parseMemoryLimitMBStrict(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseMemoryLimitMBStrict(%q) = %d, want %d", in, got, want)
		}
	}

	// Note: underscore digit separators ("1_000m") may or may not be
	// rejected depending on the Go stdlib version's ParseFloat; excluded
	// from assertions to keep the test stable across Go toolchains.
	bad := []string{"", "garbage", "1Pi", "-1g", "abc123g"}
	for _, in := range bad {
		if _, err := parseMemoryLimitMBStrict(in); err == nil {
			t.Errorf("parseMemoryLimitMBStrict(%q) expected error, got nil", in)
		}
	}
}

func TestIsValidPostgresPreset(t *testing.T) {
	for _, ok := range []string{"", "oltp", "olap", "tiny", "crit"} {
		if !IsValidPostgresPreset(ok) {
			t.Errorf("expected %q to be valid", ok)
		}
	}
	for _, bad := range []string{"hot", "OLTP", "fast", "default", " oltp"} {
		if IsValidPostgresPreset(bad) {
			t.Errorf("expected %q to be invalid", bad)
		}
	}
}

func TestApplyPostgresPreset_CustomReturnsSupplied(t *testing.T) {
	supplied := &EngineConfig{SharedBuffers: "777MB"}
	got, err := ApplyPostgresPreset(PgPresetCustom, "1g", supplied)
	if err != nil {
		t.Fatalf("custom preset: unexpected error %v", err)
	}
	if got != supplied {
		t.Errorf("custom preset must return supplied config unchanged, got %+v", got)
	}
}

// TestApplyPostgresPreset_OLTP_Scales verifies shared_buffers tracks memory
// proportionally without the unsafe 64MB floor that previously shipped.
func TestApplyPostgresPreset_OLTP_Scales(t *testing.T) {
	cases := []struct {
		mem     string
		wantSB  string
		wantWM  string
		wantECS string
	}{
		{"1g", "256MB", "4MB", "768MB"},    // 25% / 75%
		{"4g", "1024MB", "4MB", "3072MB"},
		{"256m", "64MB", "4MB", "192MB"},   // at the minimum memory floor
	}
	for _, tc := range cases {
		got, err := ApplyPostgresPreset(PgPresetOLTP, tc.mem, nil)
		if err != nil {
			t.Fatalf("OLTP[%s]: %v", tc.mem, err)
		}
		if got.SharedBuffers != tc.wantSB {
			t.Errorf("OLTP[%s].SharedBuffers = %s, want %s", tc.mem, got.SharedBuffers, tc.wantSB)
		}
		if got.WorkMem != tc.wantWM {
			t.Errorf("OLTP[%s].WorkMem = %s, want %s", tc.mem, got.WorkMem, tc.wantWM)
		}
		if got.EffectiveCacheSize != tc.wantECS {
			t.Errorf("OLTP[%s].EffectiveCacheSize = %s, want %s", tc.mem, got.EffectiveCacheSize, tc.wantECS)
		}
	}
}

func TestApplyPostgresPreset_OLAPHigherWorkMem(t *testing.T) {
	got, err := ApplyPostgresPreset(PgPresetOLAP, "8g", nil)
	if err != nil {
		t.Fatalf("OLAP[8g]: %v", err)
	}
	if got.WorkMem != "64MB" {
		t.Errorf("OLAP work_mem = %s, want 64MB", got.WorkMem)
	}
	mb, _ := strconv.Atoi(strings.TrimSuffix(got.SharedBuffers, "MB"))
	if mb < 3000 || mb > 3300 {
		t.Errorf("OLAP[8g] SharedBuffers ~3276MB expected, got %d MB", mb)
	}
	if got.MaxConnections != 50 {
		t.Errorf("OLAP MaxConnections = %d, want 50", got.MaxConnections)
	}
}

// TestApplyPostgresPreset_Tiny_ProportionalNoFloor verifies Tiny scales down
// to match low memory budgets instead of imposing a fixed floor that would
// exceed the container allocation.
func TestApplyPostgresPreset_Tiny_ProportionalNoFloor(t *testing.T) {
	got, err := ApplyPostgresPreset(PgPresetTiny, "128m", nil)
	if err != nil {
		t.Fatalf("Tiny[128m]: %v", err)
	}
	mb, _ := strconv.Atoi(strings.TrimSuffix(got.SharedBuffers, "MB"))
	if mb != 32 { // 128 / 4
		t.Errorf("Tiny[128m] SharedBuffers = %d MB, want 32 MB (proportional, no floor)", mb)
	}
}

// TestApplyPostgresPreset_Crit_EmitsDurabilityFields is a regression test
// for the Codex-flagged CRITICAL finding: Crit preset used to claim
// synchronous_commit/full_page_writes/fsync in its description but the
// derived EngineConfig emitted none of them.
func TestApplyPostgresPreset_Crit_EmitsDurabilityFields(t *testing.T) {
	got, err := ApplyPostgresPreset(PgPresetCrit, "1g", nil)
	if err != nil {
		t.Fatalf("Crit[1g]: %v", err)
	}
	if got.SynchronousCommit != "on" {
		t.Errorf("Crit SynchronousCommit = %q, want on", got.SynchronousCommit)
	}
	if got.FullPageWrites == nil || !*got.FullPageWrites {
		t.Error("Crit FullPageWrites must be true")
	}
	if got.Fsync == nil || !*got.Fsync {
		t.Error("Crit Fsync must be true")
	}
	if got.WorkMem != "8MB" {
		t.Errorf("Crit WorkMem = %s, want 8MB", got.WorkMem)
	}
}

// TestApplyPostgresPreset_BelowMinimumMemoryRejects verifies each preset
// errors when the memory budget is too small. Previously the code would
// silently emit shared_buffers values larger than the container allocation.
func TestApplyPostgresPreset_BelowMinimumMemoryRejects(t *testing.T) {
	cases := []struct {
		preset PostgresTuningPreset
		mem    string
	}{
		{PgPresetOLTP, "128m"},  // minMemOLTP=256
		{PgPresetOLAP, "256m"},  // minMemOLAP=512
		{PgPresetCrit, "128m"},  // minMemCrit=256
		{PgPresetTiny, "32m"},   // minMemTiny=64
	}
	for _, tc := range cases {
		_, err := ApplyPostgresPreset(tc.preset, tc.mem, nil)
		if err == nil {
			t.Errorf("%s[%s] should return error (below minimum), got nil", tc.preset, tc.mem)
		}
	}
}

// TestApplyPostgresPreset_UnparseableMemoryErrors is a regression test for
// the Codex-flagged MEDIUM finding: unsupported units now surface as errors
// instead of silently falling back to a 512MB budget.
func TestApplyPostgresPreset_UnparseableMemoryErrors(t *testing.T) {
	for _, bad := range []string{"garbage", "1Pi", "-1g"} {
		_, err := ApplyPostgresPreset(PgPresetOLTP, bad, nil)
		if err == nil {
			t.Errorf("memory=%q expected error, got nil", bad)
		}
	}
}

func TestListPostgresPresets_AllFour(t *testing.T) {
	presets := ListPostgresPresets()
	if len(presets) != 4 {
		t.Fatalf("expected 4 presets, got %d", len(presets))
	}
	ids := map[PostgresTuningPreset]bool{}
	for _, p := range presets {
		ids[p.ID] = true
		if p.Name == "" || p.Description == "" {
			t.Errorf("preset %s: Name/Description must be set", p.ID)
		}
		if p.MinMemoryMB <= 0 {
			t.Errorf("preset %s: MinMemoryMB must be > 0, got %d", p.ID, p.MinMemoryMB)
		}
	}
	for _, want := range []PostgresTuningPreset{PgPresetOLTP, PgPresetOLAP, PgPresetTiny, PgPresetCrit} {
		if !ids[want] {
			t.Errorf("missing preset %s", want)
		}
	}
}

// TestListPostgresPresets_CritDescriptionMatchesImplementation is a meta-test
// guarding the Codex-flagged CRITICAL finding from regressing: the Crit
// metadata description promises three durability settings; if ApplyPostgresPreset
// stops emitting one, this test must fail.
func TestListPostgresPresets_CritDescriptionMatchesImplementation(t *testing.T) {
	presets := ListPostgresPresets()
	var crit *PostgresPresetInfo
	for i := range presets {
		if presets[i].ID == PgPresetCrit {
			crit = &presets[i]
			break
		}
	}
	if crit == nil {
		t.Fatal("Crit preset missing from ListPostgresPresets")
	}
	cfg, err := ApplyPostgresPreset(PgPresetCrit, "1g", nil)
	if err != nil {
		t.Fatalf("Crit apply: %v", err)
	}
	// Walk description for each advertised setting and confirm emitted.
	if strings.Contains(crit.Description, "synchronous_commit") && cfg.SynchronousCommit == "" {
		t.Error("description advertises synchronous_commit but config omits it")
	}
	if strings.Contains(crit.Description, "full_page_writes") && cfg.FullPageWrites == nil {
		t.Error("description advertises full_page_writes but config omits it")
	}
	if strings.Contains(crit.Description, "fsync") && cfg.Fsync == nil {
		t.Error("description advertises fsync but config omits it")
	}
}
