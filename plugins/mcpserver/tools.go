package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// ToolService holds dependencies for MCP tool handlers.
type ToolService struct {
	db      *gorm.DB
	coreAPI plugin.CoreAPI
	caller  *InternalCaller
	token   string // will be set per-request via context
}

// contextKey is used to store the API token in context.
type contextKey string

const tokenContextKey contextKey = "api_token_str"
const permissionsContextKey contextKey = "api_token_permissions"

// ContextWithToken adds the API token string to the context.
func ContextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenContextKey, token)
}

// ContextWithPermissions adds the token permissions to the context.
func ContextWithPermissions(ctx context.Context, permissions string) context.Context {
	return context.WithValue(ctx, permissionsContextKey, permissions)
}

// tokenFromContext retrieves the API token from context.
func tokenFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tokenContextKey).(string); ok {
		return v
	}
	return ""
}

// checkPermission verifies the current token has the required permission scope.
// Scope format: "hosts:read", "hosts:write", "deploy:write", "docker:write", etc.
// An empty permissions list "[]" grants full access (backwards compatible).
func checkPermission(ctx context.Context, scope string) error {
	permsStr, _ := ctx.Value(permissionsContextKey).(string)
	if permsStr == "" || permsStr == "[]" {
		return nil // empty = full access (backwards compatible)
	}

	var perms []string
	if err := json.Unmarshal([]byte(permsStr), &perms); err != nil {
		return fmt.Errorf("permission denied: malformed permissions on token")
	}
	if len(perms) == 0 {
		return nil // empty array = full access
	}

	// Parse the requested scope: "hosts:write" → category="hosts", action="write"
	parts := splitScope(scope)
	category, action := parts[0], parts[1]

	for _, p := range perms {
		pp := splitScope(p)
		pCat, pAct := pp[0], pp[1]
		if pCat == "*" || pCat == category {
			if pAct == "*" || pAct == action {
				return nil
			}
		}
	}

	return fmt.Errorf("permission denied: token lacks %q scope", scope)
}

func splitScope(s string) [2]string {
	for i, c := range s {
		if c == ':' {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, "*"}
}

// requirePerm is a helper that checks permission and returns an error result if denied.
func requirePerm(ctx context.Context, scope string) (*mcp.CallToolResult, bool) {
	if err := checkPermission(ctx, scope); err != nil {
		r, _ := errorResult(err.Error())
		return r, true
	}
	return nil, false
}

// jsonText marshals v to JSON and returns it as a TextContent result.
func jsonText(v interface{}) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil
}

// errorResult returns an MCP error result.
func errorResult(msg string) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Error: " + msg}},
		IsError: true,
	}, nil
}

// RegisterTools adds all MCP tools to the server.
func (ts *ToolService) RegisterTools(srv *mcp.Server) {
	// ── Host Management ──
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_hosts",
		Title:       "List Reverse Proxy Hosts",
		Description: "List all reverse proxy hosts managed by WebCasa. Returns domain, upstream, TLS status, and enabled state.",
	}, ts.handleListHosts)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_host",
		Title:       "Create Reverse Proxy Host",
		Description: "Create a new reverse proxy host with domain, upstream address, and optional TLS/WebSocket settings.",
	}, ts.handleCreateHost)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_host",
		Title:       "Delete Reverse Proxy Host",
		Description: "Delete a reverse proxy host by its ID.",
	}, ts.handleDeleteHost)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "toggle_host",
		Title:       "Toggle Reverse Proxy Host",
		Description: "Enable or disable a reverse proxy host by its ID.",
	}, ts.handleToggleHost)

	// ── Deploy Management ──
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_projects",
		Title:       "List Deploy Projects",
		Description: "List all deployment projects. Returns project name, framework, status, domain, and git URL.",
	}, ts.handleListProjects)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_project",
		Title:       "Get Project Details",
		Description: "Get detailed information about a specific deployment project.",
	}, ts.handleGetProject)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "deploy_project",
		Title:       "Deploy a Project",
		Description: "Create a new project from a Git repository and trigger the first build. Supports Node.js, Go, Python, PHP frameworks.",
	}, ts.handleDeployProject)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "build_project",
		Title:       "Trigger Project Build",
		Description: "Trigger a new build for an existing project.",
	}, ts.handleBuildProject)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_build_logs",
		Title:       "Get Build Logs",
		Description: "Get the build logs for a project's latest or specific build.",
	}, ts.handleGetBuildLogs)

	// ── Docker Management ──
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_stacks",
		Title:       "List Docker Compose Stacks",
		Description: "List all Docker Compose stacks. Returns stack name, status, and service count.",
	}, ts.handleListStacks)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_stack",
		Title:       "Create Docker Compose Stack",
		Description: "Create a new Docker Compose stack from YAML content.",
	}, ts.handleCreateStack)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "control_stack",
		Title:       "Control Docker Stack",
		Description: "Perform an action on a Docker Compose stack: up, down, or restart.",
	}, ts.handleControlStack)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_stack_logs",
		Title:       "Get Stack Logs",
		Description: "Get recent logs from a Docker Compose stack.",
	}, ts.handleGetStackLogs)

	// ── Database Management ──
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_db_instances",
		Title:       "List Database Instances",
		Description: "List all managed database instances (MySQL, PostgreSQL, MariaDB, Redis).",
	}, ts.handleListDBInstances)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_db_instance",
		Title:       "Create Database Instance",
		Description: "Create a new database instance. Supported engines: mysql, postgres, mariadb, redis.",
	}, ts.handleCreateDBInstance)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "execute_query",
		Title:       "Execute SQL Query",
		Description: "Execute a read-only SQL query (SELECT/SHOW/DESCRIBE/EXPLAIN) on a database instance. Returns result rows as JSON.",
	}, ts.handleExecuteQuery)

	// ── AI Assistant ──
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "generate_compose",
		Title:       "Generate Docker Compose",
		Description: "Generate a Docker Compose YAML file from a natural language description using AI. Example: 'WordPress with MySQL and Redis'.",
	}, ts.handleGenerateCompose)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "diagnose_error",
		Title:       "Diagnose Error Logs",
		Description: "Analyze error logs or stack traces using AI to identify root causes and suggest fixes.",
	}, ts.handleDiagnoseError)
}

