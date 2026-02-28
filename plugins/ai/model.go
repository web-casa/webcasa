package ai

import (
	"time"

	"gorm.io/gorm"
)

// Conversation represents a chat conversation.
type Conversation struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	Title     string         `json:"title"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
	Messages  []Message      `json:"messages,omitempty" gorm:"foreignKey:ConversationID;constraint:OnDelete:CASCADE"`
}

func (Conversation) TableName() string { return "plugin_ai_conversations" }

// Message is a single message in a conversation.
type Message struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ConversationID uint      `json:"conversation_id" gorm:"index"`
	Role           string    `json:"role"` // "user", "assistant", "system"
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

func (Message) TableName() string { return "plugin_ai_messages" }

// ChatRequest is the request body for the chat endpoint.
type ChatRequest struct {
	ConversationID uint   `json:"conversation_id"` // 0 = new conversation
	Message        string `json:"message"`
	Context        string `json:"context"` // optional page context
}

// GenerateComposeRequest for text-to-template.
type GenerateComposeRequest struct {
	Description string `json:"description"`
}

// DiagnoseRequest for error diagnosis.
type DiagnoseRequest struct {
	Logs    string `json:"logs"`
	Context string `json:"context"` // optional: what the user was trying to do
}

// AIConfig holds the AI provider configuration.
type AIConfig struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"` // masked in response
	Model   string `json:"model"`
}
