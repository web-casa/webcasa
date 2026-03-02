package monitoring

import "time"

// MetricRecord stores a single minute-level metrics snapshot.
type MetricRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Timestamp      time.Time `gorm:"index;not null" json:"timestamp"`
	CPUPercent     float64   `json:"cpu_percent"`
	LoadAvg1       float64   `json:"load_avg_1"`
	LoadAvg5       float64   `json:"load_avg_5"`
	LoadAvg15      float64   `json:"load_avg_15"`
	MemTotal       uint64    `json:"mem_total"`
	MemUsed        uint64    `json:"mem_used"`
	MemPercent     float64   `json:"mem_percent"`
	SwapTotal      uint64    `json:"swap_total"`
	SwapUsed       uint64    `json:"swap_used"`
	DiskTotal      uint64    `json:"disk_total"`
	DiskUsed       uint64    `json:"disk_used"`
	DiskPercent    float64   `json:"disk_percent"`
	DiskReadBytes  uint64    `json:"disk_read_bytes"`
	DiskWriteBytes uint64    `json:"disk_write_bytes"`
	NetRecvBytes   uint64    `json:"net_recv_bytes"`
	NetSentBytes   uint64    `json:"net_sent_bytes"`
}

func (MetricRecord) TableName() string { return "plugin_monitoring_metrics" }

// AlertRule defines a threshold-based alert.
type AlertRule struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	Metric      string    `gorm:"size:64;not null" json:"metric"`
	Operator    string    `gorm:"size:4;default:>" json:"operator"`
	Threshold   float64   `gorm:"not null" json:"threshold"`
	Duration    int       `gorm:"default:1" json:"duration"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	NotifyType  string    `gorm:"size:16;default:webhook" json:"notify_type"`
	NotifyURL   string    `gorm:"size:512" json:"notify_url"`
	NotifyEmail string    `gorm:"size:256" json:"notify_email"`
	CooldownMin int       `gorm:"default:30" json:"cooldown_min"`
	LastFiredAt time.Time `json:"last_fired_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (AlertRule) TableName() string { return "plugin_monitoring_alert_rules" }

// AlertHistory records when an alert was triggered.
type AlertHistory struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	RuleID    uint      `gorm:"index;not null" json:"rule_id"`
	RuleName  string    `gorm:"size:128" json:"rule_name"`
	Metric    string    `gorm:"size:64" json:"metric"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Message   string    `gorm:"type:text" json:"message"`
	Notified  bool      `gorm:"default:false" json:"notified"`
	CreatedAt time.Time `json:"created_at"`
}

func (AlertHistory) TableName() string { return "plugin_monitoring_alert_history" }

// ── Request structs ──

// CreateAlertRequest is the input for creating an alert rule.
type CreateAlertRequest struct {
	Name        string  `json:"name" binding:"required"`
	Metric      string  `json:"metric" binding:"required"`
	Operator    string  `json:"operator"`
	Threshold   float64 `json:"threshold" binding:"required"`
	Duration    int     `json:"duration"`
	NotifyType  string  `json:"notify_type"`
	NotifyURL   string  `json:"notify_url"`
	NotifyEmail string  `json:"notify_email"`
	CooldownMin int     `json:"cooldown_min"`
}

// UpdateAlertRequest is the input for updating an alert rule.
type UpdateAlertRequest struct {
	Name        string  `json:"name"`
	Metric      string  `json:"metric"`
	Operator    string  `json:"operator"`
	Threshold   float64 `json:"threshold"`
	Duration    int     `json:"duration"`
	Enabled     *bool   `json:"enabled"`
	NotifyType  string  `json:"notify_type"`
	NotifyURL   string  `json:"notify_url"`
	NotifyEmail string  `json:"notify_email"`
	CooldownMin int     `json:"cooldown_min"`
}

// ContainerMetric is per-container stats.
type ContainerMetric struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Image      string  `json:"image"`
	Status     string  `json:"status"`
	CPUPercent float64 `json:"cpu_percent"`
	MemUsage   uint64  `json:"mem_usage"`
	MemLimit   uint64  `json:"mem_limit"`
	MemPercent float64 `json:"mem_percent"`
}

// MetricSnapshot is the in-memory representation of a system metrics sample.
type MetricSnapshot struct {
	CPUPercent     float64 `json:"cpu_percent"`
	LoadAvg1       float64 `json:"load_avg_1"`
	LoadAvg5       float64 `json:"load_avg_5"`
	LoadAvg15      float64 `json:"load_avg_15"`
	MemTotal       uint64  `json:"mem_total"`
	MemUsed        uint64  `json:"mem_used"`
	MemPercent     float64 `json:"mem_percent"`
	SwapTotal      uint64  `json:"swap_total"`
	SwapUsed       uint64  `json:"swap_used"`
	DiskTotal      uint64  `json:"disk_total"`
	DiskUsed       uint64  `json:"disk_used"`
	DiskPercent    float64 `json:"disk_percent"`
	DiskReadBytes  uint64  `json:"disk_read_bytes"`
	DiskWriteBytes uint64  `json:"disk_write_bytes"`
	NetRecvBytes   uint64  `json:"net_recv_bytes"`
	NetSentBytes   uint64  `json:"net_sent_bytes"`
}
