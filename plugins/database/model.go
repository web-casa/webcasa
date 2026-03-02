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
	{Engine: EngineMySQL, Name: "MySQL", Versions: []string{"8.4", "8.0"}, Default: "8.4", Port: 3306},
	{Engine: EnginePostgres, Name: "PostgreSQL", Versions: []string{"17", "16", "15"}, Default: "16", Port: 5432},
	{Engine: EngineMariaDB, Name: "MariaDB", Versions: []string{"11", "10.11"}, Default: "11", Port: 3306},
	{Engine: EngineRedis, Name: "Redis", Versions: []string{"7"}, Default: "7", Port: 6379},
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
	MemoryLimit   string     `gorm:"size:32;default:512m" json:"memory_limit"`
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
	RootPassword string     `json:"root_password" binding:"required"`
	MemoryLimit  string     `json:"memory_limit"`
	AutoStart    bool       `json:"auto_start"`
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
