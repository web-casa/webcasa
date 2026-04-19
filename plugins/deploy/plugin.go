package deploy

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for project deployment.
type Plugin struct {
	svc     *Service
	handler *Handler
}

// New creates a new deploy plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "deploy",
		Name:        "Project Deploy",
		Version:     "1.0.0",
		Description: "Source code deployment for Node.js, Go, PHP, Python projects with auto-detection and process management",
		Author:      "Web.Casa",
		Priority:    5,
		Icon:        "Rocket",
		Category:    "deploy",
	}
}

// Init initialises the deploy plugin: migrates DB, creates service, registers routes.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Migrate models
	if err := ctx.DB.AutoMigrate(&Project{}, &Deployment{}, &PreviewDeployment{}, &CronJob{}, &ExtraProcess{}, &GitHubInstallation{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Get JWT secret for deploy key encryption (same pattern as AI plugin).
	jwtSecret, _ := ctx.CoreAPI.GetSetting("jwt_secret")
	if jwtSecret == "" {
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
			ctx.Logger.Warn("jwt_secret not set, generated a random encryption key for deploy plugin")
		}
	}

	// Create service and handler
	p.svc = NewService(ctx.DB, ctx.CoreAPI, ctx.EventBus, ctx.Logger, ctx.DataDir, jwtSecret, ctx.ConfigStore)
	p.handler = NewHandler(p.svc)

	// Register API routes under /api/plugins/deploy/
	r := ctx.Router          // read-only (any authenticated user)
	o := ctx.OperatorRouter  // operator+ (operations: build/start/stop)
	a := ctx.AdminRouter     // admin-only (config changes)

	// Frameworks presets (read)
	r.GET("/frameworks", p.handler.GetFrameworks)
	r.GET("/suggest-env", p.handler.SuggestEnv)
	a.GET("/detect", p.handler.DetectFramework) // admin only — triggers git clone

	// Projects CRUD (read + admin mutations)
	r.GET("/projects", p.handler.ListProjects)
	a.POST("/projects", p.handler.CreateProject)
	r.GET("/projects/:id", p.handler.GetProject)
	a.PUT("/projects/:id", p.handler.UpdateProject)
	a.DELETE("/projects/:id", p.handler.DeleteProject)

	// Webhook info (admin only — token is sensitive)
	a.GET("/projects/:id/webhook", p.handler.GetWebhookInfo)

	// Project actions (operator — operational, not config changes)
	o.POST("/projects/:id/build", p.handler.BuildProject)
	o.POST("/projects/:id/start", p.handler.StartProject)
	o.POST("/projects/:id/stop", p.handler.StopProject)
	o.POST("/projects/:id/rollback", p.handler.RollbackProject)

	// Build cache (admin)
	r.GET("/projects/:id/cache", p.handler.GetCacheInfo)
	a.DELETE("/projects/:id/cache", p.handler.ClearCache)

	// Environment cloning (admin)
	a.POST("/projects/:id/clone-env", p.handler.CloneEnvVars)

	// Cron jobs (admin mutations, read for list)
	r.GET("/projects/:id/crons", p.handler.ListCronJobs)
	a.POST("/projects/:id/crons", p.handler.CreateCronJob)
	a.PUT("/projects/:id/crons/:cronId", p.handler.UpdateCronJob)
	a.DELETE("/projects/:id/crons/:cronId", p.handler.DeleteCronJob)

	// Extra processes (admin mutations, read for list)
	r.GET("/projects/:id/processes", p.handler.ListExtraProcesses)
	a.POST("/projects/:id/processes", p.handler.CreateExtraProcess)
	a.PUT("/projects/:id/processes/:procId", p.handler.UpdateExtraProcess)
	a.DELETE("/projects/:id/processes/:procId", p.handler.DeleteExtraProcess)
	a.POST("/projects/:id/processes/:procId/restart", p.handler.RestartExtraProcess)

	// Deployments & logs (read)
	r.GET("/projects/:id/deployments", p.handler.GetDeployments)
	r.GET("/projects/:id/logs", p.handler.GetBuildLog)

	// Preview deployments (v0.14+). Webhook is unauthenticated (signed);
	// list is read-only; delete is admin because it tears down Caddy
	// hosts and containers.
	r.GET("/projects/:id/previews", p.handler.ListPreviews)
	a.DELETE("/previews/:previewId", p.handler.DeletePreview)

	// GitHub OAuth endpoints
	a.GET("/github/config", p.handler.GetGitHubConfig)
	a.PUT("/github/config", p.handler.SaveGitHubConfig)
	a.GET("/github/authorize", p.handler.GitHubAuthorize)
	a.GET("/github/installations", p.handler.ListGitHubInstallations)
	a.DELETE("/github/installations/:id", p.handler.DeleteGitHubInstallation)
	a.GET("/github/installations/:id/repos", p.handler.ListGitHubRepos)

	// Public routes (no JWT required)
	ctx.PublicRouter.GET("/github/callback", p.handler.GitHubCallback)
	ctx.PublicRouter.POST("/webhook/:token", p.handler.Webhook)

	// Subscribe to cross-plugin build trigger (used by AI tool use via CoreAPI).
	ctx.EventBus.Subscribe("deploy.trigger_build", func(e pluginpkg.Event) {
		if pid, ok := e.Payload["project_id"]; ok {
			var projectID uint
			switch v := pid.(type) {
			case uint:
				projectID = v
			case float64:
				projectID = uint(v)
			}
			if projectID > 0 {
				if err := p.svc.Build(projectID); err != nil && !errors.Is(err, ErrBuildCoalesced) {
					ctx.Logger.Error("trigger_build via event failed", "project_id", projectID, "err", err)
				}
			}
		}
	})

	ctx.Logger.Info("Deploy plugin routes registered")
	return nil
}

// Start is called after Init. Starts the cron scheduler, git poller, and
// the preview-deployment GC loop.
func (p *Plugin) Start() error {
	p.svc.StartCronScheduler()
	p.svc.StartGitPoller()
	p.svc.StartPreviewGC()
	return nil
}

// Stop cleans up resources.
func (p *Plugin) Stop() error {
	p.svc.StopGitPoller()
	p.svc.StopCronScheduler()
	p.svc.StopPreviewGC()
	return nil
}

// FrontendManifest declares the frontend routes for the deploy plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "deploy",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/deploy", Component: "ProjectList", Menu: true, Icon: "Rocket", Label: "Projects", LabelZh: "项目部署"},
			{Path: "/deploy/create", Component: "ProjectCreate", Label: "Create Project", LabelZh: "创建项目"},
			{Path: "/deploy/:id", Component: "ProjectDetail", Label: "Project Detail", LabelZh: "项目详情"},
		},
		MenuGroup: "deploy",
		MenuOrder: 20,
	}
}
