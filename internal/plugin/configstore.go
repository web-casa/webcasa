package plugin

import (
	"fmt"

	"github.com/web-casa/webcasa/internal/model"
	"gorm.io/gorm"
)

// ConfigStore provides scoped key-value configuration for a single plugin.
// Keys are stored in the shared settings table with the prefix "plugin.{id}.".
type ConfigStore struct {
	db     *gorm.DB
	prefix string // "plugin.docker." etc.
}

// NewConfigStore creates a ConfigStore scoped to the given plugin ID.
func NewConfigStore(db *gorm.DB, pluginID string) *ConfigStore {
	return &ConfigStore{
		db:     db,
		prefix: fmt.Sprintf("plugin.%s.", pluginID),
	}
}

// Get reads a configuration value. Returns empty string if not found.
func (cs *ConfigStore) Get(key string) string {
	var s model.Setting
	if err := cs.db.Where("key = ?", cs.prefix+key).First(&s).Error; err != nil {
		return ""
	}
	return s.Value
}

// Set writes a configuration value (upsert).
func (cs *ConfigStore) Set(key, value string) error {
	fullKey := cs.prefix + key
	return cs.db.Where("key = ?", fullKey).
		Assign(model.Setting{Key: fullKey, Value: value}).
		FirstOrCreate(&model.Setting{}).Error
}

// Delete removes a configuration value.
func (cs *ConfigStore) Delete(key string) error {
	return cs.db.Where("key = ?", cs.prefix+key).Delete(&model.Setting{}).Error
}

// All returns all configuration values for this plugin as a map.
func (cs *ConfigStore) All() map[string]string {
	var settings []model.Setting
	cs.db.Where("key LIKE ?", cs.prefix+"%").Find(&settings)

	result := make(map[string]string, len(settings))
	prefixLen := len(cs.prefix)
	for _, s := range settings {
		result[s.Key[prefixLen:]] = s.Value
	}
	return result
}
