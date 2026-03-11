package cronjob

import (
	"encoding/json"
	"time"
)

// CronTask represents a scheduled task.
type CronTask struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	Name            string     `gorm:"size:128;not null" json:"name"`
	Expression      string     `gorm:"size:128;not null" json:"expression"` // standard 5-field cron
	Command         string     `gorm:"type:text;not null" json:"command"`
	WorkingDir      string     `gorm:"size:512" json:"working_dir"`
	Enabled         bool       `gorm:"default:true" json:"enabled"`
	Tags            string     `gorm:"type:text" json:"tags"` // JSON array: ["backup","cleanup"]
	TimeoutSec      int        `gorm:"default:300" json:"timeout_sec"`
	MaxRetries      int        `gorm:"default:0" json:"max_retries"`
	NotifyOnFailure bool       `gorm:"default:false" json:"notify_on_failure"`
	LastRunAt       *time.Time `json:"last_run_at"`
	LastStatus      string     `gorm:"size:32" json:"last_status"` // success/failed/timeout/skipped
	NextRunAt       *time.Time `json:"next_run_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (CronTask) TableName() string { return "plugin_cronjob_tasks" }

// GetTags unmarshals the JSON tags array.
func (t *CronTask) GetTags() []string {
	if t.Tags == "" {
		return nil
	}
	var tags []string
	_ = json.Unmarshal([]byte(t.Tags), &tags)
	return tags
}

// SetTags marshals a string slice into the JSON tags field.
func (t *CronTask) SetTags(tags []string) {
	if len(tags) == 0 {
		t.Tags = "[]"
		return
	}
	b, _ := json.Marshal(tags)
	t.Tags = string(b)
}

// CronLog records one execution of a cron task.
type CronLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	TaskID     uint      `gorm:"index;not null" json:"task_id"`
	TaskName   string    `gorm:"size:128" json:"task_name"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	ExitCode   int       `json:"exit_code"`
	Output     string    `gorm:"type:text" json:"output"` // truncated to 64KB
	Status     string    `gorm:"size:32" json:"status"`   // success/failed/timeout/skipped
	DurationMs int64     `json:"duration_ms"`
}

func (CronLog) TableName() string { return "plugin_cronjob_logs" }

// CreateTaskRequest is the API request for creating a task.
type CreateTaskRequest struct {
	Name            string   `json:"name" binding:"required"`
	Expression      string   `json:"expression" binding:"required"`
	Command         string   `json:"command" binding:"required"`
	WorkingDir      string   `json:"working_dir"`
	Enabled         *bool    `json:"enabled"`
	Tags            []string `json:"tags"`
	TimeoutSec      int      `json:"timeout_sec"`
	MaxRetries      int      `json:"max_retries"`
	NotifyOnFailure bool     `json:"notify_on_failure"`
}

// UpdateTaskRequest is the API request for updating a task.
type UpdateTaskRequest struct {
	Name            *string  `json:"name"`
	Expression      *string  `json:"expression"`
	Command         *string  `json:"command"`
	WorkingDir      *string  `json:"working_dir"`
	Enabled         *bool    `json:"enabled"`
	Tags            []string `json:"tags"`
	TimeoutSec      *int     `json:"timeout_sec"`
	MaxRetries      *int     `json:"max_retries"`
	NotifyOnFailure *bool    `json:"notify_on_failure"`
}
