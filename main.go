package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/web-casa/webcasa/internal/auth"
	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/database"
	"github.com/web-casa/webcasa/internal/handler"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/plugin"
	"github.com/web-casa/webcasa/internal/service"
	aiplugin "github.com/web-casa/webcasa/plugins/ai"
	deployplugin "github.com/web-casa/webcasa/plugins/deploy"
	dockerplugin "github.com/web-casa/webcasa/plugins/docker"
	dbplugin "github.com/web-casa/webcasa/plugins/database"
	fmplugin "github.com/web-casa/webcasa/plugins/filemanager"
	appstoreplugin "github.com/web-casa/webcasa/plugins/appstore"
	mcpplugin "github.com/web-casa/webcasa/plugins/mcpserver"
	backupplugin "github.com/web-casa/webcasa/plugins/backup"
	monitoringplugin "github.com/web-casa/webcasa/plugins/monitoring"
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

	// CORS — allow frontend dev server and same-origin requests
	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length", "Content-Disposition"},
		AllowCredentials: false,
	}))

	// ============ API Routes ============
	api := r.Group("/api")

	// Public routes (no auth required)
	loginLimiter := auth.NewRateLimiter(5, 900) // 5 attempts per 15 minutes
	totpSvc := service.NewTOTPService(db, cfg)
	authH := handler.NewAuthHandler(db, cfg, loginLimiter, totpSvc)
	api.POST("/auth/login", authH.Login)
	api.POST("/auth/setup", authH.Setup)
	api.GET("/auth/need-setup", authH.NeedSetup)
	api.GET("/auth/altcha-challenge", authH.AltchaChallenge)

	// Protected routes (JWT required)
	protected := api.Group("")
	protected.Use(auth.Middleware(cfg.JWTSecret, auth.WithDB(db)))

	// Admin-only routes (JWT + admin role required)
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

	// Host CRUD
	hostH := handler.NewHostHandler(hostSvc, db)
	protected.GET("/hosts", hostH.List)
	protected.POST("/hosts", hostH.Create)
	protected.GET("/hosts/:id", hostH.Get)
	protected.PUT("/hosts/:id", hostH.Update)
	protected.DELETE("/hosts/:id", hostH.Delete)
	protected.PATCH("/hosts/:id/toggle", hostH.Toggle)
	protected.POST("/hosts/:id/clone", hostH.Clone)

	// SSL Certificate management
	certH := handler.NewCertHandler(hostSvc, cfg)
	protected.POST("/hosts/:id/cert", certH.Upload)
	protected.DELETE("/hosts/:id/cert", certH.Delete)

	// Caddy process control (admin only)
	caddyH := handler.NewCaddyHandler(caddyMgr, db)
	protected.GET("/caddy/status", caddyH.Status)
	adminOnly.POST("/caddy/start", caddyH.Start)
	adminOnly.POST("/caddy/stop", caddyH.Stop)
	adminOnly.POST("/caddy/reload", caddyH.Reload)
	protected.GET("/caddy/caddyfile", caddyH.GetCaddyfile)
	adminOnly.POST("/caddy/caddyfile", caddyH.SaveCaddyfile)
	adminOnly.POST("/caddy/fmt", caddyH.Format)
	adminOnly.POST("/caddy/validate", caddyH.Validate)

	// Log viewing
	logH := handler.NewLogHandler(cfg)
	protected.GET("/logs", logH.GetLogs)
	protected.GET("/logs/files", logH.ListLogFiles)
	protected.GET("/logs/download", logH.Download)

	// Config import/export (admin only)
	exportH := handler.NewExportHandler(hostSvc)
	protected.GET("/config/export", exportH.Export)
	adminOnly.POST("/config/import", exportH.Import)

	// User management (admin only)
	userH := handler.NewUserHandler(db)
	protected.GET("/users", userH.List)
	adminOnly.POST("/users", userH.Create)
	adminOnly.PUT("/users/:id", userH.Update)
	adminOnly.DELETE("/users/:id", userH.Delete)

	// Audit logs
	auditH := handler.NewAuditHandler(db)
	protected.GET("/audit/logs", auditH.List)

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
	protected.POST("/groups", groupH.Create)
	protected.PUT("/groups/:id", groupH.Update)
	protected.DELETE("/groups/:id", groupH.Delete)
	protected.POST("/groups/:id/batch-enable", groupH.BatchEnable)
	protected.POST("/groups/:id/batch-disable", groupH.BatchDisable)

	// Tags
	tagSvc := service.NewTagService(db)
	tagH := handler.NewTagHandler(tagSvc, db)
	protected.GET("/tags", tagH.List)
	protected.POST("/tags", tagH.Create)
	protected.PUT("/tags/:id", tagH.Update)
	protected.DELETE("/tags/:id", tagH.Delete)

	// Templates
	tplSvc := service.NewTemplateService(db, hostSvc)
	tplSvc.SeedPresets() // Seed preset templates if table is empty
	tplH := handler.NewTemplateHandler(tplSvc, db)
	protected.GET("/templates", tplH.List)
	protected.POST("/templates", tplH.Create)
	protected.PUT("/templates/:id", tplH.Update)
	protected.DELETE("/templates/:id", tplH.Delete)
	protected.POST("/templates/import", tplH.Import)
	protected.GET("/templates/:id/export", tplH.Export)
	protected.POST("/templates/:id/create-host", tplH.CreateHost)
	protected.POST("/hosts/:id/save-as-template", tplH.SaveAsTemplate)

	// Settings (admin only for mutations)
	settingH := handler.NewSettingHandler(db)
	protected.GET("/settings/all", settingH.GetAll)
	adminOnly.PUT("/settings", settingH.Update)

	// Certificates (admin only for mutations)
	certMgrH := handler.NewCertificateHandler(db, cfg)
	protected.GET("/certificates", certMgrH.List)
	adminOnly.POST("/certificates", certMgrH.Upload)
	adminOnly.DELETE("/certificates/:id", certMgrH.Delete)

	// ============ Plugin System ============
	pluginRouter := protected.Group("/plugins")
	publicPluginRouter := api.Group("/plugins") // public routes (no JWT) for webhooks etc.
	pluginMgr := initPlugins(db, pluginRouter, publicPluginRouter, hostSvc, caddyMgr, cfg)
	pluginH := handler.NewPluginHandler(pluginMgr)
	protected.GET("/plugins", pluginH.List)
	protected.POST("/plugins/:id/enable", pluginH.Enable)
	protected.POST("/plugins/:id/disable", pluginH.Disable)
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
func initPlugins(db *gorm.DB, protectedRouter *gin.RouterGroup, publicRouter *gin.RouterGroup, hostSvc *service.HostService, caddyMgr *caddy.Manager, cfg *config.Config) *plugin.Manager {
	coreAPI := plugin.NewCoreAPI(db, hostSvc, caddyMgr)
	pluginMgr := plugin.NewManager(db, protectedRouter, publicRouter, coreAPI, cfg.DataDir)

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
	// Future:

	// Initialise and start all enabled plugins.
	if err := pluginMgr.InitAll(); err != nil {
		log.Printf("⚠️  Plugin init failed: %v", err)
	}
	if err := pluginMgr.StartAll(); err != nil {
		log.Printf("⚠️  Plugin start failed: %v", err)
	}

	return pluginMgr
}

// setupFrontend serves the Vue SPA from web/dist if it exists
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

		// Try to serve the exact file
		filePath := filepath.Join(distPath, path)
		if _, err := os.Stat(filePath); err == nil {
			c.File(filePath)
			return
		}

		// SPA fallback
		c.File(filepath.Join(distPath, "index.html"))
	})

	log.Println("✅ Serving frontend from web/dist")
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