// ──────────────────────────── Host Tools ────────────────────────────

type listHostsInput struct {
	Search  string `json:"search,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

func (ts *ToolService) handleListHosts(ctx context.Context, req *mcp.CallToolRequest, input listHostsInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "hosts:read"); denied {
		return r, nil, nil
	}
	var hosts []model.Host
	q := ts.db.Preload("Upstreams").Order("domain ASC")
	if input.Search != "" {
		q = q.Where("domain LIKE ?", "%"+input.Search+"%")
	}
	if input.Enabled != nil {
		q = q.Where("enabled = ?", *input.Enabled)
	}
	if err := q.Find(&hosts).Error; err != nil {
		r, _ := errorResult("failed to list hosts: " + err.Error())
		return r, nil, nil
	}

	type hostSummary struct {
		ID        uint   `json:"id"`
		Domain    string `json:"domain"`
		HostType  string `json:"host_type"`
		Enabled   bool   `json:"enabled"`
		TLS       bool   `json:"tls_enabled"`
		Upstreams []string `json:"upstreams"`
	}
	summaries := make([]hostSummary, 0, len(hosts))
	for _, h := range hosts {
		ups := make([]string, 0, len(h.Upstreams))
		for _, u := range h.Upstreams {
			ups = append(ups, u.Address)
		}
		summaries = append(summaries, hostSummary{
			ID:        h.ID,
			Domain:    h.Domain,
			HostType:  h.HostType,
			Enabled:   h.Enabled != nil && *h.Enabled,
			TLS:       h.TLSEnabled != nil && *h.TLSEnabled,
			Upstreams: ups,
		})
	}
	r, err := jsonText(summaries)
	return r, nil, err
}

type createHostInput struct {
	Domain    string `json:"domain" jsonschema:"required"`
	Upstream  string `json:"upstream" jsonschema:"required"`
	TLS       *bool  `json:"tls,omitempty"`
	WebSocket *bool  `json:"websocket,omitempty"`
}

func (ts *ToolService) handleCreateHost(ctx context.Context, req *mcp.CallToolRequest, input createHostInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "hosts:write"); denied {
		return r, nil, nil
	}
	tlsEnabled := true
	if input.TLS != nil {
		tlsEnabled = *input.TLS
	}
	ws := false
	if input.WebSocket != nil {
		ws = *input.WebSocket
	}

	hostID, err := ts.coreAPI.CreateHost(plugin.CreateHostRequest{
		Domain:       input.Domain,
		UpstreamAddr: input.Upstream,
		TLSEnabled:   tlsEnabled,
		HTTPRedirect: tlsEnabled,
		WebSocket:    ws,
	})
	if err != nil {
		r, _ := errorResult("failed to create host: " + err.Error())
		return r, nil, nil
	}

	r, _ := jsonText(map[string]interface{}{
		"host_id": hostID,
		"domain":  input.Domain,
		"message": "Host created successfully",
	})
	return r, nil, nil
}

type deleteHostInput struct {
	HostID uint `json:"host_id" jsonschema:"required"`
}

func (ts *ToolService) handleDeleteHost(ctx context.Context, req *mcp.CallToolRequest, input deleteHostInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "hosts:write"); denied {
		return r, nil, nil
	}
	if err := ts.coreAPI.DeleteHost(input.HostID); err != nil {
		r, _ := errorResult("failed to delete host: " + err.Error())
		return r, nil, nil
	}
	r, _ := jsonText(map[string]string{"message": "Host deleted successfully"})
	return r, nil, nil
}

type toggleHostInput struct {
	HostID uint `json:"host_id" jsonschema:"required"`
}

func (ts *ToolService) handleToggleHost(ctx context.Context, req *mcp.CallToolRequest, input toggleHostInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "hosts:write"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	_, err := ts.caller.Call("PATCH", "/api/hosts/"+strconv.FormatUint(uint64(input.HostID), 10)+"/toggle", nil, token)
	if err != nil {
		r, _ := errorResult("failed to toggle host: " + err.Error())
		return r, nil, nil
	}
	r, _ := jsonText(map[string]string{"message": "Host toggled successfully"})
	return r, nil, nil
}

// ──────────────────────────── Deploy Tools ────────────────────────────

type emptyInput struct{}

func (ts *ToolService) handleListProjects(ctx context.Context, req *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "deploy:read"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	data, err := ts.caller.Get("/api/plugins/deploy/projects", token)
	if err != nil {
		r, _ := errorResult("failed to list projects: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type getProjectInput struct {
	ProjectID uint `json:"project_id" jsonschema:"required"`
}

func (ts *ToolService) handleGetProject(ctx context.Context, req *mcp.CallToolRequest, input getProjectInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "deploy:read"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	data, err := ts.caller.Get("/api/plugins/deploy/projects/"+strconv.FormatUint(uint64(input.ProjectID), 10), token)
	if err != nil {
		r, _ := errorResult("failed to get project: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type deployProjectInput struct {
	Name      string `json:"name" jsonschema:"required"`
	GitURL    string `json:"git_url" jsonschema:"required"`
	Branch    string `json:"branch,omitempty"`
	Framework string `json:"framework,omitempty"`
	Domain    string `json:"domain,omitempty"`
}

func (ts *ToolService) handleDeployProject(ctx context.Context, req *mcp.CallToolRequest, input deployProjectInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "deploy:write"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	body := map[string]interface{}{
		"name":    input.Name,
		"git_url": input.GitURL,
	}
	if input.Branch != "" {
		body["branch"] = input.Branch
	}
	if input.Framework != "" {
		body["framework"] = input.Framework
	}
	if input.Domain != "" {
		body["domain"] = input.Domain
	}

	// Create project
	data, err := ts.caller.Post("/api/plugins/deploy/projects", body, token)
	if err != nil {
		r, _ := errorResult("failed to create project: " + err.Error())
		return r, nil, nil
	}

	// Extract project ID and trigger build
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err == nil {
		if id, ok := result["id"].(float64); ok {
			_, _ = ts.caller.Post("/api/plugins/deploy/projects/"+strconv.FormatInt(int64(id), 10)+"/build", nil, token)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type buildProjectInput struct {
	ProjectID uint `json:"project_id" jsonschema:"required"`
}

func (ts *ToolService) handleBuildProject(ctx context.Context, req *mcp.CallToolRequest, input buildProjectInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "deploy:write"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	data, err := ts.caller.Post("/api/plugins/deploy/projects/"+strconv.FormatUint(uint64(input.ProjectID), 10)+"/build", nil, token)
	if err != nil {
		r, _ := errorResult("failed to trigger build: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type getBuildLogsInput struct {
	ProjectID   uint `json:"project_id" jsonschema:"required"`
	BuildNumber int  `json:"build_number,omitempty"`
}

func (ts *ToolService) handleGetBuildLogs(ctx context.Context, req *mcp.CallToolRequest, input getBuildLogsInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "deploy:read"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	path := fmt.Sprintf("/api/plugins/deploy/projects/%d/logs?type=build", input.ProjectID)
	if input.BuildNumber > 0 {
		path += "&build=" + strconv.Itoa(input.BuildNumber)
	}
	data, err := ts.caller.Get(path, token)
	if err != nil {
		r, _ := errorResult("failed to get build logs: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// ──────────────────────────── Docker Tools ────────────────────────────

func (ts *ToolService) handleListStacks(ctx context.Context, req *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "docker:read"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	data, err := ts.caller.Get("/api/plugins/docker/stacks", token)
	if err != nil {
		r, _ := errorResult("failed to list stacks: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type createStackInput struct {
	Name        string `json:"name" jsonschema:"required"`
	ComposeYAML string `json:"compose_yaml" jsonschema:"required"`
	EnvContent  string `json:"env,omitempty"`
}

func (ts *ToolService) handleCreateStack(ctx context.Context, req *mcp.CallToolRequest, input createStackInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "docker:write"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	body := map[string]interface{}{
		"name":         input.Name,
		"compose_file": input.ComposeYAML,
	}
	if input.EnvContent != "" {
		body["env_file"] = input.EnvContent
	}
	data, err := ts.caller.Post("/api/plugins/docker/stacks", body, token)
	if err != nil {
		r, _ := errorResult("failed to create stack: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type controlStackInput struct {
	StackID uint   `json:"stack_id" jsonschema:"required"`
	Action  string `json:"action" jsonschema:"required"`
}

func (ts *ToolService) handleControlStack(ctx context.Context, req *mcp.CallToolRequest, input controlStackInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "docker:write"); denied {
		return r, nil, nil
	}
	// Validate action against allowed values to prevent path traversal
	switch input.Action {
	case "up", "down", "restart":
		// valid
	default:
		r, _ := errorResult("invalid action: must be 'up', 'down', or 'restart'")
		return r, nil, nil
	}

	token := tokenFromContext(ctx)
	path := fmt.Sprintf("/api/plugins/docker/stacks/%d/%s", input.StackID, input.Action)
	data, err := ts.caller.Post(path, nil, token)
	if err != nil {
		r, _ := errorResult("failed to " + input.Action + " stack: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type getStackLogsInput struct {
	StackID uint `json:"stack_id" jsonschema:"required"`
	Tail    int  `json:"tail,omitempty"`
}

func (ts *ToolService) handleGetStackLogs(ctx context.Context, req *mcp.CallToolRequest, input getStackLogsInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "docker:read"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	tail := 100
	if input.Tail > 0 {
		tail = input.Tail
	}
	path := fmt.Sprintf("/api/plugins/docker/stacks/%d/logs?tail=%d", input.StackID, tail)
	data, err := ts.caller.Get(path, token)
	if err != nil {
		r, _ := errorResult("failed to get stack logs: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// ──────────────────────────── Database Tools ────────────────────────────

func (ts *ToolService) handleListDBInstances(ctx context.Context, req *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "database:read"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	data, err := ts.caller.Get("/api/plugins/database/instances", token)
	if err != nil {
		r, _ := errorResult("failed to list database instances: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type createDBInstanceInput struct {
	Engine  string `json:"engine" jsonschema:"required"`
	Name    string `json:"name" jsonschema:"required"`
	Version string `json:"version,omitempty"`
}

func (ts *ToolService) handleCreateDBInstance(ctx context.Context, req *mcp.CallToolRequest, input createDBInstanceInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "database:write"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	body := map[string]interface{}{
		"engine": input.Engine,
		"name":   input.Name,
	}
	if input.Version != "" {
		body["version"] = input.Version
	}
	data, err := ts.caller.Post("/api/plugins/database/instances", body, token)
	if err != nil {
		r, _ := errorResult("failed to create database instance: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type executeQueryInput struct {
	InstanceID uint   `json:"instance_id" jsonschema:"required"`
	Database   string `json:"database" jsonschema:"required"`
	Query      string `json:"query" jsonschema:"required"`
}

func (ts *ToolService) handleExecuteQuery(ctx context.Context, req *mcp.CallToolRequest, input executeQueryInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "database:write"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	body := map[string]interface{}{
		"database": input.Database,
		"query":    input.Query,
		"limit":    100,
	}
	path := fmt.Sprintf("/api/plugins/database/instances/%d/execute", input.InstanceID)
	data, err := ts.caller.Post(path, body, token)
	if err != nil {
		r, _ := errorResult("failed to execute query: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// ──────────────────────────── AI Tools ────────────────────────────

type generateComposeInput struct {
	Description string `json:"description" jsonschema:"required"`
}

func (ts *ToolService) handleGenerateCompose(ctx context.Context, req *mcp.CallToolRequest, input generateComposeInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "ai:read"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	// The AI generate-compose endpoint uses SSE, but we need a synchronous result.
	// We'll call it and collect the full response.
	body := map[string]interface{}{
		"description": input.Description,
	}
	data, err := ts.caller.Post("/api/plugins/ai/generate-compose", body, token)
	if err != nil {
		// If SSE doesn't work with our caller, provide a helpful message
		r, _ := errorResult("failed to generate compose: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

type diagnoseErrorInput struct {
	Logs    string `json:"logs" jsonschema:"required"`
	Context string `json:"context,omitempty"`
}

func (ts *ToolService) handleDiagnoseError(ctx context.Context, req *mcp.CallToolRequest, input diagnoseErrorInput) (*mcp.CallToolResult, any, error) {
	if r, denied := requirePerm(ctx, "ai:read"); denied {
		return r, nil, nil
	}
	token := tokenFromContext(ctx)
	body := map[string]interface{}{
		"logs":    input.Logs,
		"context": input.Context,
	}
	data, err := ts.caller.Post("/api/plugins/ai/diagnose", body, token)
	if err != nil {
		r, _ := errorResult("failed to diagnose error: " + err.Error())
		return r, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
