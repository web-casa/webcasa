package ai

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for AI Assistant.
type Plugin struct {
	svc     *Service
	handler *Handler
}

// New creates a new AI assistant plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "ai",
		Name:        "AI Assistant",
		Version:     "1.0.0",
		Description: "AI-powered chat assistant with error diagnosis and template generation",
		Author:      "Web.Casa",
		Priority:    30,
		Icon:        "Bot",
		Category:    "tool",
	}
}

// Init initialises the AI plugin: migrates DB, registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Migrate models.
	if err := ctx.DB.AutoMigrate(&Conversation{}, &Message{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Get JWT secret for API key encryption.
	jwtSecret, _ := ctx.CoreAPI.GetSetting("jwt_secret")
	if jwtSecret == "" {
		// No jwt_secret configured. Use a persistent per-installation random key
		// stored in the plugin's own config to avoid a predictable hardcoded fallback.
		jwtSecret = ctx.ConfigStore.Get("_encryption_key")
		if jwtSecret == "" {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return fmt.Errorf("generate encryption key: %w", err)
			}
			jwtSecret = hex.EncodeToString(b)
			if err := ctx.ConfigStore.Set("_encryption_key", jwtSecret); err != nil {
				return fmt.Errorf("persist encryption key: %w", err)
			}
			ctx.Logger.Warn("jwt_secret not set, generated a random encryption key for AI plugin")
		}
	}

	// Create service and handler.
	p.svc = NewService(ctx.DB, ctx.ConfigStore, ctx.CoreAPI, ctx.Logger, jwtSecret)
	p.handler = NewHandler(p.svc)

	// Register API routes under /api/plugins/ai/
	r := ctx.Router

	// Config
	r.GET("/config", p.handler.GetConfig)
	r.PUT("/config", p.handler.UpdateConfig)
	r.POST("/config/test", p.handler.TestConnection)

	// Chat (SSE)
	r.POST("/chat", p.handler.Chat)

	// Conversations
	r.GET("/conversations", p.handler.ListConversations)
	r.GET("/conversations/:id", p.handler.GetConversation)
	r.DELETE("/conversations/:id", p.handler.DeleteConversation)

	// Tools (SSE)
	r.POST("/generate-compose", p.handler.GenerateCompose)
	r.POST("/diagnose", p.handler.Diagnose)

	ctx.Logger.Info("AI assistant plugin routes registered")
	return nil
}

// Start is called after Init. No background tasks.
func (p *Plugin) Start() error {
	return nil
}

// Stop cleans up resources.
func (p *Plugin) Stop() error {
	return nil
}

// FrontendManifest declares the frontend routes.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "ai",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/ai/config", Component: "AIConfig", Menu: true, Icon: "Bot", Label: "AI Assistant", LabelZh: "AI 助手"},
		},
		MenuGroup: "tool",
		MenuOrder: 50,
	}
}
