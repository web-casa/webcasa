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
	authH := handler.NewAuthHandler(db, cfg)
	api.POST("/auth/login", authH.Login)
	api.POST("/auth/setup", authH.Setup)
	api.GET("/auth/need-setup", authH.NeedSetup)

	// Protected routes (JWT required)
	protected := api.Group("")
	protected.Use(auth.Middleware(cfg.JWTSecret))

	// User info
	protected.GET("/auth/me", authH.Me)

	// Host CRUD
	hostH := handler.NewHostHandler(hostSvc)
	protected.GET("/hosts", hostH.List)
	protected.POST("/hosts", hostH.Create)
	protected.GET("/hosts/:id", hostH.Get)
	protected.PUT("/hosts/:id", hostH.Update)
	protected.DELETE("/hosts/:id", hostH.Delete)
	protected.PATCH("/hosts/:id/toggle", hostH.Toggle)

	// Caddy process control
	caddyH := handler.NewCaddyHandler(caddyMgr)
	protected.GET("/caddy/status", caddyH.Status)
	protected.POST("/caddy/start", caddyH.Start)
	protected.POST("/caddy/stop", caddyH.Stop)
	protected.POST("/caddy/reload", caddyH.Reload)
	protected.GET("/caddy/caddyfile", caddyH.GetCaddyfile)

	// Log viewing
	logH := handler.NewLogHandler(cfg)
	protected.GET("/logs", logH.GetLogs)
	protected.GET("/logs/files", logH.ListLogFiles)
	protected.GET("/logs/download", logH.Download)

	// Config import/export
	exportH := handler.NewExportHandler(hostSvc)
	protected.GET("/config/export", exportH.Export)
	protected.POST("/config/import", exportH.Import)

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
