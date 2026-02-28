package ai

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// Service implements the AI assistant business logic.
type Service struct {
	db          *gorm.DB
	configStore *pluginpkg.ConfigStore
	coreAPI     pluginpkg.CoreAPI
	logger      *slog.Logger
	jwtSecret   string // for API key encryption
}

// NewService creates a new AI assistant service.
func NewService(db *gorm.DB, configStore *pluginpkg.ConfigStore, coreAPI pluginpkg.CoreAPI, logger *slog.Logger, jwtSecret string) *Service {
	return &Service{
		db:          db,
		configStore: configStore,
		coreAPI:     coreAPI,
		logger:      logger,
		jwtSecret:   jwtSecret,
	}
}

// ── Config ──

// GetConfig returns the AI config (API key is masked).
func (s *Service) GetConfig() AIConfig {
	encKey := s.configStore.Get("api_key")
	apiKey, _ := Decrypt(encKey, s.jwtSecret)
	return AIConfig{
		BaseURL: s.configStore.Get("base_url"),
		APIKey:  MaskAPIKey(apiKey),
		Model:   s.configStore.Get("model"),
	}
}

// UpdateConfig saves the AI configuration. If api_key is "****" it is left unchanged.
func (s *Service) UpdateConfig(cfg AIConfig) error {
	if cfg.BaseURL != "" {
		s.configStore.Set("base_url", strings.TrimRight(cfg.BaseURL, "/"))
	}
	if cfg.Model != "" {
		s.configStore.Set("model", cfg.Model)
	}
	// Only update the key if it's not the masked placeholder.
	if cfg.APIKey != "" && !strings.Contains(cfg.APIKey, "****") {
		enc, err := Encrypt(cfg.APIKey, s.jwtSecret)
		if err != nil {
			return fmt.Errorf("encrypt api key: %w", err)
		}
		s.configStore.Set("api_key", enc)
	}
	return nil
}

// TestConnection tests the LLM connectivity.
func (s *Service) TestConnection(ctx context.Context) error {
	client, err := s.getClient()
	if err != nil {
		return err
	}
	return client.TestConnection(ctx)
}

// ── Conversations ──

// ListConversations returns all conversations ordered by most recent.
func (s *Service) ListConversations() ([]Conversation, error) {
	var convs []Conversation
	err := s.db.Order("updated_at DESC").Find(&convs).Error
	return convs, err
}

// GetConversation returns a conversation with its messages.
func (s *Service) GetConversation(id uint) (*Conversation, error) {
	var conv Conversation
	if err := s.db.Preload("Messages", func(db *gorm.DB) *gorm.DB {
		return db.Order("created_at ASC")
	}).First(&conv, id).Error; err != nil {
		return nil, err
	}
	return &conv, nil
}

// DeleteConversation removes a conversation and its messages.
func (s *Service) DeleteConversation(id uint) error {
	return s.db.Select("Messages").Delete(&Conversation{ID: id}).Error
}

// ── Chat ──

// Chat handles a user message: creates/appends to conversation, streams AI response.
func (s *Service) Chat(ctx context.Context, req ChatRequest, cb StreamCallback) (uint, error) {
	client, err := s.getClient()
	if err != nil {
		return 0, err
	}

	var conv Conversation
	if req.ConversationID > 0 {
		if err := s.db.First(&conv, req.ConversationID).Error; err != nil {
			return 0, fmt.Errorf("conversation not found: %w", err)
		}
	} else {
		// Create new conversation with first ~30 chars of message as title.
		title := req.Message
		if len(title) > 30 {
			title = title[:30] + "..."
		}
		conv = Conversation{Title: title}
		if err := s.db.Create(&conv).Error; err != nil {
			return 0, fmt.Errorf("create conversation: %w", err)
		}
	}

	// Save user message.
	userMsg := Message{ConversationID: conv.ID, Role: "user", Content: req.Message}
	s.db.Create(&userMsg)

	// Build messages for the API.
	var history []Message
	s.db.Where("conversation_id = ?", conv.ID).Order("created_at ASC").Find(&history)

	apiMessages := s.buildMessages(history, req.Context)

	// Stream the response, collecting full content.
	var fullContent strings.Builder
	if err := client.ChatStream(ctx, apiMessages, func(delta string) error {
		fullContent.WriteString(delta)
		return cb(delta)
	}); err != nil {
		return conv.ID, fmt.Errorf("stream: %w", err)
	}

	// Save assistant message.
	assistantMsg := Message{ConversationID: conv.ID, Role: "assistant", Content: fullContent.String()}
	s.db.Create(&assistantMsg)

	// Update conversation timestamp.
	s.db.Model(&conv).UpdateColumn("updated_at", gorm.Expr("CURRENT_TIMESTAMP"))

	return conv.ID, nil
}

