package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

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
	tools       *ToolRegistry
	memory      *MemoryService

	// pendingConfirms tracks tool calls that require user confirmation.
	// Key: pending_id, Value: pendingEntry with user ownership and approval channel.
	pendingMu       sync.Mutex
	pendingConfirms map[string]*pendingEntry
}

// pendingEntry binds a confirmation to its owning user.
type pendingEntry struct {
	userID uint
	ch     chan bool
}

// NewService creates a new AI assistant service.
func NewService(db *gorm.DB, configStore *pluginpkg.ConfigStore, coreAPI pluginpkg.CoreAPI, logger *slog.Logger, jwtSecret string) *Service {
	toolReg := NewToolRegistry(coreAPI, logger)
	RegisterBuiltinTools(toolReg)

	memorySvc := NewMemoryService(db, logger)

	svc := &Service{
		db:              db,
		configStore:     configStore,
		coreAPI:         coreAPI,
		logger:          logger,
		jwtSecret:       jwtSecret,
		tools:           toolReg,
		memory:          memorySvc,
		pendingConfirms: make(map[string]*pendingEntry),
	}
	// Set back-reference so tools that need AI generation can access the service.
	toolReg.svc = svc
	// Initialize embedding client from saved config.
	svc.initEmbeddingClient()
	return svc
}

// ── Config ──

