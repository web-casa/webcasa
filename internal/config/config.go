package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
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

	// Ensure directories exist early so we can write the secret file.
	// 0700: the data dir holds the SQLite DB, JWT secret and encrypted
	// credentials, so it must not be world-readable or world-traversable.
	os.MkdirAll(dataDir, 0700)

	cfg := &Config{
		Port:          envOrDefault("WEBCASA_PORT", "39921"),
		DBPath:        envOrDefault("WEBCASA_DB_PATH", filepath.Join(dataDir, "webcasa.db")),
		JWTSecret:     resolveJWTSecret(dataDir),
		CaddyBin:      envOrDefault("WEBCASA_CADDY_BIN", "caddy"),
		CaddyfilePath: envOrDefault("WEBCASA_CADDYFILE_PATH", filepath.Join(dataDir, "Caddyfile")),
		LogDir:        envOrDefault("WEBCASA_LOG_DIR", filepath.Join(dataDir, "logs")),
		DataDir:       dataDir,
		AdminAPI:      envOrDefault("WEBCASA_ADMIN_API", "http://localhost:2019"),
	}

	// Ensure directories exist (0700: not world-readable; backups may contain
	// exported credentials).
	os.MkdirAll(cfg.LogDir, 0700)
	os.MkdirAll(filepath.Join(dataDir, "backups"), 0700)

	// Best-effort tightening of the DB file permissions to owner-only. The DB is
	// opened by the database package; if it already exists, ensure it is 0600.
	if _, err := os.Stat(cfg.DBPath); err == nil {
		os.Chmod(cfg.DBPath, 0600)
	}

	return cfg
}

// resolveJWTSecret determines the JWT secret using this priority:
//  1. WEBCASA_JWT_SECRET env var (if set and not an insecure default)
//  2. Persisted secret in data/.jwt_secret
//  3. Auto-generate a new cryptographic random secret and persist it
func resolveJWTSecret(dataDir string) string {
	// Known insecure defaults that must be rejected.
	insecureDefaults := map[string]bool{
		"webcasa-change-me-in-production": true,
		"change-me-in-production":         true,
		"CHANGE_ME_TO_RANDOM_STRING":      true, // placeholder shipped in scripts/webcasa.env
	}

	// 1. Explicit env var takes precedence (if not an insecure default)
	if envSecret := os.Getenv("WEBCASA_JWT_SECRET"); envSecret != "" && !insecureDefaults[envSecret] {
		return envSecret
	}

	// 2. Try to load persisted secret
	secretFile := filepath.Join(dataDir, ".jwt_secret")
	if data, err := os.ReadFile(secretFile); err == nil {
		secret := strings.TrimSpace(string(data))
		if secret != "" {
			return secret
		}
	}

	// 3. Generate a cryptographically random secret and persist it
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		log.Fatalf("FATAL: failed to generate JWT secret: %v", err)
	}
	secret := hex.EncodeToString(secretBytes)

	if err := os.WriteFile(secretFile, []byte(secret+"\n"), 0600); err != nil {
		log.Printf("⚠️  Could not persist JWT secret to %s: %v", secretFile, err)
		log.Printf("   Set WEBCASA_JWT_SECRET env var to ensure stable sessions across restarts.")
	} else {
		log.Printf("🔑 Generated new JWT secret and saved to %s", secretFile)
	}

	return secret
}

func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
