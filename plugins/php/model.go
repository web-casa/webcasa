package php

import "time"

// RuntimeType defines the PHP runtime type.
type RuntimeType string

const (
	RuntimeFPM     RuntimeType = "fpm"
	RuntimeFranken RuntimeType = "frankenphp"
)

// PHPRuntime represents a Docker-managed PHP runtime (FPM or FrankenPHP).
type PHPRuntime struct {
	ID            uint        `gorm:"primaryKey" json:"id"`
	Version       string      `gorm:"size:16;not null" json:"version"`
	Type          RuntimeType `gorm:"size:16;not null" json:"type"`
	Status        string      `gorm:"size:16;default:stopped" json:"status"`
	Port          int         `gorm:"not null;uniqueIndex" json:"port"`
	ContainerName string      `gorm:"size:128" json:"container_name"`
	DataDir       string      `gorm:"size:512" json:"data_dir"`
	Extensions    string      `gorm:"type:text" json:"extensions"`     // JSON []string
	MemoryLimit   string      `gorm:"size:32;default:256m" json:"memory_limit"`
	PHPConfig     string      `gorm:"type:text" json:"php_config"`     // JSON PHPIniConfig
	FPMConfig     string      `gorm:"type:text" json:"fpm_config"`     // JSON FPMPoolConfig
	CustomImage   string      `gorm:"size:256" json:"custom_image"`    // built image name
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

func (PHPRuntime) TableName() string { return "plugin_php_runtimes" }

// PHPSite represents a PHP website linked to a runtime.
type PHPSite struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Name          string    `gorm:"uniqueIndex;not null;size:128" json:"name"`
	Domain        string    `gorm:"size:255" json:"domain"`
	RootPath      string    `gorm:"size:512;not null" json:"root_path"`
	RuntimeID     uint      `gorm:"index" json:"runtime_id"`     // FK to FPM runtime (0 for FrankenPHP)
	PHPVersion    string    `gorm:"size:16" json:"php_version"`
	RuntimeType   string    `gorm:"size:16" json:"runtime_type"` // "fpm" | "frankenphp"
	HostID        uint      `json:"host_id"`
	Port          int       `json:"port"`                        // FrankenPHP container port
	ContainerName string    `gorm:"size:128" json:"container_name"`
	DataDir       string    `gorm:"size:512" json:"data_dir"`
	WorkerMode    bool      `json:"worker_mode"`
	WorkerScript  string    `gorm:"size:512" json:"worker_script"`
	Extensions    string    `gorm:"type:text" json:"extensions"` // FrankenPHP extensions JSON
	Status        string    `gorm:"size:16;default:active" json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (PHPSite) TableName() string { return "plugin_php_sites" }

// ── Version definitions ──

// VersionInfo describes an available PHP version.
type VersionInfo struct {
	Version string      `json:"version"`
	Type    RuntimeType `json:"type"`
	Image   string      `json:"image"`
	Default bool        `json:"default,omitempty"`
	EOL     bool        `json:"eol,omitempty"`
}

// SupportedVersions lists all installable PHP versions.
var SupportedVersions = []VersionInfo{
	// Traditional PHP-FPM
	{Version: "8.4", Type: RuntimeFPM, Image: "php:8.4-fpm-alpine", Default: true},
	{Version: "8.3", Type: RuntimeFPM, Image: "php:8.3-fpm-alpine"},
	{Version: "8.2", Type: RuntimeFPM, Image: "php:8.2-fpm-alpine"},
	{Version: "8.1", Type: RuntimeFPM, Image: "php:8.1-fpm-alpine"},
	{Version: "8.0", Type: RuntimeFPM, Image: "php:8.0-fpm-alpine", EOL: true},
	{Version: "7.4", Type: RuntimeFPM, Image: "php:7.4-fpm-alpine", EOL: true},
	// FrankenPHP (8.2+)
	{Version: "8.4", Type: RuntimeFranken, Image: "dunglas/frankenphp:latest-php8.4-alpine", Default: true},
	{Version: "8.3", Type: RuntimeFranken, Image: "dunglas/frankenphp:latest-php8.3-alpine"},
	{Version: "8.2", Type: RuntimeFranken, Image: "dunglas/frankenphp:latest-php8.2-alpine"},
}

// FindVersion looks up a version by version string and runtime type.
func FindVersion(version string, rt RuntimeType) *VersionInfo {
	for _, v := range SupportedVersions {
		if v.Version == version && v.Type == rt {
			return &v
		}
	}
	return nil
}

// ── PHP Configuration ──

// PHPIniConfig holds structured php.ini settings.
type PHPIniConfig struct {
	// Resource limits
	MemoryLimit      string `json:"memory_limit"`       // default "256M"
	MaxExecutionTime int    `json:"max_execution_time"` // default 30
	MaxInputTime     int    `json:"max_input_time"`     // default 60
	MaxInputVars     int    `json:"max_input_vars"`     // default 1000
	// Upload
	UploadMaxFilesize string `json:"upload_max_filesize"` // default "64M"
	PostMaxSize       string `json:"post_max_size"`       // default "128M"
	MaxFileUploads    int    `json:"max_file_uploads"`    // default 20
	// Error handling
	DisplayErrors  bool   `json:"display_errors"`  // default false
	ErrorReporting string `json:"error_reporting"` // default "E_ALL & ~E_DEPRECATED"
	LogErrors      bool   `json:"log_errors"`      // default true
	// Session
	SessionGcMaxlifetime int `json:"session_gc_maxlifetime"` // default 1440
	// OPcache
	OpcacheEnable     bool   `json:"opcache_enable"`      // default true
	OpcacheMemory     string `json:"opcache_memory"`      // default "128"
	OpcacheMaxFiles   int    `json:"opcache_max_files"`   // default 10000
	OpcacheRevalidate int    `json:"opcache_revalidate"`  // default 2 (seconds)
	// Timezone
	DateTimezone string `json:"date_timezone"` // default system timezone
	// Advanced
	CustomDirectives string `json:"custom_directives"` // raw php.ini fragment
}

// DefaultPHPIniConfig returns sensible defaults.
func DefaultPHPIniConfig() PHPIniConfig {
	return PHPIniConfig{
		MemoryLimit:          "256M",
		MaxExecutionTime:     30,
		MaxInputTime:         60,
		MaxInputVars:         1000,
		UploadMaxFilesize:    "64M",
		PostMaxSize:          "128M",
		MaxFileUploads:       20,
		DisplayErrors:        false,
		ErrorReporting:       "E_ALL & ~E_DEPRECATED",
		LogErrors:            true,
		SessionGcMaxlifetime: 1440,
		OpcacheEnable:        true,
		OpcacheMemory:        "128",
		OpcacheMaxFiles:      10000,
		OpcacheRevalidate:    2,
		DateTimezone:         "UTC",
	}
}

// FPMPoolConfig holds PHP-FPM process manager settings.
type FPMPoolConfig struct {
	PM              string `json:"pm"`                // "dynamic" | "static" | "ondemand"
	MaxChildren     int    `json:"max_children"`
	StartServers    int    `json:"start_servers"`
	MinSpareServers int    `json:"min_spare_servers"`
	MaxSpareServers int    `json:"max_spare_servers"`
	MaxRequests     int    `json:"max_requests"`  // default 500
	IdleTimeout     int    `json:"idle_timeout"`  // ondemand mode, seconds
}

// DefaultFPMPoolConfig returns sensible defaults.
func DefaultFPMPoolConfig() FPMPoolConfig {
	return FPMPoolConfig{
		PM:              "dynamic",
		MaxChildren:     10,
		StartServers:    4,
		MinSpareServers: 2,
		MaxSpareServers: 4,
		MaxRequests:     500,
		IdleTimeout:     10,
	}
}

// ── Extension definitions ──

// ExtensionInfo describes a PHP extension.
type ExtensionInfo struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Category string   `json:"category"` // database, image, i18n, compression, performance, cache, math, debug
	PECL     bool     `json:"pecl,omitempty"`
	Deps     []string `json:"deps,omitempty"` // Alpine apk dependencies
}

// CommonExtensions lists frequently used PHP extensions.
var CommonExtensions = []ExtensionInfo{
	// Database
	{Name: "pdo_mysql", Label: "PDO MySQL", Category: "database"},
	{Name: "mysqli", Label: "MySQLi", Category: "database"},
	{Name: "pdo_pgsql", Label: "PDO PostgreSQL", Category: "database", Deps: []string{"postgresql-dev"}},
	// Image
	{Name: "gd", Label: "GD", Category: "image", Deps: []string{"freetype-dev", "libjpeg-turbo-dev", "libpng-dev"}},
	{Name: "imagick", Label: "ImageMagick", Category: "image", PECL: true, Deps: []string{"imagemagick-dev"}},
	// Internationalization
	{Name: "intl", Label: "Intl", Category: "i18n", Deps: []string{"icu-dev"}},
	{Name: "mbstring", Label: "Multibyte String", Category: "i18n"},
	// Compression
	{Name: "zip", Label: "Zip", Category: "compression", Deps: []string{"libzip-dev"}},
	// Performance
	{Name: "opcache", Label: "OPcache", Category: "performance"},
	// Cache
	{Name: "redis", Label: "Redis", Category: "cache", PECL: true},
	{Name: "memcached", Label: "Memcached", Category: "cache", PECL: true, Deps: []string{"libmemcached-dev", "zlib-dev"}},
	// Math / Crypto
	{Name: "bcmath", Label: "BCMath", Category: "math"},
	// Debug
	{Name: "xdebug", Label: "Xdebug", Category: "debug", PECL: true},
}

// ── Request types ──

// CreateRuntimeRequest is the input for installing a PHP runtime.
type CreateRuntimeRequest struct {
	Version     string      `json:"version" binding:"required"`
	Type        RuntimeType `json:"type" binding:"required"`
	Extensions  []string    `json:"extensions"`
	MemoryLimit string      `json:"memory_limit"`
	AutoStart   bool        `json:"auto_start"`
}

// CreateSiteRequest is the input for creating a PHP site.
type CreateSiteRequest struct {
	Name         string `json:"name" binding:"required"`
	Domain       string `json:"domain" binding:"required"`
	PHPVersion   string `json:"php_version" binding:"required"`
	RuntimeType  string `json:"runtime_type" binding:"required"` // "fpm" | "frankenphp"
	RuntimeID    uint   `json:"runtime_id"`                      // required for FPM
	RootPath     string `json:"root_path"`                       // default /var/www/{domain}
	WorkerMode   bool   `json:"worker_mode"`                     // FrankenPHP only
	WorkerScript string `json:"worker_script"`                   // FrankenPHP worker script path
	Extensions   []string `json:"extensions"`                    // FrankenPHP per-site extensions
	TLSEnabled   bool   `json:"tls_enabled"`
	HTTPRedirect bool   `json:"http_redirect"`
}

// UpdateConfigRequest is the input for updating runtime config.
type UpdateConfigRequest struct {
	PHPConfig *PHPIniConfig  `json:"php_config,omitempty"`
	FPMConfig *FPMPoolConfig `json:"fpm_config,omitempty"`
}

// InstallExtensionRequest is the input for installing an extension.
type InstallExtensionRequest struct {
	Extensions []string `json:"extensions" binding:"required"`
}

// UpdateSiteRequest is the input for updating a PHP site.
type UpdateSiteRequest struct {
	Domain       string `json:"domain,omitempty"`
	WorkerMode   *bool  `json:"worker_mode,omitempty"`
	WorkerScript string `json:"worker_script,omitempty"`
}