// GetConfig returns the AI config (API key is masked).
func (s *Service) GetConfig() AIConfig {
	encKey := s.configStore.Get("api_key")
	apiKey, _ := Decrypt(encKey, s.jwtSecret)
	apiFormat := s.configStore.Get("api_format")
	if apiFormat == "" {
		apiFormat = "openai-chat"
	}
	encEmbKey := s.configStore.Get("embedding_api_key")
	embAPIKey, _ := Decrypt(encEmbKey, s.jwtSecret)
	return AIConfig{
		BaseURL:          s.configStore.Get("base_url"),
		APIKey:           MaskAPIKey(apiKey),
		Model:            s.configStore.Get("model"),
		APIFormat:        apiFormat,
		EmbeddingModel:   s.configStore.Get("embedding_model"),
		EmbeddingBaseURL: s.configStore.Get("embedding_base_url"),
		EmbeddingAPIKey:  MaskAPIKey(embAPIKey),
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
	if cfg.APIFormat != "" {
		s.configStore.Set("api_format", cfg.APIFormat)
	}
	// Save embedding model (empty string disables embedding).
	s.configStore.Set("embedding_model", cfg.EmbeddingModel)
	// Save separate embedding API credentials.
	if cfg.EmbeddingBaseURL != "" {
		s.configStore.Set("embedding_base_url", strings.TrimRight(cfg.EmbeddingBaseURL, "/"))
	} else {
		s.configStore.Set("embedding_base_url", "")
	}
	if cfg.EmbeddingAPIKey != "" && !strings.Contains(cfg.EmbeddingAPIKey, "****") {
		enc, err := Encrypt(cfg.EmbeddingAPIKey, s.jwtSecret)
		if err != nil {
			return fmt.Errorf("encrypt embedding api key: %w", err)
		}
		s.configStore.Set("embedding_api_key", enc)
	}
	// Re-initialize the embedding client with the new config.
	s.initEmbeddingClient()
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

// TestEmbeddingConnection tests the embedding API connectivity.
func (s *Service) TestEmbeddingConnection() error {
	embModel := s.configStore.Get("embedding_model")
	if embModel == "" {
		return fmt.Errorf("embedding model not configured")
	}

	baseURL := s.configStore.Get("embedding_base_url")
	encKey := s.configStore.Get("embedding_api_key")
	if baseURL == "" {
		baseURL = s.configStore.Get("base_url")
	}
	if encKey == "" {
		encKey = s.configStore.Get("api_key")
	}
	if baseURL == "" || encKey == "" {
		return fmt.Errorf("embedding API credentials not configured")
	}

	apiKey, err := Decrypt(encKey, s.jwtSecret)
	if err != nil {
		return fmt.Errorf("decrypt embedding api key: %w", err)
	}

	client := NewEmbeddingClient(baseURL, apiKey, embModel)
	_, err = client.Embed("test")
	return err
}

// ── Conversations ──

// ListConversations returns conversations for a specific user ordered by most recent.
func (s *Service) ListConversations(userID uint) ([]Conversation, error) {
	var convs []Conversation
	err := s.db.Where("user_id = ?", userID).Order("updated_at DESC").Find(&convs).Error
	return convs, err
}

// GetConversation returns a conversation with its messages, scoped to the user.
func (s *Service) GetConversation(id uint, userID uint) (*Conversation, error) {
	var conv Conversation
	if err := s.db.Preload("Messages", func(db *gorm.DB) *gorm.DB {
		return db.Order("created_at ASC")
	}).Where("user_id = ?", userID).First(&conv, id).Error; err != nil {
		return nil, err
	}
	return &conv, nil
}

// DeleteConversation removes a conversation and its messages, scoped to the user.
func (s *Service) DeleteConversation(id uint, userID uint) error {
	result := s.db.Where("id = ? AND user_id = ?", id, userID).Select("Messages").Delete(&Conversation{})
	if result.RowsAffected == 0 {
		return fmt.Errorf("conversation not found")
	}
	return result.Error
}

// ── Chat ──

// Chat handles a user message: creates/appends to conversation, streams AI response.
func (s *Service) Chat(ctx context.Context, req ChatRequest, userID uint, cb StreamCallback) (uint, error) {
	client, err := s.getClient()
	if err != nil {
		return 0, err
	}

	var conv Conversation
	if req.ConversationID > 0 {
		if err := s.db.Where("user_id = ?", userID).First(&conv, req.ConversationID).Error; err != nil {
			return 0, fmt.Errorf("conversation not found: %w", err)
		}
	} else {
		// Create new conversation with first ~30 runes of message as title.
		title := req.Message
		runes := []rune(title)
		if len(runes) > 30 {
			title = string(runes[:30]) + "..."
		}
		conv = Conversation{Title: title, UserID: userID}
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

// ResolveConfirmation resolves a pending tool confirmation.
// The userID must match the user who initiated the tool call.
func (s *Service) ResolveConfirmation(pendingID string, approved bool, userID uint) error {
	s.pendingMu.Lock()
	entry, ok := s.pendingConfirms[pendingID]
	s.pendingMu.Unlock()

	if !ok {
		return fmt.Errorf("no pending confirmation found for ID %q", pendingID)
	}

	if entry.userID != userID {
		return fmt.Errorf("permission denied: confirmation belongs to a different user")
	}

	entry.ch <- approved
	return nil
}

// ── Chat with Tools ──

// ChatWithTools handles a user message with tool use support.
// The callback receives StreamEvents: text deltas, tool calls, tool results, and done.
func (s *Service) ChatWithTools(ctx context.Context, req ChatRequest, userID uint, userRole string, cb StreamEventCallback) (uint, error) {
	client, err := s.getClient()
	if err != nil {
		return 0, err
	}

	var conv Conversation
	if req.ConversationID > 0 {
		if err := s.db.Where("user_id = ?", userID).First(&conv, req.ConversationID).Error; err != nil {
			return 0, fmt.Errorf("conversation not found: %w", err)
		}
	} else {
		title := req.Message
		runes := []rune(title)
		if len(runes) > 30 {
			title = string(runes[:30]) + "..."
		}
		conv = Conversation{Title: title, UserID: userID}
		if err := s.db.Create(&conv).Error; err != nil {
			return 0, fmt.Errorf("create conversation: %w", err)
		}
	}

	// Save user message.
	userMsg := Message{ConversationID: conv.ID, Role: "user", Content: req.Message}
	s.db.Create(&userMsg)

	// Build tool-use messages from conversation history.
	var history []Message
	s.db.Where("conversation_id = ?", conv.ID).Order("created_at ASC").Find(&history)
	apiMessages := s.buildToolMessages(history, req.Context)

	// Get tool schemas for the provider.
	var toolSchemas []map[string]interface{}
	if client.apiFormat == "anthropic-messages" {
		toolSchemas = s.tools.AnthropicToolSchema()
	} else {
		toolSchemas = s.tools.OpenAIToolSchema()
	}

	// Tool use loop: max 10 rounds for multi-step operations (e.g. auto_deploy).
	const maxRounds = 10
	var fullContent strings.Builder

	for round := 0; round < maxRounds; round++ {
		var pendingToolCalls []ToolCall
		var roundText strings.Builder

		err := client.ChatStreamWithTools(ctx, apiMessages, toolSchemas, func(event StreamEvent) error {
			switch event.Type {
			case "delta":
				roundText.WriteString(event.Content)
				return cb(event) // Forward text delta to frontend
			case "tool_call":
				pendingToolCalls = append(pendingToolCalls, *event.ToolCall)
				return cb(event) // Forward tool_call to frontend
			case "done":
				// Don't forward done yet — check if we need another round
			}
			return nil
		})
		if err != nil {
			return conv.ID, fmt.Errorf("stream round %d: %w", round, err)
		}

		// If no tool calls, we're done.
		if len(pendingToolCalls) == 0 {
			fullContent.WriteString(roundText.String())
			break
		}

		// Record assistant message with tool calls in API messages.
		assistantMsg := ToolUseMessage{
			Role:      "assistant",
			Content:   roundText.String(),
			ToolCalls: pendingToolCalls,
		}
		apiMessages = append(apiMessages, assistantMsg)
		fullContent.WriteString(roundText.String())

		// Execute each tool call and add results.
		for _, tc := range pendingToolCalls {
			s.logger.Info("executing tool", "name", tc.Name, "id", tc.ID)

			// Check admin-only permission before executing.
			tool := s.tools.Get(tc.Name)
			if tool != nil && tool.AdminOnly && userRole != "admin" {
				resultContent := `{"error": "Permission denied: this tool requires admin privileges"}`
				cb(StreamEvent{
					Type:    "tool_result",
					Content: resultContent,
					ToolCall: &ToolCall{
						ID:   tc.ID,
						Name: tc.Name,
					},
				})
				apiMessages = append(apiMessages, ToolUseMessage{
					Role:       "tool",
					Content:    resultContent,
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
				})
				s.logger.Warn("tool blocked: admin-only", "name", tc.Name, "user_id", userID, "role", userRole)
				continue
			}

			// Check if tool needs user confirmation.
			if tool != nil && tool.NeedsConfirmation {
				// Parse arguments for display.
				var argsMap map[string]interface{}
				json.Unmarshal([]byte(tc.Arguments), &argsMap)

				pendingID := fmt.Sprintf("%s-%s", tc.ID, tc.Name)

				// Create confirmation channel bound to this user.
				confirmCh := make(chan bool, 1)
				s.pendingMu.Lock()
				s.pendingConfirms[pendingID] = &pendingEntry{userID: userID, ch: confirmCh}
				s.pendingMu.Unlock()

				// Send confirm_required event to frontend.
				confirmData, _ := json.Marshal(PendingConfirmation{
					PendingID: pendingID,
					ToolName:  tc.Name,
					Arguments: argsMap,
				})
				cb(StreamEvent{
					Type:    "confirm_required",
					Content: string(confirmData),
				})

				// Wait for user decision (with context cancellation support).
				var approved bool
				select {
				case approved = <-confirmCh:
				case <-ctx.Done():
					approved = false
				}

				// Cleanup.
				s.pendingMu.Lock()
				delete(s.pendingConfirms, pendingID)
				s.pendingMu.Unlock()

				if !approved {
					resultContent := `{"status": "rejected", "message": "User rejected this action"}`
					cb(StreamEvent{
						Type:    "tool_result",
						Content: resultContent,
						ToolCall: &ToolCall{
							ID:   tc.ID,
							Name: tc.Name,
						},
					})
					apiMessages = append(apiMessages, ToolUseMessage{
						Role:       "tool",
						Content:    resultContent,
						ToolCallID: tc.ID,
						ToolName:   tc.Name,
					})
					continue
				}
			}

			result, execErr := s.tools.Execute(ctx, tc.Name, json.RawMessage(tc.Arguments))

			var resultContent string
			if execErr != nil {
				resultContent = fmt.Sprintf(`{"error": %q}`, execErr.Error())
			} else {
				resultBytes, _ := json.Marshal(result)
				resultContent = string(resultBytes)
			}

			// Send tool_result event to frontend.
			cb(StreamEvent{
				Type:    "tool_result",
				Content: resultContent,
				ToolCall: &ToolCall{
					ID:   tc.ID,
					Name: tc.Name,
				},
			})

			// Add tool result to API messages for next round.
			apiMessages = append(apiMessages, ToolUseMessage{
				Role:       "tool",
				Content:    resultContent,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})
		}
	}

	// Send final done event.
	cb(StreamEvent{Type: "done"})

	// Save assistant message (text portion).
	if content := fullContent.String(); content != "" {
		assistantMsg := Message{ConversationID: conv.ID, Role: "assistant", Content: content}
		s.db.Create(&assistantMsg)
	}

	s.db.Model(&conv).UpdateColumn("updated_at", gorm.Expr("CURRENT_TIMESTAMP"))

	// Async memory extraction after conversation turn.
	if s.configStore.Get("memory_enabled") != "false" && s.configStore.Get("auto_extract") != "false" {
		convID := conv.ID
		userMessage := req.Message
		assistantResponse := fullContent.String()
		go s.extractMemories(convID, userMessage, assistantResponse)
	}

	return conv.ID, nil
}

// buildToolMessages constructs the ToolUseMessage slice from conversation history.
func (s *Service) buildToolMessages(history []Message, pageContext string) []ToolUseMessage {
	systemPrompt := systemPromptToolUse

	// Inject relevant memories from previous interactions.
	if s.configStore.Get("memory_enabled") != "false" {
		var query string
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == "user" {
				query = history[i].Content
				break
			}
		}
		if query != "" {
			if memCtx, err := s.memory.BuildMemoryContext(query, 8); err == nil && memCtx != "" {
				systemPrompt += "\n\n" + memCtx
			}
		}
	}

	if pageContext != "" {
		systemPrompt += "\n\nCurrent page context:\n" + pageContext
	}

	msgs := []ToolUseMessage{{Role: "system", Content: systemPrompt}}

	// Include conversation history (limit to last 20 messages).
	start := 0
	if len(history) > 20 {
		start = len(history) - 20
	}
	for _, m := range history[start:] {
		msgs = append(msgs, ToolUseMessage{Role: m.Role, Content: m.Content})
	}

	return msgs
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

// ── Generate Dockerfile ──

// GenerateDockerfile converts a natural language description to an optimized Dockerfile.
func (s *Service) GenerateDockerfile(ctx context.Context, description string, cb StreamCallback) error {
	client, err := s.getClient()
	if err != nil {
		return err
	}

	messages := []chatMessage{
		{
			Role: "system",
			Content: `You are a Dockerfile expert. Convert the user's project description into an optimized, production-ready Dockerfile.
Rules:
- Output ONLY valid Dockerfile syntax, no explanations or markdown fences
- Use multi-stage builds when appropriate (build stage + runtime stage)
- Use slim/alpine base images to minimize image size (e.g. node:20-alpine, python:3.12-slim, golang:1.22-alpine)
- Leverage Docker layer caching: COPY package*.json before COPY . for Node.js, COPY go.mod go.sum before COPY . for Go
- Run as non-root user in production (add USER directive)
- Include HEALTHCHECK where appropriate
- Use ARG/ENV for configurable values
- Add useful comments explaining important directives
- Set appropriate EXPOSE ports
- Use .dockerignore best practices in comments`,
		},
		{Role: "user", Content: description},
	}

	return client.ChatStream(ctx, messages, cb)
}

// GenerateDockerfileSync runs Dockerfile generation synchronously and returns the full content.
func (s *Service) GenerateDockerfileSync(ctx context.Context, description string) (string, error) {
	var result strings.Builder
	err := s.GenerateDockerfile(ctx, description, func(delta string) error {
		result.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", err
	}
	return result.String(), nil
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

// DiagnoseSync runs AI diagnosis synchronously and returns the full response text.
// Used for automated build failure diagnosis (no streaming needed).
func (s *Service) DiagnoseSync(ctx context.Context, req DiagnoseRequest) (string, error) {
	var result strings.Builder
	err := s.Diagnose(ctx, req, func(delta string) error {
		result.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", err
	}
	return result.String(), nil
}

// ── Code Review ──

// ReviewCode streams a code review for a project.
func (s *Service) ReviewCode(ctx context.Context, projectID uint, cb StreamCallback) error {
	client, err := s.getClient()
	if err != nil {
		return err
	}

	// Get project info and source files
	proj, err := s.coreAPI.GetProject(projectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}

	projectName, _ := proj["name"].(string)
	framework, _ := proj["framework"].(string)

	// Read key files from project source directory.
	fileContents := s.readProjectFiles(projectID)
	if fileContents == "" {
		return fmt.Errorf("no source files found for project %d", projectID)
	}

	prompt := fmt.Sprintf("Review the following source code for project %q (framework: %s).\n\nAnalyze for:\n1. Security vulnerabilities\n2. Configuration errors\n3. Performance issues\n4. Best practice violations\n5. Missing error handling\n\nFiles:\n%s", projectName, framework, fileContents)

	messages := []chatMessage{
		{
			Role:    "system",
			Content: "You are a senior code reviewer specializing in security and deployment readiness. Provide a structured review with severity levels (Critical/Warning/Info). Use markdown. Be specific with line references.",
		},
		{Role: "user", Content: prompt},
	}

	return client.ChatStream(ctx, messages, cb)
}

// ReviewCodeSync runs code review synchronously and returns the full text.
func (s *Service) ReviewCodeSync(ctx context.Context, projectID uint) (string, error) {
	var result strings.Builder
	err := s.ReviewCode(ctx, projectID, func(delta string) error {
		result.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", err
	}
	return result.String(), nil
}

// readProjectFiles reads key source files from a project's source directory.
func (s *Service) readProjectFiles(projectID uint) string {
	// Determine the source directory via the data dir convention.
	dataDir, _ := s.coreAPI.GetSetting("data_dir")
	if dataDir == "" {
		dataDir = "data/plugins/deploy"
	}
	srcDir := fmt.Sprintf("%s/sources/project_%d", dataDir, projectID)

	keyFiles := []string{
		"Dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"package.json", "go.mod", "composer.json", "requirements.txt",
		"next.config.js", "next.config.mjs", "nuxt.config.ts",
		".env.example", ".env.sample",
		"main.go", "index.js", "server.js", "app.py", "manage.py",
	}

	var result strings.Builder
	for _, name := range keyFiles {
		path := srcDir + "/" + name
		data, err := readFileSafe(path, 200) // max 200 lines
		if err != nil {
			continue
		}
		result.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", name, data))
	}
	return result.String()
}

// readFileSafe reads up to maxLines lines from a file, or returns error.
func readFileSafe(path string, maxLines int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() && len(lines) < maxLines {
		lines = append(lines, scanner.Text())
	}
	return strings.Join(lines, "\n"), nil
}

// ── Rollback Suggestion ──

// SuggestRollbackSync analyzes a project and suggests whether to rollback.
func (s *Service) SuggestRollbackSync(ctx context.Context, projectID uint) (string, error) {
	client, err := s.getClient()
	if err != nil {
		return "", err
	}

	// Get project info
	proj, err := s.coreAPI.GetProject(projectID)
	if err != nil {
		return "", fmt.Errorf("project not found: %w", err)
	}

	projectName, _ := proj["name"].(string)
	framework, _ := proj["framework"].(string)
	status, _ := proj["status"].(string)

	// Get recent deployments
	db := s.coreAPI.GetDB()
	var deployments []struct {
		BuildNum  int    `json:"build_num"`
		Status    string `json:"status"`
		Duration  int    `json:"duration"`
		GitCommit string `json:"git_commit"`
		CreatedAt string `json:"created_at"`
	}
	db.Table("plugin_deploy_deployments").
		Where("project_id = ?", projectID).
		Order("build_num DESC").
		Limit(5).
		Find(&deployments)

	deploymentsJSON, _ := json.Marshal(deployments)

	// Get runtime logs
	runtimeLog, _ := s.coreAPI.GetRuntimeLog(projectID, 50)

	prompt := fmt.Sprintf(`Analyze this project and advise on rollback:

Project: %s (framework: %s, current status: %s)

Recent 5 deployments:
%s

Recent runtime logs:
%s

Provide:
1. Should the project be rolled back? (Yes/No)
2. If yes, which build number to roll back to and why
3. Brief risk assessment`, projectName, framework, status, string(deploymentsJSON), runtimeLog)

	messages := []chatMessage{
		{
			Role:    "system",
			Content: "You are a deployment expert. Analyze the project's deployment history and runtime status to make a rollback recommendation. Be concise and actionable. Use markdown.",
		},
		{Role: "user", Content: prompt},
	}

	var result strings.Builder
	err = client.ChatStream(ctx, messages, func(delta string) error {
		result.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", err
	}
	return result.String(), nil
}

// ── Alert Summary ──

// SummarizeAlertsSync generates an AI summary of recent monitoring alerts.
func (s *Service) SummarizeAlertsSync(ctx context.Context) (string, error) {
	client, err := s.getClient()
	if err != nil {
		return "", err
	}

	alerts, err := s.coreAPI.GetRecentAlerts()
	if err != nil {
		return "", fmt.Errorf("get alerts: %w", err)
	}
	if len(alerts) == 0 {
		return "No recent alerts found. The system appears to be healthy.", nil
	}

	alertsJSON, _ := json.Marshal(alerts)

	prompt := fmt.Sprintf(`Analyze these recent monitoring alerts and provide:
1. Summary of alert trends (what's happening)
2. Likely root causes
3. Recommended actions to resolve the issues
4. Priority assessment

Alerts (most recent first):
%s`, string(alertsJSON))

	messages := []chatMessage{
		{
			Role:    "system",
			Content: "You are a system monitoring expert. Analyze alert data to identify patterns, root causes, and provide actionable recommendations. Use markdown.",
		},
		{Role: "user", Content: prompt},
	}

	var result strings.Builder
	err = client.ChatStream(ctx, messages, func(delta string) error {
		result.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", err
	}
	return result.String(), nil
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

	apiFormat := s.configStore.Get("api_format")
	return NewLLMClient(baseURL, apiKey, model, apiFormat), nil
}

func (s *Service) buildMessages(history []Message, pageContext string) []chatMessage {
	systemPrompt := systemPromptBasic

	// Inject relevant memories from previous interactions.
	if s.configStore.Get("memory_enabled") != "false" {
		var query string
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == "user" {
				query = history[i].Content
				break
			}
		}
		if query != "" {
			if memCtx, err := s.memory.BuildMemoryContext(query, 8); err == nil && memCtx != "" {
				systemPrompt += "\n\n" + memCtx
			}
		}
	}

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

// initEmbeddingClient initializes the embedding client from saved config.
// Uses separate embedding_base_url / embedding_api_key when provided,
// otherwise falls back to the main chat base_url / api_key.
func (s *Service) initEmbeddingClient() {
	embModel := s.configStore.Get("embedding_model")
	if embModel == "" {
		// No embedding model configured — disable vector search, use keyword fallback.
		s.memory.SetEmbeddingClient(nil)
		return
	}

	// Prefer dedicated embedding credentials if set.
	baseURL := s.configStore.Get("embedding_base_url")
	encKey := s.configStore.Get("embedding_api_key")

	// Fall back to main chat credentials.
	if baseURL == "" {
		baseURL = s.configStore.Get("base_url")
	}
	if encKey == "" {
		encKey = s.configStore.Get("api_key")
	}

	if baseURL == "" || encKey == "" {
		return
	}

	apiKey, err := Decrypt(encKey, s.jwtSecret)
	if err != nil {
		return
	}

	s.memory.SetEmbeddingClient(NewEmbeddingClient(baseURL, apiKey, embModel))
}

// extractMemories uses the LLM to extract key facts from a conversation turn.
func (s *Service) extractMemories(convID uint, userMessage, assistantResponse string) {
	if userMessage == "" && assistantResponse == "" {
		return
	}

	client, err := s.getClient()
	if err != nil {
		return
	}

	prompt := fmt.Sprintf(`Extract key facts worth remembering from this conversation exchange for future reference.
Rules:
- Only extract concrete, reusable facts about THIS server
- Skip generic knowledge that any AI would know
- Each fact on a new line, format: [category|importance] fact
- Categories: server_config, troubleshooting, user_preference, deployment, general
- Importance: 0.0-1.0 (higher = more useful to remember)
- Output ONLY the extracted facts, nothing else. If none, output nothing.

User: %s
Assistant: %s`, userMessage, assistantResponse)

	messages := []chatMessage{
		{Role: "system", Content: "You extract and summarize key facts from conversations. Output only structured facts, no explanations."},
		{Role: "user", Content: prompt},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result strings.Builder
	if err := client.ChatStream(ctx, messages, func(delta string) error {
		result.WriteString(delta)
		return nil
	}); err != nil {
		s.logger.Warn("memory extraction failed", "err", err)
		return
	}

	// Parse extracted facts.
	lines := strings.Split(result.String(), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "[") {
			continue
		}

		// Parse [category|importance] fact
		closeBracket := strings.Index(line, "]")
		if closeBracket < 0 {
			continue
		}
		meta := line[1:closeBracket]
		fact := strings.TrimSpace(line[closeBracket+1:])
		if fact == "" {
			continue
		}

		category := "general"
		importance := float32(0.5)

		parts := strings.SplitN(meta, "|", 2)
		if len(parts) >= 1 {
			category = strings.TrimSpace(parts[0])
		}
		if len(parts) >= 2 {
			if v, err := fmt.Sscanf(parts[1], "%f", &importance); err != nil || v == 0 {
				importance = 0.5
			}
		}

		if _, err := s.memory.SaveMemory(fact, category, importance, &convID); err != nil {
			s.logger.Warn("failed to save extracted memory", "err", err, "fact", fact)
		}
	}
}
