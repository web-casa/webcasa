package docker

import "time"

// Stack represents a Docker Compose project (a group of related containers).
type Stack struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null;size:128" json:"name"`
	Description string    `gorm:"size:512" json:"description"`
	ComposeFile string    `gorm:"type:text;not null" json:"compose_file"` // YAML content
	EnvFile     string    `gorm:"type:text" json:"env_file"`             // .env content
	Status      string    `gorm:"size:16;default:stopped" json:"status"` // running, stopped, partial, error
	DataDir     string    `gorm:"size:512" json:"data_dir"`              // directory where compose file is stored
	AutoUpdate  *bool     `gorm:"default:false" json:"auto_update"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName overrides GORM table name with plugin prefix.
func (Stack) TableName() string { return "plugin_docker_stacks" }
