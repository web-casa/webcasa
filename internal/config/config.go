package config

import (
	"os"
	"path/filepath"
)

// Config holds all application configuration
type Config struct {
	Port          string // Panel HTTP port
	DBPath        string // SQLite database path
	JWTSecret     string // JWT signing secret
	CaddyBin      string // Path to caddy binary
	CaddyfilePath string // Path to generated Caddyfile
	LogDir        string // Directory for Caddy logs
	DataDir       string // Data directory root
	AdminAPI      string // Caddy admin API URL
}

// Load reads configuration from environment variables with sensible defaults
func Load() *Config {
	dataDir := envOrDefault("WEBCASA_DATA_DIR", "./data")

	cfg := &Config{
		Port:          envOrDefault("WEBCASA_PORT", "39921"),
		DBPath:        envOrDefault("WEBCASA_DB_PATH", filepath.Join(dataDir, "webcasa.db")),
		JWTSecret:     envOrDefault("WEBCASA_JWT_SECRET", "webcasa-change-me-in-production"),
		CaddyBin:      envOrDefault("WEBCASA_CADDY_BIN", "caddy"),
		CaddyfilePath: envOrDefault("WEBCASA_CADDYFILE_PATH", filepath.Join(dataDir, "Caddyfile")),
		LogDir:        envOrDefault("WEBCASA_LOG_DIR", filepath.Join(dataDir, "logs")),
		DataDir:       dataDir,
		AdminAPI:      envOrDefault("WEBCASA_ADMIN_API", "http://localhost:2019"),
	}

	// Ensure directories exist
	os.MkdirAll(dataDir, 0755)
	os.MkdirAll(cfg.LogDir, 0755)
	os.MkdirAll(filepath.Join(dataDir, "backups"), 0755)

	return cfg
}

func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
