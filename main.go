package main

import (
	"bufio"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/web-casa/webcasa/internal/auth"
	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/database"
	"github.com/web-casa/webcasa/internal/handler"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/notify"
	"github.com/web-casa/webcasa/internal/plugin"
	"github.com/web-casa/webcasa/internal/service"
	"github.com/web-casa/webcasa/internal/versioncheck"
	aiplugin "github.com/web-casa/webcasa/plugins/ai"
	deployplugin "github.com/web-casa/webcasa/plugins/deploy"
	dockerplugin "github.com/web-casa/webcasa/plugins/docker"
	dbplugin "github.com/web-casa/webcasa/plugins/database"
	fmplugin "github.com/web-casa/webcasa/plugins/filemanager"
	appstoreplugin "github.com/web-casa/webcasa/plugins/appstore"
	mcpplugin "github.com/web-casa/webcasa/plugins/mcpserver"
	backupplugin "github.com/web-casa/webcasa/plugins/backup"
	monitoringplugin "github.com/web-casa/webcasa/plugins/monitoring"
	firewallplugin "github.com/web-casa/webcasa/plugins/firewall"
	phpplugin "github.com/web-casa/webcasa/plugins/php"
	cronjobplugin "github.com/web-casa/webcasa/plugins/cronjob"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z"
var Version = "dev"

func main() {
	// Handle CLI commands before starting the server
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--reset-password", "-reset-password", "reset-password":
			resetPassword()
			return
		case "--version", "-v":
			fmt.Printf("WebCasa v%s\n", Version)
			return
		}
	}

	// Load configuration
	cfg := config.Load()

	// Initialize database
	db := database.Init(cfg.DBPath)

	// Initialize Caddy manager
	caddyMgr := caddy.NewManager(cfg)

	// Initialize services
	hostSvc := service.NewHostService(db, caddyMgr, cfg)

	// Ensure a valid Caddyfile exists on startup
	// This generates it from the database (even if empty → minimal global options)
	if err := caddyMgr.EnsureCaddyfile(); err != nil {
		log.Printf("⚠️  Failed to ensure Caddyfile: %v", err)
	}
	if err := hostSvc.ApplyConfig(); err != nil {
		log.Printf("⚠️  Failed to apply initial config: %v", err)
	}

	// Auto-start Caddy if not already running
	if !caddyMgr.IsRunning() {
		log.Println("Caddy not running, auto-starting...")
		if err := caddyMgr.Start(); err != nil {
			log.Printf("⚠️  Failed to auto-start Caddy: %v", err)
		}
	}

	// Setup Gin
	r := gin.Default()

	// CORS — dynamic origin check: same-origin + localhost dev + WEBCASA_CORS_ORIGINS
	corsOrigins := os.Getenv("WEBCASA_CORS_ORIGINS") // comma-separated extra origins
	r.Use(cors.New(cors.Config{
		AllowOriginWithContextFunc: func(c *gin.Context, origin string) bool {
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			hostname := u.Hostname()
			// Allow localhost/127.0.0.1 for development
			if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
				return true
			}
			// Allow same-origin: compare full host:port from origin against request Host header.
			// u.Host includes port (e.g., "example.com:8080"), c.Request.Host also includes port.
			// Also infer scheme from the request to compare schemes.
			originHost := u.Host // host:port (or just host if default port)
			reqHost := c.Request.Host
			if originHost == reqHost {
				// Hosts match; also verify scheme matches.
				originScheme := u.Scheme
				reqScheme := "http"
				if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
					reqScheme = "https"
				}
				if originScheme == reqScheme {
					return true
				}
			}
			// Allow if full origin URL matches configured extra origins
			if corsOrigins != "" {
				for _, allowed := range strings.Split(corsOrigins, ",") {
					if strings.TrimSpace(allowed) == origin {
						return true
					}
				}
			}
			return false
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length", "Content-Disposition"},
		AllowCredentials: false,
	}))

	// ============ API Routes ============
	api := r.Group("/api")

	// Public routes (no auth required)
	limiters := auth.NewLimiters()
	totpSvc := service.NewTOTPService(db, cfg)
	authH := handler.NewAuthHandler(db, cfg, limiters, totpSvc)
	api.POST("/auth/login", authH.Login)
	api.POST("/auth/setup", authH.Setup)
	api.GET("/auth/need-setup", authH.NeedSetup)
	api.GET("/auth/altcha-challenge", authH.AltchaChallenge)

	// Protected routes (JWT required)
	protected := api.Group("")
	protected.Use(auth.Middleware(cfg.JWTSecret, auth.WithDB(db)))

	// Operator routes (JWT + operator/admin/owner role required)
	operatorOnly := api.Group("")
	operatorOnly.Use(auth.Middleware(cfg.JWTSecret, auth.WithDB(db)))
	operatorOnly.Use(auth.RequireOperator(db))

	// Admin-only routes (JWT + admin/owner role required)
	adminOnly := api.Group("")
	adminOnly.Use(auth.Middleware(cfg.JWTSecret, auth.WithDB(db)))
	adminOnly.Use(auth.RequireAdmin(db))

	// User info
	protected.GET("/auth/me", authH.Me)

	// 2FA TOTP endpoints
	protected.POST("/auth/2fa/setup", authH.Setup2FA)
	protected.POST("/auth/2fa/verify", authH.Verify2FA)
	protected.POST("/auth/2fa/disable", authH.Disable2FA)

	// Dashboard stats
	dashH := handler.NewDashboardHandler(hostSvc, caddyMgr, Version)
	protected.GET("/dashboard/stats", dashH.Stats)
	protected.GET("/news", dashH.News)

	// Host CRUD
	hostH := handler.NewHostHandler(hostSvc, db)
	protected.GET("/hosts", hostH.List)
	adminOnly.POST("/hosts", hostH.Create)
	protected.GET("/hosts/:id", hostH.Get)
	adminOnly.PUT("/hosts/:id", hostH.Update)
	adminOnly.DELETE("/hosts/:id", hostH.Delete)
	operatorOnly.PATCH("/hosts/:id/toggle", hostH.Toggle)
	adminOnly.POST("/hosts/:id/clone", hostH.Clone)

	// SSL Certificate management (admin only — modifies TLS config)
	certH := handler.NewCertHandler(hostSvc, cfg)
	adminOnly.POST("/hosts/:id/cert", certH.Upload)
	adminOnly.DELETE("/hosts/:id/cert", certH.Delete)

	// Caddy process control (operator for start/stop/reload, admin for config)
	caddyH := handler.NewCaddyHandler(caddyMgr, db)
	protected.GET("/caddy/status", caddyH.Status)
	operatorOnly.POST("/caddy/start", caddyH.Start)
	operatorOnly.POST("/caddy/stop", caddyH.Stop)
	operatorOnly.POST("/caddy/reload", caddyH.Reload)
	adminOnly.GET("/caddy/check-upgrade", caddyH.CheckUpgrade)
	adminOnly.POST("/caddy/upgrade", caddyH.Upgrade)
	adminOnly.GET("/caddy/caddyfile", caddyH.GetCaddyfile)
	adminOnly.POST("/caddy/caddyfile", caddyH.SaveCaddyfile)
	adminOnly.POST("/caddy/fmt", caddyH.Format)
	adminOnly.POST("/caddy/validate", caddyH.Validate)

	// Log viewing
	logH := handler.NewLogHandler(cfg)
	protected.GET("/logs", logH.GetLogs)
	protected.GET("/logs/files", logH.ListLogFiles)
	protected.GET("/logs/download", logH.Download)
	protected.GET("/logs/system", logH.GetSystemLog)

	// Config import/export (admin only)
	exportH := handler.NewExportHandler(hostSvc)
	adminOnly.GET("/config/export", exportH.Export)
	adminOnly.POST("/config/import", exportH.Import)

	// User management (admin only)
	userH := handler.NewUserHandler(db)
	adminOnly.GET("/users", userH.List)
	adminOnly.POST("/users", userH.Create)
	adminOnly.PUT("/users/:id", userH.Update)
	adminOnly.DELETE("/users/:id", userH.Delete)

	// Audit logs (admin only — contains user actions, IPs, sensitive context)
	auditH := handler.NewAuditHandler(db)
	adminOnly.GET("/audit/logs", auditH.List)

	// DNS providers (admin only for mutations)
	dnsH := handler.NewDnsProviderHandler(db)
	protected.GET("/dns-providers", dnsH.List)
	protected.GET("/dns-providers/:id", dnsH.Get)
	adminOnly.POST("/dns-providers", dnsH.Create)
	adminOnly.PUT("/dns-providers/:id", dnsH.Update)
	adminOnly.DELETE("/dns-providers/:id", dnsH.Delete)

	// DNS Check
	dnsCheckSvc := service.NewDnsCheckService(db)
	dnsCheckH := handler.NewDnsCheckHandler(dnsCheckSvc, db)
	protected.GET("/dns-check", dnsCheckH.Check)

	// Groups
	groupSvc := service.NewGroupService(db, caddyMgr, cfg, hostSvc)
	groupH := handler.NewGroupHandler(groupSvc, db)
	protected.GET("/groups", groupH.List)
	adminOnly.POST("/groups", groupH.Create)
	adminOnly.PUT("/groups/:id", groupH.Update)
	adminOnly.DELETE("/groups/:id", groupH.Delete)
	adminOnly.POST("/groups/:id/batch-enable", groupH.BatchEnable)
	adminOnly.POST("/groups/:id/batch-disable", groupH.BatchDisable)

	// Tags
	tagSvc := service.NewTagService(db)
	tagH := handler.NewTagHandler(tagSvc, db)
	protected.GET("/tags", tagH.List)
	adminOnly.POST("/tags", tagH.Create)
	adminOnly.PUT("/tags/:id", tagH.Update)
	adminOnly.DELETE("/tags/:id", tagH.Delete)

	// Templates
	tplSvc := service.NewTemplateService(db, hostSvc)
	tplSvc.SeedPresets() // Seed preset templates if table is empty
	tplH := handler.NewTemplateHandler(tplSvc, db)
	protected.GET("/templates", tplH.List)
	adminOnly.POST("/templates", tplH.Create)
	adminOnly.PUT("/templates/:id", tplH.Update)
	adminOnly.DELETE("/templates/:id", tplH.Delete)
	adminOnly.POST("/templates/import", tplH.Import)
	protected.GET("/templates/:id/export", tplH.Export)
	adminOnly.POST("/templates/:id/create-host", tplH.CreateHost)
	adminOnly.POST("/hosts/:id/save-as-template", tplH.SaveAsTemplate)

	// Settings (admin only — may contain sensitive values)
	settingH := handler.NewSettingHandler(db)
	adminOnly.GET("/settings/all", settingH.GetAll)
	adminOnly.PUT("/settings", settingH.Update)

	// Notifications
	notifier := notify.NewNotifier(db, slog.Default())
	notifyH := handler.NewNotifyHandler(notifier)
	adminOnly.GET("/notify/channels", notifyH.ListChannels)
	adminOnly.POST("/notify/channels", notifyH.CreateChannel)
	adminOnly.PUT("/notify/channels/:id", notifyH.UpdateChannel)
	adminOnly.DELETE("/notify/channels/:id", notifyH.DeleteChannel)
	adminOnly.POST("/notify/channels/:id/test", notifyH.TestChannel)

	// Certificates (admin only — contains file paths)
	certMgrH := handler.NewCertificateHandler(db, cfg)
	adminOnly.GET("/certificates", certMgrH.List)
	adminOnly.POST("/certificates", certMgrH.Upload)
	adminOnly.DELETE("/certificates/:id", certMgrH.Delete)

	// ============ Plugin System ============
	pluginRouter := protected.Group("/plugins")
	operatorPluginRouter := operatorOnly.Group("/plugins")
	adminPluginRouter := adminOnly.Group("/plugins")
	publicPluginRouter := api.Group("/plugins") // public routes (no JWT) for webhooks etc.
	pluginMgr := initPlugins(db, pluginRouter, operatorPluginRouter, adminPluginRouter, publicPluginRouter, hostSvc, caddyMgr, cfg)

	// ============ Notification Integration ============
	// Subscribe notifier to EventBus for deploy/backup/monitoring events
	eventBus := pluginMgr.EventBus()
	eventBus.Subscribe("deploy.*", func(e plugin.Event) {
		title := formatEventTitle(e)
		notifier.Send(notify.NotifyEvent{
			Type: e.Type, Title: title, Message: formatEventMessage(e), Data: e.Payload, Time: e.Time,
		})
	})
	eventBus.Subscribe("backup.*", func(e plugin.Event) {
		title := formatEventTitle(e)
		notifier.Send(notify.NotifyEvent{
			Type: e.Type, Title: title, Message: formatEventMessage(e), Data: e.Payload, Time: e.Time,
		})
	})
	eventBus.Subscribe("monitoring.alert.*", func(e plugin.Event) {
		title := formatEventTitle(e)
		notifier.Send(notify.NotifyEvent{
			Type: e.Type, Title: title, Message: formatEventMessage(e), Data: e.Payload, Time: e.Time,
		})
	})
	eventBus.Subscribe("system.inspection.*", func(e plugin.Event) {
		title := formatEventTitle(e)
		notifier.Send(notify.NotifyEvent{
			Type: e.Type, Title: title, Message: formatEventMessage(e), Data: e.Payload, Time: e.Time,
		})
	})
	eventBus.Subscribe("system.selfheal.*", func(e plugin.Event) {
		title := formatEventTitle(e)
		notifier.Send(notify.NotifyEvent{
			Type: e.Type, Title: title, Message: formatEventMessage(e), Data: e.Payload, Time: e.Time,
		})
	})
	eventBus.Subscribe("cronjob.task.failed", func(e plugin.Event) {
		title := formatEventTitle(e)
		notifier.Send(notify.NotifyEvent{
			Type: e.Type, Title: title, Message: formatEventMessage(e), Data: e.Payload, Time: e.Time,
		})
	})

	// ============ Version Checker ============
	versionChecker := versioncheck.NewChecker(
		"https://raw.githubusercontent.com/web-casa/webcasa/main/versions.json",
		slog.Default(),
	)
	versionChecker.Start()

	versionH := handler.NewVersionHandler(versionChecker)
	protected.GET("/version-check", versionH.Check)

	pluginH := handler.NewPluginHandler(pluginMgr)
	protected.GET("/plugins", pluginH.List)
	adminOnly.POST("/plugins/:id/enable", pluginH.Enable)
	adminOnly.POST("/plugins/:id/disable", pluginH.Disable)
	adminOnly.POST("/plugins/:id/sidebar", pluginH.SetSidebarVisibility)
	adminOnly.POST("/plugins/:id/install", pluginH.Install)
	protected.GET("/plugins/frontend-manifests", pluginH.FrontendManifests)

	// ============ Frontend Static Files ============
	setupFrontend(r)

	// Start server
	addr := ":" + cfg.Port
	log.Printf("🚀 WebCasa starting on http://localhost%s", addr)
	log.Printf("📁 Data directory: %s", cfg.DataDir)
	log.Printf("📄 Caddyfile path: %s", cfg.CaddyfilePath)

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// initPlugins creates the plugin manager and registers all compiled-in plugins.
// New plugins are added here by calling pluginMgr.Register(...).
func initPlugins(db *gorm.DB, protectedRouter *gin.RouterGroup, operatorRouter *gin.RouterGroup, adminRouter *gin.RouterGroup, publicRouter *gin.RouterGroup, hostSvc *service.HostService, caddyMgr *caddy.Manager, cfg *config.Config) *plugin.Manager {
	coreAPI := plugin.NewCoreAPI(db, hostSvc, caddyMgr, cfg.DataDir, cfg.JWTSecret)
	pluginMgr := plugin.NewManager(db, protectedRouter, operatorRouter, adminRouter, publicRouter, coreAPI, cfg.DataDir)
	coreAPI.SetEventBus(pluginMgr.EventBus())

	// ── Register plugins here ──
	if err := pluginMgr.Register(dockerplugin.New()); err != nil {
		log.Printf("⚠️  Register docker plugin: %v", err)
	}
	if err := pluginMgr.Register(deployplugin.New()); err != nil {
		log.Printf("⚠️  Register deploy plugin: %v", err)
	}
	if err := pluginMgr.Register(aiplugin.New()); err != nil {
		log.Printf("⚠️  Register ai plugin: %v", err)
	}
	if err := pluginMgr.Register(fmplugin.New()); err != nil {
		log.Printf("⚠️  Register filemanager plugin: %v", err)
	}
	if err := pluginMgr.Register(dbplugin.New()); err != nil {
		log.Printf("⚠️  Register database plugin: %v", err)
	}
	if err := pluginMgr.Register(monitoringplugin.New()); err != nil {
		log.Printf("⚠️  Register monitoring plugin: %v", err)
	}
	if err := pluginMgr.Register(backupplugin.New()); err != nil {
		log.Printf("⚠️  Register backup plugin: %v", err)
	}
	if err := pluginMgr.Register(appstoreplugin.New()); err != nil {
		log.Printf("⚠️  Register appstore plugin: %v", err)
	}
	if err := pluginMgr.Register(mcpplugin.New()); err != nil {
		log.Printf("⚠️  Register mcpserver plugin: %v", err)
	}
	if err := pluginMgr.Register(firewallplugin.New()); err != nil {
		log.Printf("⚠️  Register firewall plugin: %v", err)
	}
	if err := pluginMgr.Register(phpplugin.New()); err != nil {
		log.Printf("⚠️  Register php plugin: %v", err)
	}
	if err := pluginMgr.Register(cronjobplugin.New()); err != nil {
		log.Printf("⚠️  Register cronjob plugin: %v", err)
	}

	// Initialise and start all enabled plugins.
	if err := pluginMgr.InitAll(); err != nil {
		log.Printf("⚠️  Plugin init failed: %v", err)
	}
	if err := pluginMgr.StartAll(); err != nil {
		log.Printf("⚠️  Plugin start failed: %v", err)
	}

	return pluginMgr
}