// ── Generate Compose ──

// GenerateCompose converts a natural language description to a Docker Compose YAML.
func (s *Service) GenerateCompose(ctx context.Context, description string, cb StreamCallback) error {
	client, err := s.getClient()
	if err != nil {
		return err
	}

	messages := []chatMessage{
		{
			Role: "system",
			Content: `You are a Docker Compose expert. Convert the user's description into a valid docker-compose.yml file.
Rules:
- Output ONLY valid YAML, no explanations or markdown fences
- Use version '3.8' or higher
- Include health checks where appropriate
- Use reasonable defaults for ports, volumes, environment variables
- Add comments in YAML for important configuration options`,
		},
		{Role: "user", Content: description},
	}

	return client.ChatStream(ctx, messages, cb)
}

// ── Diagnose ──

// Diagnose analyzes error logs and returns diagnosis + fix suggestions.
func (s *Service) Diagnose(ctx context.Context, req DiagnoseRequest, cb StreamCallback) error {
	client, err := s.getClient()
	if err != nil {
		return err
	}

	prompt := "Analyze these error logs and provide:\n1. Root cause analysis\n2. Step-by-step fix instructions\n3. Prevention suggestions\n\n"
	if req.Context != "" {
		prompt += "Context: " + req.Context + "\n\n"
	}
	prompt += "Logs:\n```\n" + req.Logs + "\n```"

	messages := []chatMessage{
		{
			Role:    "system",
			Content: "You are a senior DevOps engineer and system administrator. Diagnose errors concisely and provide actionable fixes. Use markdown formatting.",
		},
		{Role: "user", Content: prompt},
	}

	return client.ChatStream(ctx, messages, cb)
}

// ── Internal helpers ──

func (s *Service) getClient() (*LLMClient, error) {
	baseURL := s.configStore.Get("base_url")
	encKey := s.configStore.Get("api_key")
	model := s.configStore.Get("model")

	if baseURL == "" || encKey == "" || model == "" {
		return nil, fmt.Errorf("AI not configured: please set base URL, API key, and model in AI settings")
	}

	apiKey, err := Decrypt(encKey, s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt api key: %w", err)
	}

	return NewLLMClient(baseURL, apiKey, model), nil
}

func (s *Service) buildMessages(history []Message, pageContext string) []chatMessage {
	systemPrompt := `You are Web.Casa AI Assistant, a helpful server management assistant.
You help users with:
- Docker container and Compose stack management
- Project deployment and configuration
- Caddy reverse proxy setup
- Error diagnosis and troubleshooting
- General server administration

Be concise, practical, and provide code snippets when helpful. Use markdown formatting.`

	if pageContext != "" {
		systemPrompt += "\n\nCurrent page context:\n" + pageContext
	}

	msgs := []chatMessage{{Role: "system", Content: systemPrompt}}

	// Include conversation history (limit to last 20 messages to stay within token limits).
	start := 0
	if len(history) > 20 {
		start = len(history) - 20
	}
	for _, m := range history[start:] {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}

	return msgs
}
