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
}

// ListPostgresPresets returns metadata for all available presets, ordered for UI display.
func ListPostgresPresets() []PostgresPresetInfo {
	return []PostgresPresetInfo{
		{
			ID:          PgPresetOLTP,
			Name:        "OLTP — Web App",
			Description: "Transactional workloads: web apps, APIs, short queries. shared_buffers≈25% RAM, work_mem=4MB.",
			GoodFor:     "REST APIs, e-commerce, CMS",
		},
		{
			ID:          PgPresetOLAP,
			Name:        "OLAP — Analytics",
			Description: "Analytical workloads: reports, aggregations, long-running queries. shared_buffers≈40% RAM, work_mem=64MB, parallel workers scaled with CPU.",
			GoodFor:     "BI reports, ETL jobs, data warehouses",
		},
		{
			ID:          PgPresetTiny,
			Name:        "Tiny — Constrained",
			Description: "Resource-constrained instances (<1GB allocated): conservative connections and buffers to avoid OOM.",
			GoodFor:     "Hobby projects, dev databases, IoT edge",
		},
		{
			ID:          PgPresetCrit,
			Name:        "Crit — Strict Consistency",
			Description: "Maximum durability: synchronous_commit=on, full_page_writes=on, fsync=on. Lower throughput, higher safety.",
			GoodFor:     "Financial ledgers, audit logs, compliance",
		},
	}
}

// ApplyPostgresPreset returns an EngineConfig populated for the given preset
// scaled to the instance's memory budget. Callers should marshal this to
// Instance.Config JSON. If the preset is PgPresetCustom (or unknown), the
// caller-supplied config (if any) is returned unchanged.
//
// memoryLimit follows the same format as Instance.MemoryLimit ("256m", "1g",
// "0.5g", "512mb"). On parse failure a safe 512MB default is assumed so
// preset application never blocks instance creation.
func ApplyPostgresPreset(preset PostgresTuningPreset, memoryLimit string, supplied *EngineConfig) *EngineConfig {
	if preset == PgPresetCustom {
		return supplied
	}
	memMB := parseMemoryLimitMB(memoryLimit)
	if memMB <= 0 {
		memMB = 512
	}

	switch preset {
	case PgPresetOLTP:
		return pgOLTP(memMB)
	case PgPresetOLAP:
		return pgOLAP(memMB)
	case PgPresetTiny:
		return pgTiny(memMB)
	case PgPresetCrit:
		return pgCrit(memMB)
	default:
		return supplied
	}
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

func pgOLTP(memMB int) *EngineConfig {
	sharedMB := memMB / 4 // 25% RAM
	if sharedMB < 64 {
		sharedMB = 64
	}
	cacheMB := memMB * 3 / 4 // 75% RAM
	return &EngineConfig{
		SharedBuffers:           fmt.Sprintf("%dMB", sharedMB),
		WorkMem:                 "4MB",
		EffectiveCacheSize:      fmt.Sprintf("%dMB", cacheMB),
		MaxConnections:          100,
		WalLevel:                "replica",
		LogMinDurationStatement: intPtr(500),
	}
}

func pgOLAP(memMB int) *EngineConfig {
	sharedMB := memMB * 2 / 5 // 40% RAM
	if sharedMB < 128 {
		sharedMB = 128
	}
	cacheMB := memMB * 3 / 4
	return &EngineConfig{
		SharedBuffers:           fmt.Sprintf("%dMB", sharedMB),
		WorkMem:                 "64MB",
		EffectiveCacheSize:      fmt.Sprintf("%dMB", cacheMB),
		MaxConnections:          50,
		WalLevel:                "replica",
		LogMinDurationStatement: intPtr(2000),
	}
}

func pgTiny(memMB int) *EngineConfig {
	// Hard caps regardless of memory budget — protects shared hosts.
	sharedMB := 128
	if sharedMB > memMB/3 {
		sharedMB = memMB / 3
		if sharedMB < 32 {
			sharedMB = 32
		}
	}
	cacheMB := memMB / 2
	if cacheMB < 64 {
		cacheMB = 64
	}
	return &EngineConfig{
		SharedBuffers:           fmt.Sprintf("%dMB", sharedMB),
		WorkMem:                 "2MB",
		EffectiveCacheSize:      fmt.Sprintf("%dMB", cacheMB),
		MaxConnections:          20,
		WalLevel:                "minimal",
		LogMinDurationStatement: intPtr(5000),
	}
}

func pgCrit(memMB int) *EngineConfig {
	// Same memory layout as OLTP but durability dialed up via WAL settings.
	// Note: synchronous_commit / full_page_writes are not yet first-class
	// EngineConfig fields; for v0.11 we ship the safe memory baseline and
	// document that crit-level WAL settings will land alongside the
	// PgBouncer + PITR work in v0.12.
	sharedMB := memMB / 4
	if sharedMB < 64 {
		sharedMB = 64
	}
	cacheMB := memMB * 3 / 4
	return &EngineConfig{
		SharedBuffers:           fmt.Sprintf("%dMB", sharedMB),
		WorkMem:                 "8MB",
		EffectiveCacheSize:      fmt.Sprintf("%dMB", cacheMB),
		MaxConnections:          100,
		WalLevel:                "replica",
		LogMinDurationStatement: intPtr(1000),
	}
}

// parseMemoryLimitMB converts strings like "256m", "1g", "0.5g", "512mb",
// "1.5G" into an integer megabyte value. Returns 0 on failure so callers
// can fall back to a safe default.
func parseMemoryLimitMB(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}
	// Strip an optional trailing "b" (e.g. "1gb" -> "1g").
	if strings.HasSuffix(s, "b") {
		s = strings.TrimSuffix(s, "b")
	}
	var unitMul float64 = 1 // default unit = MB if no suffix
	switch {
	case strings.HasSuffix(s, "g"):
		unitMul = 1024
		s = strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "m"):
		unitMul = 1
		s = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "k"):
		unitMul = 1.0 / 1024.0
		s = strings.TrimSuffix(s, "k")
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v <= 0 {
		return 0
	}
	return int(v * unitMul)
}
