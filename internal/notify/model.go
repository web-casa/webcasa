package notify

import "time"

// Channel represents a notification channel (webhook or email).
type Channel struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Type      string    `gorm:"size:32;not null" json:"type"` // webhook | email
	Name      string    `gorm:"size:128;not null" json:"name"`
	Config    string    `gorm:"type:text" json:"config"`   // JSON config (url for webhook, smtp settings for email)
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	Events    string    `gorm:"type:text" json:"events"`   // JSON array of event patterns: ["deploy.*", "backup.*"]
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Channel) TableName() string {
	return "notify_channels"
}

// WebhookConfig holds config for webhook channels.
type WebhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"` // optional custom headers
}

// EmailConfig holds config for email channels.
type EmailConfig struct {
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"` // comma-separated recipients
	UseTLS   bool   `json:"use_tls"`
}

// DiscordConfig holds config for Discord webhook channels.
type DiscordConfig struct {
	WebhookURL string `json:"webhook_url"`
}

// TelegramConfig holds config for Telegram bot channels.
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// NotifyEvent is the payload sent to notification channels.
type NotifyEvent struct {
	Type    string                 `json:"type"`    // e.g. deploy.build.failed
	Title   string                 `json:"title"`   // human-readable title
	Message string                 `json:"message"` // detailed message
	Data    map[string]interface{} `json:"data"`    // raw event data
	Time    time.Time              `json:"time"`
}
