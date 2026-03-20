package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// Plugin implements the plugin.Plugin interface for AI Assistant.
type Plugin struct {
	svc        *Service
	handler    *Handler
	inspection *InspectionService
	selfHeal   *SelfHealEngine
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
	if err := ctx.DB.AutoMigrate(&Conversation{}, &Message{}, &Memory{}, &InspectionRecord{}); err != nil {
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

	// Create service, handler, and inspection service.
	p.svc = NewService(ctx.DB, ctx.ConfigStore, ctx.CoreAPI, ctx.Logger, jwtSecret)
	p.handler = NewHandler(p.svc)
	p.inspection = NewInspectionService(p.svc, ctx.CoreAPI, ctx.ConfigStore, ctx.EventBus, ctx.DB, ctx.Logger)

	// Wire inspection into the tool registry so run_inspection tool can access it.
	p.svc.tools.inspection = p.inspection

	// Create self-heal engine and subscribe to monitoring events.
	p.selfHeal = NewSelfHealEngine(p.svc, ctx.CoreAPI, ctx.EventBus, ctx.Logger)
	p.selfHeal.Subscribe()

	// Register API routes under /api/plugins/ai/
	r := ctx.Router       // read-only
	a := ctx.AdminRouter  // admin-only

	// Config (read + admin mutations)
	r.GET("/config", p.handler.GetConfig)
	r.GET("/presets", p.handler.GetPresets)
	a.PUT("/config", p.handler.UpdateConfig)
	a.POST("/config/test", p.handler.TestConnection)
	a.POST("/config/test-embedding", p.handler.TestEmbeddingConnection)

	// Chat (SSE) — any logged-in user can chat
	r.POST("/chat", p.handler.Chat)

	// Conversations (read + user delete)
	r.GET("/conversations", p.handler.ListConversations)
	r.GET("/conversations/:id", p.handler.GetConversation)
	r.DELETE("/conversations/:id", p.handler.DeleteConversation)

	// Tool confirmations — any logged-in user can confirm
	r.POST("/confirm", p.handler.Confirm)

	// Tools (SSE) — any logged-in user can use
	r.POST("/generate-compose", p.handler.GenerateCompose)
	r.POST("/generate-dockerfile", p.handler.GenerateDockerfile)
	r.POST("/diagnose", p.handler.Diagnose)

	// Review-code reads project source files — admin only.
	a.POST("/review-code", p.handler.ReviewCode)

	// Memory management
	r.GET("/memories", p.handler.ListMemories)
	a.DELETE("/memories/:id", p.handler.DeleteMemory)
	a.POST("/memories/clear", p.handler.ClearMemories)

	// Inspection routes
	a.POST("/inspection/run", p.handler.RunInspection)
	r.GET("/inspection/config", p.handler.GetInspectionConfig)
	a.PUT("/inspection/config", p.handler.UpdateInspectionConfig)
	r.GET("/inspection/history", p.handler.GetInspectionHistory)

	// Subscribe to build failure events for auto-diagnosis
	db := ctx.DB
	logger := ctx.Logger
	ctx.EventBus.Subscribe("deploy.build.failed", func(e pluginpkg.Event) {
		go p.handleBuildFailureDiagnosis(db, logger, e)
	})

	ctx.Logger.Info("AI assistant plugin routes registered")
	return nil
}

// handleBuildFailureDiagnosis processes a build failure event and runs AI diagnosis.
func (p *Plugin) handleBuildFailureDiagnosis(db *gorm.DB, logger *slog.Logger, e pluginpkg.Event) {
	deploymentID, _ := e.Payload["deployment_id"].(float64)
	projectName, _ := e.Payload["project_name"].(string)
	framework, _ := e.Payload["framework"].(string)
	errorMsg, _ := e.Payload["error_msg"].(string)
	logTail, _ := e.Payload["log_tail"].(string)

	if deploymentID == 0 || logTail == "" {
		return
	}

	diagCtx := fmt.Sprintf("Project: %s, Framework: %s, Error: %s", projectName, framework, errorMsg)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := p.svc.DiagnoseSync(ctx, DiagnoseRequest{
		Logs:    logTail,
		Context: diagCtx,
	})
	if err != nil {
		logger.Error("auto AI diagnosis failed", "deployment_id", uint(deploymentID), "err", err)
		return
	}

	// Write diagnosis result directly to the deployment record
	db.Table("plugin_deploy_deployments").Where("id = ?", uint(deploymentID)).Update("diagnosis_result", result)
	logger.Info("auto AI diagnosis completed", "deployment_id", uint(deploymentID), "project", projectName)
}

// Start is called after Init. Starts the inspection scheduler.
func (p *Plugin) Start() error {
	if p.inspection != nil {
		p.inspection.Start()
	}
	return nil
}

// Stop cleans up resources.
func (p *Plugin) Stop() error {
	if p.inspection != nil {
		p.inspection.Stop()
	}
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
