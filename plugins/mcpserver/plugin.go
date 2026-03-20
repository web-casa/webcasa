package mcpserver

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the MCP Server plugin.
type Plugin struct {
	mcpHandler *mcp.StreamableHTTPHandler
}

// New creates a new MCP Server plugin.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns plugin metadata.
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		ID:           "mcpserver",
		Name:         "MCP Server",
		Version:      "1.0.0",
		Description:  "Model Context Protocol server for AI IDE integration (Cursor, Windsurf, Claude Code)",
		Author:       "WebCasa",
		Dependencies: []string{},
		Priority:     90, // load last
		Icon:         "Cpu",
		Category:     "tool",
	}
}

// Init initialises the plugin: migrates tables, creates services, registers routes.
func (p *Plugin) Init(ctx *plugin.Context) error {
	db := ctx.DB
	logger := ctx.Logger

	// Migrate the api_tokens table (core table, not plugin-prefixed)
	if err := db.AutoMigrate(&APIToken{}); err != nil {
		return err
	}

	// Services
	tokenSvc := NewTokenService(db)
	handler := NewHandler(tokenSvc)

	// Determine the panel port for internal calls
	port := "39921"
	if v, err := ctx.CoreAPI.GetSetting("port"); err == nil && v != "" {
		port = v
	}
	caller := NewInternalCaller(port)

	// Create MCP server
	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "webcasa",
		Title:   "WebCasa MCP Server",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		Instructions: "WebCasa is an AI-First lightweight server management panel. Use these tools to manage reverse proxies, deploy projects, control Docker stacks, manage databases, and generate Docker Compose configurations.",
		Logger:       slog.New(logger.Handler()),
	})

	// Register all MCP tools
	toolSvc := &ToolService{
		db:      db,
		coreAPI: ctx.CoreAPI,
		caller:  caller,
	}
	toolSvc.RegisterTools(mcpSrv)

	// Create Streamable HTTP handler
	p.mcpHandler = mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return mcpSrv },
		nil,
	)

	// ── Routes ──

	// Token management (admin-only: only admins can create/delete API tokens)
	ctx.Router.GET("/tokens", handler.ListTokens)
	ctx.AdminRouter.POST("/tokens", handler.CreateToken)
	ctx.AdminRouter.DELETE("/tokens/:id", handler.DeleteToken)

	// MCP protocol endpoint (public route — authenticates via API token internally)
	ctx.PublicRouter.Any("/mcp", p.mcpMiddleware(tokenSvc), gin.WrapH(p.mcpHandler))
	// Some MCP clients send to /mcp/ with trailing slash or use GET for SSE
	ctx.PublicRouter.Any("/mcp/*path", p.mcpMiddleware(tokenSvc), func(c *gin.Context) {
		p.mcpHandler.ServeHTTP(c.Writer, c.Request)
	})

	logger.Info("MCP Server plugin initialized")
	return nil
}

// mcpMiddleware validates API tokens for MCP requests.
func (p *Plugin) mcpMiddleware(tokenSvc *TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract Bearer token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required. Use Bearer token."})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			c.Abort()
			return
		}

		tokenStr := parts[1]
		if !strings.HasPrefix(tokenStr, "wc_") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "MCP endpoint requires an API token (wc_...)"})
			c.Abort()
			return
		}

		token, err := tokenSvc.ValidateToken(tokenStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		// Store token info and the raw token string in context for internal calls
		c.Set("user_id", token.UserID)
		c.Set("api_token", true)
		c.Set("api_token_str", tokenStr)

		// Also set it in the request context so MCP tool handlers can access it
		ctx := ContextWithToken(c.Request.Context(), tokenStr)
		ctx = ContextWithPermissions(ctx, token.Permissions)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// Start is called after Init.
func (p *Plugin) Start() error {
	return nil
}

// Stop is called during shutdown.
func (p *Plugin) Stop() error {
	return nil
}

// FrontendManifest declares frontend routes.
func (p *Plugin) FrontendManifest() plugin.FrontendManifest {
	return plugin.FrontendManifest{
		ID: "mcpserver",
		Routes: []plugin.FrontendRoute{
			{
				Path:    "/mcp",
				Menu:    true,
				Icon:    "Cpu",
				Label:   "MCP Server",
				LabelZh: "MCP 服务",
			},
		},
		MenuGroup: "tool",
		MenuOrder: 10,
	}
}

// Compile-time interface checks.
var (
	_ plugin.Plugin           = (*Plugin)(nil)
	_ plugin.FrontendProvider = (*Plugin)(nil)
)
