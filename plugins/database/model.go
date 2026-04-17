package database

import "time"

// EngineType defines supported database engines.
type EngineType string

const (
	EngineMySQL    EngineType = "mysql"
	EnginePostgres EngineType = "postgres"
	EngineMariaDB  EngineType = "mariadb"
	EngineRedis    EngineType = "redis"
)

// EngineConfig holds tuneable configuration parameters for a database instance.
// Fields are engine-specific: only the relevant ones are used per engine type.
type EngineConfig struct {
	// MySQL / MariaDB
	InnoDBBufferPoolSize string  `json:"innodb_buffer_pool_size,omitempty"` // e.g. "128M"
	MaxConnections       int     `json:"max_connections,omitempty"`
	CharacterSetServer   string  `json:"character_set_server,omitempty"`    // e.g. "utf8mb4"
	CollationServer      string  `json:"collation_server,omitempty"`        // e.g. "utf8mb4_unicode_ci"
	SlowQueryLog         *bool   `json:"slow_query_log,omitempty"`
	LongQueryTime        float64 `json:"long_query_time,omitempty"` // seconds

	// PostgreSQL
	SharedBuffers           string `json:"shared_buffers,omitempty"`             // e.g. "128MB"
	WorkMem                 string `json:"work_mem,omitempty"`                   // e.g. "4MB"
	EffectiveCacheSize      string `json:"effective_cache_size,omitempty"`        // e.g. "384MB"
	WalLevel                string `json:"wal_level,omitempty"`                  // replica / logical / minimal
	LogMinDurationStatement *int   `json:"log_min_duration_statement,omitempty"` // ms, -1=off
	// Durability knobs. nil = leave Postgres default; set only by the "crit"
	// tuning preset today, but exposed for future advanced-config UX.
	SynchronousCommit string `json:"synchronous_commit,omitempty"` // on | off | local | remote_apply | remote_write
	FullPageWrites    *bool  `json:"full_page_writes,omitempty"`
	Fsync             *bool  `json:"fsync,omitempty"`

	// Redis
	MaxMemory       string `json:"maxmemory,omitempty"`        // e.g. "256mb"
	MaxMemoryPolicy string `json:"maxmemory_policy,omitempty"` // e.g. "noeviction"
	AppendOnly      *bool  `json:"appendonly,omitempty"`
	Save            string `json:"save,omitempty"` // e.g. "3600 1 300 100" or "" to disable
}

// EnginePresets provides development and production preset configurations.
var EnginePresets = map[EngineType]map[string]EngineConfig{
	EngineMySQL: {
		"development": {
			InnoDBBufferPoolSize: "128M",
			MaxConnections:       50,
			CharacterSetServer:   "utf8mb4",
			CollationServer:      "utf8mb4_unicode_ci",
			SlowQueryLog:         boolPtr(true),
			LongQueryTime:        2,
		},
		"production": {
			InnoDBBufferPoolSize: "1G",
			MaxConnections:       200,
			CharacterSetServer:   "utf8mb4",
			CollationServer:      "utf8mb4_unicode_ci",
			SlowQueryLog:         boolPtr(true),
			LongQueryTime:        1,
		},
	},
	EngineMariaDB: {
		"development": {
			InnoDBBufferPoolSize: "128M",
			MaxConnections:       50,
			CharacterSetServer:   "utf8mb4",
			CollationServer:      "utf8mb4_unicode_ci",
			SlowQueryLog:         boolPtr(true),
			LongQueryTime:        2,
		},
		"production": {
			InnoDBBufferPoolSize: "1G",
			MaxConnections:       200,
			CharacterSetServer:   "utf8mb4",
			CollationServer:      "utf8mb4_unicode_ci",
			SlowQueryLog:         boolPtr(true),
			LongQueryTime:        1,
		},
	},
	EnginePostgres: {
		"development": {
			SharedBuffers:           "64MB",
			MaxConnections:          50,
			WorkMem:                 "2MB",
			EffectiveCacheSize:      "192MB",
			WalLevel:                "replica",
			LogMinDurationStatement: intPtr(2000),
		},
		"production": {
			SharedBuffers:           "512MB",
			MaxConnections:          200,
			WorkMem:                 "8MB",
			EffectiveCacheSize:      "1536MB",
			WalLevel:                "replica",
			LogMinDurationStatement: intPtr(1000),
		},
	},
	EngineRedis: {
		"development": {
			MaxMemory:       "128mb",
			MaxMemoryPolicy: "noeviction",
			AppendOnly:      boolPtr(false),
			Save:            "3600 1 300 100",
		},
		"production": {
			MaxMemory:       "1gb",
			MaxMemoryPolicy: "noeviction",
			AppendOnly:      boolPtr(true),
			Save:            "3600 1 300 100 60 10000",
		},
	},
}

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

// EngineInfo describes a supported engine with available versions.
type EngineInfo struct {
	Engine   EngineType `json:"engine"`
	Name     string     `json:"name"`
	Versions []string   `json:"versions"`
	Default  string     `json:"default"`
	Port     int        `json:"default_port"`
}