// setupFrontend serves the React SPA from web/dist if it exists
func setupFrontend(r *gin.Engine) {
	distPath := "web/dist"

	if _, err := os.Stat(distPath); os.IsNotExist(err) {
		log.Println("⚠️  Frontend dist not found. Run 'cd web && npm run build' to build the frontend.")
		log.Println("   For development, run the Vite dev server: cd web && npm run dev")
		return
	}

	// Serve static assets
	r.Static("/assets", filepath.Join(distPath, "assets"))

	// Serve favicon and other root files
	r.StaticFile("/favicon.ico", filepath.Join(distPath, "favicon.ico"))

	// SPA fallback: serve index.html for all non-API, non-asset routes
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Don't interfere with API routes
		if strings.HasPrefix(path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
			return
		}

		// Resolve and validate the path stays within distPath
		cleaned := filepath.Clean(path)
		filePath := filepath.Join(distPath, cleaned)
		absDistPath, _ := filepath.Abs(distPath)
		absFilePath, _ := filepath.Abs(filePath)
		if !strings.HasPrefix(absFilePath, absDistPath+string(filepath.Separator)) && absFilePath != absDistPath {
			// Path traversal attempt — fall back to index.html
			c.File(filepath.Join(distPath, "index.html"))
			return
		}

		// Try to serve the exact file
		if _, err := os.Stat(filePath); err == nil {
			c.File(filePath)
			return
		}

		// SPA fallback
		c.File(filepath.Join(distPath, "index.html"))
	})

	log.Println("✅ Serving frontend from web/dist")
}

