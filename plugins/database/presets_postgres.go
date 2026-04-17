package database

import (
	"fmt"
	"strconv"
	"strings"
)

// PostgresTuningPreset names a workload-aware PostgreSQL tuning preset.
// Selected at instance creation time and stored on the Instance for audit
// and possible re-application after a memory-limit change.
type PostgresTuningPreset string

const (
	PgPresetCustom PostgresTuningPreset = ""     // user-supplied EngineConfig, no auto-tuning
	PgPresetOLTP   PostgresTuningPreset = "oltp" // transactional web apps
	PgPresetOLAP   PostgresTuningPreset = "olap" // analytics / long-running queries
	PgPresetTiny   PostgresTuningPreset = "tiny" // resource-constrained nodes (<2GB)
	PgPresetCrit   PostgresTuningPreset = "crit" // strict consistency (financial / audit)
)

// PostgresPresetInfo describes a preset for UI / API consumption.
type PostgresPresetInfo struct {
	ID          PostgresTuningPreset `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	GoodFor     string               `json:"good_for"`
	// MinMemoryMB is the minimum instance memory budget a preset supports.
	// Below this, shared_buffers would exceed a safe fraction of physical
	// memory (accounting for the Postgres process itself and max_connections
	// overhead) and PG would fail to start or immediately OOM.
	MinMemoryMB int `json:"min_memory_mb"`
}

// Minimum instance memory (MB) required for each preset. Enforced in
// ApplyPostgresPreset; below these thresholds the caller gets an error
// rather than a silently-broken container.
const (
	minMemOLTP = 256
	minMemOLAP = 512
	minMemCrit = 256
	minMemTiny = 64
)

// ListPostgresPresets returns metadata for all available presets, ordered for UI display.
func ListPostgresPresets() []PostgresPresetInfo {
	return []PostgresPresetInfo{
		{
			ID:          PgPresetOLTP,
			Name:        "OLTP — Web App",
			Description: "Transactional workloads: web apps, APIs, short queries. shared_buffers≈25% RAM, work_mem=4MB.",
			GoodFor:     "REST APIs, e-commerce, CMS",
			MinMemoryMB: minMemOLTP,
		},
		{
			ID:          PgPresetOLAP,
			Name:        "OLAP — Analytics",
			Description: "Analytical workloads: reports, aggregations, long-running queries. shared_buffers≈40% RAM, work_mem=64MB.",
			GoodFor:     "BI reports, ETL jobs, data warehouses",
			MinMemoryMB: minMemOLAP,
		},
		{
			ID:          PgPresetTiny,
			Name:        "Tiny — Constrained",
			Description: "Resource-constrained instances: conservative connections and buffers to avoid OOM. Sized proportionally to memory — no hard floor.",
			GoodFor:     "Hobby projects, dev databases, IoT edge",
			MinMemoryMB: minMemTiny,
		},
		{
			ID:          PgPresetCrit,
			Name:        "Crit — Strict Consistency",
			Description: "Maximum durability: synchronous_commit=on, full_page_writes=on, fsync=on. Lower throughput, higher safety.",
			GoodFor:     "Financial ledgers, audit logs, compliance",
			MinMemoryMB: minMemCrit,
		},
	}
}

// ApplyPostgresPreset returns an EngineConfig populated for the given preset
// scaled to the instance's memory budget, or an error if the memory budget
// is too small or unparseable. Callers should marshal the returned config
// to Instance.Config JSON. If preset is PgPresetCustom, the caller-supplied
// config is returned unchanged.
//
// memoryLimit follows the Instance.MemoryLimit format ("256m", "1g",
// "0.5g", "512mb", "1Gi"). An unparseable value is rejected rather than
// silently falling back to a 512MB default — that behaviour previously
// concealed misconfiguration.
//
// Per-preset memory minimums are enforced (see MinMemoryMB in the preset
// metadata). Below the minimum, Postgres would either refuse to start or
// exhaust memory because shared_buffers + max_connections overhead would
// exceed the physical container allocation.
func ApplyPostgresPreset(preset PostgresTuningPreset, memoryLimit string, supplied *EngineConfig) (*EngineConfig, error) {
	if preset == PgPresetCustom {
		return supplied, nil
	}
	memMB, err := parseMemoryLimitMBStrict(memoryLimit)
	if err != nil {
		return nil, fmt.Errorf("tuning preset %q: cannot parse memory_limit %q: %w", preset, memoryLimit, err)
	}

	var cfg *EngineConfig
	var min int
	switch preset {
	case PgPresetOLTP:
		min = minMemOLTP
		if memMB >= min {
			cfg = pgOLTP(memMB)
		}
	case PgPresetOLAP:
		min = minMemOLAP
		if memMB >= min {
			cfg = pgOLAP(memMB)
		}
	case PgPresetTiny:
		min = minMemTiny
		if memMB >= min {
			cfg = pgTiny(memMB)
		}
	case PgPresetCrit:
		min = minMemCrit
		if memMB >= min {
			cfg = pgCrit(memMB)
		}
	default:
		return supplied, nil
	}

	if cfg == nil {
		return nil, fmt.Errorf("tuning preset %q requires at least %d MB memory (got %d MB); increase memory_limit or pick a lighter preset", preset, min, memMB)
	}
	return cfg, nil
}

// IsValidPostgresPreset reports whether s names a known preset (or empty for custom).
func IsValidPostgresPreset(s string) bool {
	switch PostgresTuningPreset(s) {
	case PgPresetCustom, PgPresetOLTP, PgPresetOLAP, PgPresetTiny, PgPresetCrit:
		return true
	}
	return false
}

// ── Preset bodies ──
//
// Each preset sizes shared_buffers as a proportion of the memory budget with
// NO fixed floor. The per-preset minimum memory check in ApplyPostgresPreset
// guarantees memMB is large enough that proportional math yields safe values.

func pgOLTP(memMB int) *EngineConfig {
	return &EngineConfig{
		SharedBuffers:           fmt.Sprintf("%dMB", memMB/4),     // 25% RAM
		WorkMem:                 "4MB",
		EffectiveCacheSize:      fmt.Sprintf("%dMB", memMB*3/4),   // 75% RAM (OS cache hint, not allocated)
		MaxConnections:          100,
		WalLevel:                "replica",
		LogMinDurationStatement: intPtr(500),
	}
}

func pgOLAP(memMB int) *EngineConfig {
	return &EngineConfig{
		SharedBuffers:           fmt.Sprintf("%dMB", memMB*2/5),   // 40% RAM
		WorkMem:                 "64MB",
		EffectiveCacheSize:      fmt.Sprintf("%dMB", memMB*3/4),
		MaxConnections:          50,
		WalLevel:                "replica",
		LogMinDurationStatement: intPtr(2000),
	}
}

func pgTiny(memMB int) *EngineConfig {
	return &EngineConfig{
		SharedBuffers:           fmt.Sprintf("%dMB", memMB/4),
		WorkMem:                 "2MB",
		EffectiveCacheSize:      fmt.Sprintf("%dMB", memMB/2),
		MaxConnections:          20,
		WalLevel:                "minimal",
		LogMinDurationStatement: intPtr(5000),
	}
}

func pgCrit(memMB int) *EngineConfig {
	// Same memory layout as OLTP with durability dialed up:
	// synchronous_commit=on forces WAL flush before ack,
	// full_page_writes=on protects against partial-write corruption,
	// fsync=on requires OS-level fsync on commit.
	// Lower throughput, strong durability. Use for financial ledgers,
	// audit trails, compliance-governed datasets.
	t := true
	return &EngineConfig{
		SharedBuffers:           fmt.Sprintf("%dMB", memMB/4),
		WorkMem:                 "8MB",
		EffectiveCacheSize:      fmt.Sprintf("%dMB", memMB*3/4),
		MaxConnections:          100,
		WalLevel:                "replica",
		LogMinDurationStatement: intPtr(1000),
		SynchronousCommit:       "on",
		FullPageWrites:          &t,
		Fsync:                   &t,
	}
}

// parseMemoryLimitMB converts strings like "256m", "1g", "0.5g", "512mb",
// "1.5G", "1Gi", "2Ti" into an integer megabyte value. Returns 0 on
// unparseable / non-positive input. Used by the lenient legacy callers
// (test fixtures, ad-hoc). For preset application use
// parseMemoryLimitMBStrict which propagates the exact parse failure reason.
func parseMemoryLimitMB(s string) int {
	v, _ := parseMemoryLimitMBStrict(s)
	return v
}

// parseMemoryLimitMBStrict is the parser used by preset application. Returns
// a descriptive error on unknown/unsupported units so misconfiguration
// surfaces as an explicit 400 instead of being silently coerced to 512MB.
//
// Supported units (case-insensitive, whitespace tolerated):
//   - plain (no suffix): interpreted as MB
//   - k / kb:   kilobytes (binary, 1/1024 MB)
//   - m / mb:   megabytes
//   - g / gb:   gigabytes
//   - t / tb:   terabytes
//   - ki:       IEC kibibytes (same as k here)
//   - mi:       IEC mebibytes
//   - gi:       IEC gibibytes
//   - ti:       IEC tebibytes
//
// Fractional values are accepted (e.g. "0.5g" -> 512).
func parseMemoryLimitMBStrict(s string) (int, error) {
	orig := s
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}

	// Ordered longest-suffix-first so "gi"/"mi"/"gb"/"mb" beat "g"/"m"/"b".
	type unit struct {
		suffix string
		mul    float64 // multiplier to MB
	}
	units := []unit{
		{"ti", 1024 * 1024},
		{"gi", 1024},
		{"mi", 1},
		{"ki", 1.0 / 1024.0},
		{"tb", 1024 * 1024},
		{"gb", 1024},
		{"mb", 1},
		{"kb", 1.0 / 1024.0},
		{"t", 1024 * 1024},
		{"g", 1024},
		{"m", 1},
		{"k", 1.0 / 1024.0},
	}
	mul := 1.0 // default: MB
	matched := false
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			mul = u.mul
			s = strings.TrimSuffix(s, u.suffix)
			matched = true
			break
		}
	}

	s = strings.TrimSpace(s)
	// If the trailing character is a letter, it's an unsupported unit —
	// refuse rather than silently treating as MB.
	if !matched && len(s) > 0 {
		last := s[len(s)-1]
		if (last >= 'a' && last <= 'z') || last == 'i' {
			return 0, fmt.Errorf("unsupported unit suffix in %q", orig)
		}
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", orig)
	}
	if v <= 0 {
		return 0, fmt.Errorf("non-positive value %q", orig)
	}
	return int(v * mul), nil
}