// SupportedEngines lists all available database engines.
var SupportedEngines = []EngineInfo{
	{Engine: EngineMariaDB, Name: "MariaDB", Versions: []string{"11.8", "11.4", "10.11"}, Default: "11.8", Port: 3306},
	{Engine: EngineMySQL, Name: "MySQL", Versions: []string{"8.4", "8.0"}, Default: "8.4", Port: 3306},
	{Engine: EnginePostgres, Name: "PostgreSQL", Versions: []string{"18", "17", "16", "15", "14"}, Default: "17", Port: 5432},
	{Engine: EngineRedis, Name: "Redis", Versions: []string{"8.6", "8.4", "7.4", "6.2"}, Default: "8.6", Port: 6379},
}

// Instance represents a Docker-managed database server instance.
type Instance struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Name          string     `gorm:"uniqueIndex;not null;size:128" json:"name"`
	Engine        EngineType `gorm:"size:32;not null" json:"engine"`
	Version       string     `gorm:"size:32;not null" json:"version"`
	Status        string     `gorm:"size:16;default:stopped" json:"status"`
	Port          int        `gorm:"not null" json:"port"`
	RootPassword  string     `gorm:"size:256" json:"-"`
	DataDir       string     `gorm:"size:512" json:"data_dir"`
	ContainerName string     `gorm:"size:128" json:"container_name"`
	MemoryLimit   string     `gorm:"size:32;default:0.5g" json:"memory_limit"`
	// TuningPreset records which workload-aware preset was applied at creation
	// (postgres only for v0.11). Empty/unset means the user supplied raw Config
	// directly. Stored for audit and possible re-application after a memory
	// resize. See plugins/database/presets_postgres.go for available presets.
	TuningPreset  string     `gorm:"size:16;default:''" json:"tuning_preset"`
	Config        string     `gorm:"type:text" json:"config"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// TableName overrides GORM table name with plugin prefix.
func (Instance) TableName() string { return "plugin_database_instances" }

// Database represents a logical database within an Instance.
type Database struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	InstanceID uint      `gorm:"index;not null" json:"instance_id"`
	Name       string    `gorm:"size:128;not null" json:"name"`
	Charset    string    `gorm:"size:32;default:utf8mb4" json:"charset"`
	Collation  string    `gorm:"size:64" json:"collation"`
	CreatedAt  time.Time `json:"created_at"`
}

// TableName overrides GORM table name with plugin prefix.
func (Database) TableName() string { return "plugin_database_databases" }

// DatabaseUser represents a user within an Instance.
type DatabaseUser struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	InstanceID uint      `gorm:"index;not null" json:"instance_id"`
	Username   string    `gorm:"size:128;not null" json:"username"`
	Host       string    `gorm:"size:128;default:%" json:"host"`
	CreatedAt  time.Time `json:"created_at"`
}

// TableName overrides GORM table name with plugin prefix.
func (DatabaseUser) TableName() string { return "plugin_database_users" }

// ── Request / Response structs ──

// CreateInstanceRequest is the input for creating a new database instance.
type CreateInstanceRequest struct {
	Name         string     `json:"name" binding:"required"`
	Engine       EngineType `json:"engine" binding:"required"`
	Version      string     `json:"version"`
	Port         int        `json:"port"`
	RootPassword string     `json:"root_password"`
	MemoryLimit  string        `json:"memory_limit"`
	AutoStart    bool          `json:"auto_start"`
	// TuningPreset, when non-empty, runs the named workload-aware preset (see
	// presets_postgres.go) against MemoryLimit and overwrites Config with the
	// derived EngineConfig. Currently postgres only; ignored for other engines.
	TuningPreset string        `json:"tuning_preset,omitempty"`
	Config       *EngineConfig `json:"config,omitempty"`
}

// CreateDatabaseRequest is the input for creating a logical database.
type CreateDatabaseRequest struct {
	Name    string `json:"name" binding:"required"`
	Charset string `json:"charset"`
}

// CreateUserRequest is the input for creating a database user.
type CreateUserRequest struct {
	Username  string   `json:"username" binding:"required"`
	Password  string   `json:"password" binding:"required"`
	Databases []string `json:"databases"`
}

// ConnectionInfo is the response for connection info display.
type ConnectionInfo struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	ConnectionURI  string `json:"connection_uri"`
	CLICommand     string `json:"cli_command"`
	EnvVar         string `json:"env_var"`
	DockerInternal string `json:"docker_internal"`
}

// SQLiteQueryRequest is the input for executing a SQLite query.
type SQLiteQueryRequest struct {
	Path  string `json:"path" binding:"required"`
	Query string `json:"query" binding:"required"`
	Limit int    `json:"limit"`
}

// ExecuteQueryRequest is the input for executing a query against a running instance.
type ExecuteQueryRequest struct {
	Query    string `json:"query" binding:"required"`
	Database string `json:"database"`
	Limit    int    `json:"limit"`
}
