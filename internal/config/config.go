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
	dataDir := envOrDefault("CADDYPANEL_DATA_DIR", "./data")

	cfg := &Config{
		Port:          envOrDefault("CADDYPANEL_PORT", "8080"),
		DBPath:        envOrDefault("CADDYPANEL_DB_PATH", filepath.Join(dataDir, "caddypanel.db")),
		JWTSecret:     envOrDefault("CADDYPANEL_JWT_SECRET", "caddypanel-change-me-in-production"),
		CaddyBin:      envOrDefault("CADDYPANEL_CADDY_BIN", "caddy"),
		CaddyfilePath: envOrDefault("CADDYPANEL_CADDYFILE_PATH", filepath.Join(dataDir, "Caddyfile")),
		LogDir:        envOrDefault("CADDYPANEL_LOG_DIR", filepath.Join(dataDir, "logs")),
		DataDir:       dataDir,
		AdminAPI:      envOrDefault("CADDYPANEL_ADMIN_API", "http://localhost:2019"),
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
