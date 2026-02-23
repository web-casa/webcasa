package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/caddypanel/caddypanel/internal/auth"
	"github.com/caddypanel/caddypanel/internal/caddy"
	"github.com/caddypanel/caddypanel/internal/config"
	"github.com/caddypanel/caddypanel/internal/database"
	"github.com/caddypanel/caddypanel/internal/handler"
	"github.com/caddypanel/caddypanel/internal/service"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z"
var Version = "dev"

func main() {
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
	challengeStore := auth.NewChallengeStore()
	authH := handler.NewAuthHandler(db, cfg, loginLimiter, challengeStore)
	api.POST("/auth/login", authH.Login)
	api.POST("/auth/setup", authH.Setup)
	api.GET("/auth/need-setup", authH.NeedSetup)
	api.GET("/auth/challenge", authH.Challenge)

	// Protected routes (JWT required)
	protected := api.Group("")
	protected.Use(auth.Middleware(cfg.JWTSecret))

	// User info
	protected.GET("/auth/me", authH.Me)

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

	// SSL Certificate management
	certH := handler.NewCertHandler(hostSvc, cfg)
	protected.POST("/hosts/:id/cert", certH.Upload)
	protected.DELETE("/hosts/:id/cert", certH.Delete)

	// Caddy process control
	caddyH := handler.NewCaddyHandler(caddyMgr, db)
	protected.GET("/caddy/status", caddyH.Status)
	protected.POST("/caddy/start", caddyH.Start)
	protected.POST("/caddy/stop", caddyH.Stop)
	protected.POST("/caddy/reload", caddyH.Reload)
	protected.GET("/caddy/caddyfile", caddyH.GetCaddyfile)
	protected.POST("/caddy/caddyfile", caddyH.SaveCaddyfile)
	protected.POST("/caddy/fmt", caddyH.Format)
	protected.POST("/caddy/validate", caddyH.Validate)

	// Log viewing
	logH := handler.NewLogHandler(cfg)
	protected.GET("/logs", logH.GetLogs)
	protected.GET("/logs/files", logH.ListLogFiles)
	protected.GET("/logs/download", logH.Download)

	// Config import/export
	exportH := handler.NewExportHandler(hostSvc)
	protected.GET("/config/export", exportH.Export)
	protected.POST("/config/import", exportH.Import)

	// User management
	userH := handler.NewUserHandler(db)
	protected.GET("/users", userH.List)
	protected.POST("/users", userH.Create)
	protected.PUT("/users/:id", userH.Update)
	protected.DELETE("/users/:id", userH.Delete)

	// Audit logs
	auditH := handler.NewAuditHandler(db)
	protected.GET("/audit/logs", auditH.List)

	// DNS providers
	dnsH := handler.NewDnsProviderHandler(db)
	protected.GET("/dns-providers", dnsH.List)
	protected.GET("/dns-providers/:id", dnsH.Get)
	protected.POST("/dns-providers", dnsH.Create)
	protected.PUT("/dns-providers/:id", dnsH.Update)
	protected.DELETE("/dns-providers/:id", dnsH.Delete)

	// Settings
	settingH := handler.NewSettingHandler(db)
	protected.GET("/settings/all", settingH.GetAll)
	protected.PUT("/settings", settingH.Update)

	// Certificates
	certMgrH := handler.NewCertificateHandler(db, cfg)
	protected.GET("/certificates", certMgrH.List)
	protected.POST("/certificates", certMgrH.Upload)
	protected.DELETE("/certificates/:id", certMgrH.Delete)

	// ============ Frontend Static Files ============
	setupFrontend(r)

	// Start server
	addr := ":" + cfg.Port
	log.Printf("🚀 CaddyPanel starting on http://localhost%s", addr)
	log.Printf("📁 Data directory: %s", cfg.DataDir)
	log.Printf("📄 Caddyfile path: %s", cfg.CaddyfilePath)

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
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
