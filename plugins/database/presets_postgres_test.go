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
		{"768K", 0}, // 768 KB rounds to 0 MB; treated as parse failure
		{"garbage", 0},
		{"-1g", 0},
	}
	for _, tc := range cases {
		if got := parseMemoryLimitMB(tc.in); got != tc.want {
			t.Errorf("parseMemoryLimitMB(%q) = %d, want %d", tc.in, got, tc.want)
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
	got := ApplyPostgresPreset(PgPresetCustom, "1g", supplied)
	if got != supplied {
		t.Errorf("custom preset must return supplied config unchanged, got %+v", got)
	}
}

func TestApplyPostgresPreset_OLTPScalesWithMemory(t *testing.T) {
	cases := []struct {
		mem     string
		wantSB  string
		wantWM  string
		wantECS string
		wantMC  int
	}{
		{"1g", "256MB", "4MB", "768MB", 100},      // 25% / 75%
		{"4g", "1024MB", "4MB", "3072MB", 100},
		{"128m", "64MB", "4MB", "96MB", 100},      // 25% would be 32 -> floor at 64
	}
	for _, tc := range cases {
		got := ApplyPostgresPreset(PgPresetOLTP, tc.mem, nil)
		if got == nil {
			t.Fatalf("OLTP[%s] returned nil", tc.mem)
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
		if got.MaxConnections != tc.wantMC {
			t.Errorf("OLTP[%s].MaxConnections = %d, want %d", tc.mem, got.MaxConnections, tc.wantMC)
		}
	}
}

func TestApplyPostgresPreset_OLAPHigherWorkMem(t *testing.T) {
	got := ApplyPostgresPreset(PgPresetOLAP, "8g", nil)
	if got.WorkMem != "64MB" {
		t.Errorf("OLAP work_mem = %s, want 64MB (analytic queries need bigger sort/hash buffers)", got.WorkMem)
	}
	// 40% of 8GB = ~3276MB
	if !strings.HasSuffix(got.SharedBuffers, "MB") {
		t.Errorf("OLAP SharedBuffers should be in MB, got %s", got.SharedBuffers)
	}
	mb, _ := strconv.Atoi(strings.TrimSuffix(got.SharedBuffers, "MB"))
	if mb < 3000 || mb > 3300 {
		t.Errorf("OLAP[8g] SharedBuffers ~3276MB expected, got %d MB", mb)
	}
	if got.MaxConnections != 50 {
		t.Errorf("OLAP MaxConnections = %d, want 50 (fewer concurrent for heavier queries)", got.MaxConnections)
	}
}

func TestApplyPostgresPreset_TinyHardCaps(t *testing.T) {
	// Tiny on a generous budget should still be conservative.
	got := ApplyPostgresPreset(PgPresetTiny, "8g", nil)
	mb, _ := strconv.Atoi(strings.TrimSuffix(got.SharedBuffers, "MB"))
	if mb > 256 {
		t.Errorf("Tiny SharedBuffers should be capped low, got %d MB", mb)
	}
	if got.MaxConnections != 20 {
		t.Errorf("Tiny MaxConnections = %d, want 20", got.MaxConnections)
	}
	if got.WalLevel != "minimal" {
		t.Errorf("Tiny WalLevel = %s, want minimal (skip replication overhead)", got.WalLevel)
	}
}

func TestApplyPostgresPreset_FallsBackOnUnparseableMemory(t *testing.T) {
	// Bad memoryLimit must not cause nil panic; should produce a usable config.
	got := ApplyPostgresPreset(PgPresetOLTP, "garbage", nil)
	if got == nil {
		t.Fatal("ApplyPostgresPreset must not return nil even on unparseable memory")
	}
	if got.SharedBuffers == "" {
		t.Error("expected non-empty SharedBuffers from fallback memory budget")
	}
}

func TestListPostgresPresets_AllFour(t *testing.T) {
	presets := ListPostgresPresets()
	if len(presets) != 4 {
		t.Fatalf("expected 4 presets (oltp/olap/tiny/crit), got %d", len(presets))
	}
	ids := map[PostgresTuningPreset]bool{}
	for _, p := range presets {
		ids[p.ID] = true
		if p.Name == "" || p.Description == "" {
			t.Errorf("preset %s: Name/Description must be set", p.ID)
		}
	}
	for _, want := range []PostgresTuningPreset{PgPresetOLTP, PgPresetOLAP, PgPresetTiny, PgPresetCrit} {
		if !ids[want] {
			t.Errorf("missing preset %s", want)
		}
	}
}
