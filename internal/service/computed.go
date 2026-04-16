package service

import (
	"encoding/json"

	"github.com/web-casa/webcasa/internal/model"
	"gorm.io/gorm"
)

// ComputedDefaults holds built-in default values for computed properties.
var ComputedDefaults = map[string]string{
	"max_body_size":         "100m",
	"proxy_read_timeout":    "60s",
	"proxy_connect_timeout": "10s",
	"hsts_max_age":          "31536000",
	"access_log_format":     "",
	"proxy_buffer_size":     "",
	"proxy_buffers":         "",
}

// ComputedValue resolves a configuration value using 3-tier fallback:
//  1. Host-level override (from host.config_overrides JSON)
//  2. Global setting (from settings table)
//  3. Built-in default
//
// Returns the resolved value and which tier it came from ("host", "global", or "default").
func ComputedValue(db *gorm.DB, hostID uint, key string) (string, string) {
	// Tier 1: Host-level override
	if hostID > 0 {
		var host model.Host
		if db.Select("config_overrides").First(&host, hostID).Error == nil && host.ConfigOverrides != "" {
			var overrides map[string]string
			if json.Unmarshal([]byte(host.ConfigOverrides), &overrides) == nil {
				if v, ok := overrides[key]; ok && v != "" {
					return v, "host"
				}
			}
		}
	}

	// Tier 2: Global setting
	var setting model.Setting
	settingKey := "caddy." + key
	if db.Where("key = ?", settingKey).First(&setting).Error == nil && setting.Value != "" {
		return setting.Value, "global"
	}

	// Tier 3: Built-in default
	if v, ok := ComputedDefaults[key]; ok {
		return v, "default"
	}
	return "", "default"
}

// ComputedValueSimple is a convenience wrapper that returns just the value.
func ComputedValueSimple(db *gorm.DB, hostID uint, key string) string {
	v, _ := ComputedValue(db, hostID, key)
	return v
}

// SetHostOverride sets a host-level configuration override.
func SetHostOverride(db *gorm.DB, hostID uint, key, value string) error {
	var host model.Host
	if err := db.Select("id, config_overrides").First(&host, hostID).Error; err != nil {
		return err
	}

	overrides := make(map[string]string)
	if host.ConfigOverrides != "" {
		json.Unmarshal([]byte(host.ConfigOverrides), &overrides)
	}

	if value == "" {
		delete(overrides, key)
	} else {
		overrides[key] = value
	}

	data, _ := json.Marshal(overrides)
	return db.Model(&model.Host{}).Where("id = ?", hostID).Update("config_overrides", string(data)).Error
}
