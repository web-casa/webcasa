package backup

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// BackupConfig is the singleton configuration for the backup plugin (ID=1).
type BackupConfig struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	TargetType      string    `gorm:"size:16;default:local" json:"target_type"` // local, s3, webdav, sftp
	LocalPath       string    `gorm:"size:512" json:"local_path"`
	S3Endpoint      string    `gorm:"size:256" json:"s3_endpoint"`
	S3Bucket        string    `gorm:"size:128" json:"s3_bucket"`
	S3AccessKey     string    `gorm:"size:256" json:"-"`
	S3SecretKey     string    `gorm:"size:256" json:"-"`
	S3Region        string    `gorm:"size:64" json:"s3_region"`
	WebdavURL       string    `gorm:"size:512" json:"webdav_url"`
	WebdavUser      string    `gorm:"size:128" json:"webdav_user"`
	WebdavPassword  string    `gorm:"size:256" json:"-"`
	SftpHost        string    `gorm:"size:256" json:"sftp_host"`
	SftpPort        int       `gorm:"default:22" json:"sftp_port"`
	SftpUser        string    `gorm:"size:128" json:"sftp_user"`
	SftpPassword    string    `gorm:"size:256" json:"-"`
	SftpKeyPath     string    `gorm:"size:512" json:"sftp_key_path"`
	SftpPath        string    `gorm:"size:512" json:"sftp_path"`
	ScheduleEnabled bool      `gorm:"default:false" json:"schedule_enabled"`
	CronExpr        string    `gorm:"size:64;default:0 2 * * *" json:"cron_expr"`
	RetainCount     int       `gorm:"default:10" json:"retain_count"`
	RetainDays      int       `gorm:"default:30" json:"retain_days"`
	Scopes          JSONArray `gorm:"type:text" json:"scopes"` // ["panel", "docker", "database"]
	RepoPassword    string    `gorm:"size:256" json:"-"`
	RepoInitialized bool      `gorm:"default:false" json:"repo_initialized"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (BackupConfig) TableName() string { return "plugin_backup_config" }

// BackupSnapshot represents a single backup snapshot.
type BackupSnapshot struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	SnapshotID string    `gorm:"size:128;index" json:"snapshot_id"` // Kopia snapshot ID
	Status     string    `gorm:"size:16;default:pending" json:"status"`
	Scopes     JSONArray `gorm:"type:text" json:"scopes"`
	SizeBytes  int64     `json:"size_bytes"`
	Duration   float64   `json:"duration"` // seconds
	ErrorMsg   string    `gorm:"type:text" json:"error_msg"`
	Trigger    string    `gorm:"size:16;default:manual" json:"trigger"` // manual, scheduled
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (BackupSnapshot) TableName() string { return "plugin_backup_snapshots" }

// BackupLog stores log entries for a backup operation.
type BackupLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	SnapshotID uint      `gorm:"index;not null" json:"snapshot_id"`
	Level      string    `gorm:"size:8" json:"level"` // info, warn, error
	Message    string    `gorm:"type:text" json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

func (BackupLog) TableName() string { return "plugin_backup_logs" }

// ── Request Structs ──

// UpdateConfigRequest is the input for updating backup configuration.
type UpdateConfigRequest struct {
	TargetType      string   `json:"target_type"`
	LocalPath       string   `json:"local_path"`
	S3Endpoint      string   `json:"s3_endpoint"`
	S3Bucket        string   `json:"s3_bucket"`
	S3AccessKey     string   `json:"s3_access_key"`
	S3SecretKey     string   `json:"s3_secret_key"`
	S3Region        string   `json:"s3_region"`
	WebdavURL       string   `json:"webdav_url"`
	WebdavUser      string   `json:"webdav_user"`
	WebdavPassword  string   `json:"webdav_password"`
	SftpHost        string   `json:"sftp_host"`
	SftpPort        int      `json:"sftp_port"`
	SftpUser        string   `json:"sftp_user"`
	SftpPassword    string   `json:"sftp_password"`
	SftpKeyPath     string   `json:"sftp_key_path"`
	SftpPath        string   `json:"sftp_path"`
	ScheduleEnabled *bool    `json:"schedule_enabled"`
	CronExpr        string   `json:"cron_expr"`
	RetainCount     int      `json:"retain_count"`
	RetainDays      int      `json:"retain_days"`
	Scopes          []string `json:"scopes"`
	RepoPassword    string   `json:"repo_password"`
}

// BackupStatus is the response for the current status endpoint.
type BackupStatus struct {
	Running     bool            `json:"running"`
	LastBackup  *BackupSnapshot `json:"last_backup"`
	NextRunTime *time.Time      `json:"next_run_time"`
}

// ── JSON Array Type ──

// JSONArray is a custom type for storing string arrays as JSON in the database.
type JSONArray []string

func (j JSONArray) Value() (driver.Value, error) {
	if j == nil {
		return "[]", nil
	}
	data, err := json.Marshal(j)
	return string(data), err
}

func (j *JSONArray) Scan(value interface{}) error {
	if value == nil {
		*j = []string{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return fmt.Errorf("unsupported type for JSONArray: %T", value)
	}
	return json.Unmarshal(bytes, j)
}