// formatEventTitle generates a human-readable title for a notification event.
func formatEventTitle(e plugin.Event) string {
	projectName, _ := e.Payload["project_name"].(string)
	switch e.Type {
	case "deploy.build.failed":
		return fmt.Sprintf("Build Failed: %s", projectName)
	case "deploy.build.success":
		return fmt.Sprintf("Build Success: %s", projectName)
	case "deploy.trigger_build":
		return fmt.Sprintf("Build Triggered: %s", projectName)
	case "cronjob.task.failed":
		taskName, _ := e.Payload["task_name"].(string)
		return fmt.Sprintf("Cron Job Failed: %s", taskName)
	default:
		return e.Type
	}
}

// formatEventMessage generates a detailed message for a notification event.
func formatEventMessage(e plugin.Event) string {
	var parts []string
	for k, v := range e.Payload {
		if k == "log_tail" {
			continue // skip large log content
		}
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}
	if len(parts) == 0 {
		return e.Type
	}
	return strings.Join(parts, "\n")
}

// resetPassword handles the --reset-password CLI command
func resetPassword() {
	fmt.Println("🔐 WebCasa — 密码重置工具")
	fmt.Println("============================")

	// Load config to get DB path
	cfg := config.Load()
	db := database.Init(cfg.DBPath)

	reader := bufio.NewReader(os.Stdin)

	// Get username
	fmt.Print("请输入用户名 (默认 admin): ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}

	// Get password
	fmt.Print("请输入新密码 (至少8位): ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		fmt.Println("❌ 密码长度不能少于8位")
		os.Exit(1)
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("❌ 密码加密失败: %v\n", err)
		os.Exit(1)
	}

	// Check if user exists
	var user model.User
	result := db.Where("username = ?", username).First(&user)
	if result.Error != nil {
		// User doesn't exist — create new
		user = model.User{
			Username: username,
			Password: string(hash),
			Role:     "admin",
		}
		if err := db.Create(&user).Error; err != nil {
			fmt.Printf("❌ 创建用户失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ 已创建管理员账户: %s\n", username)
	} else {
		// User exists — update password
		user.Password = string(hash)
		if err := db.Save(&user).Error; err != nil {
			fmt.Printf("❌ 更新密码失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ 已重置用户 %s 的密码\n", username)
	}

	fmt.Println("\n请重启 WebCasa 服务后使用新密码登录:")
	fmt.Println("  systemctl restart webcasa")
}
