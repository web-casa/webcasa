package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// RegisterBuiltinTools registers all built-in tools with the registry.
func RegisterBuiltinTools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "list_hosts",
		Description: "List all reverse proxy sites (domains) managed by the panel, including their status, TLS settings, and upstream addresses.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.ListHosts()
		},
	})

	r.Register(&Tool{
		Name:        "get_host",
		Description: "Get detailed information about a specific reverse proxy site by its ID.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The host ID",
				},
			},
			"required": []string{"id"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			return r.coreAPI.GetHost(p.ID)
		},
	})

	r.Register(&Tool{
		Name:        "create_host",
		Description: "Create a new reverse proxy site. Sets up a domain pointing to an upstream address with optional TLS and WebSocket support.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "The domain name, e.g. 'app.example.com'",
				},
				"upstream_addr": map[string]interface{}{
					"type":        "string",
					"description": "The upstream address, e.g. 'localhost:3000'",
				},
				"tls_enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable automatic HTTPS/TLS",
				},
				"http_redirect": map[string]interface{}{
					"type":        "boolean",
					"description": "Redirect HTTP to HTTPS",
				},
				"websocket": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable WebSocket proxying",
				},
			},
			"required": []string{"domain", "upstream_addr"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler:           createHostHandler(r),
	})

	r.Register(&Tool{
		Name:        "list_projects",
		Description: "List all deployment projects with their status, framework, domain, and build info.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.ListProjects()
		},
	})

	r.Register(&Tool{
		Name:        "get_project",
		Description: "Get detailed information about a specific deployment project by its ID.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID",
				},
			},
			"required": []string{"id"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			return r.coreAPI.GetProject(p.ID)
		},
	})

	r.Register(&Tool{
		Name:        "get_build_log",
		Description: "Get the build log for a specific project deployment. Use this to diagnose build failures.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID",
				},
				"build_num": map[string]interface{}{
					"type":        "number",
					"description": "The build number (use the project's current_build field)",
				},
			},
			"required": []string{"project_id", "build_num"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID uint `json:"project_id"`
				BuildNum  int  `json:"build_num"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			log, err := r.coreAPI.GetBuildLog(p.ProjectID, p.BuildNum)
			if err != nil {
				return nil, err
			}
			// Truncate very long logs for token efficiency
			if len(log) > 8000 {
				log = log[len(log)-8000:]
				log = "... (truncated, showing last 8000 chars)\n" + log
			}
			return map[string]interface{}{"log": log}, nil
		},
	})

	r.Register(&Tool{
		Name:        "get_runtime_log",
		Description: "Get the runtime (stdout/stderr) log of a running project. Useful for debugging runtime errors.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID",
				},
				"lines": map[string]interface{}{
					"type":        "number",
					"description": "Number of recent log lines to return (default: 100, max: 500)",
				},
			},
			"required": []string{"project_id"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID uint `json:"project_id"`
				Lines     int  `json:"lines"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if p.Lines <= 0 {
				p.Lines = 100
			}
			if p.Lines > 500 {
				p.Lines = 500
			}
			log, err := r.coreAPI.GetRuntimeLog(p.ProjectID, p.Lines)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"log": log}, nil
		},
	})

	r.Register(&Tool{
		Name:        "deploy_project",
		Description: "Trigger a new build and deployment for a project. The build runs asynchronously — use get_project to check status.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID to deploy",
				},
			},
			"required": []string{"project_id"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID uint `json:"project_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.TriggerBuild(p.ProjectID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "build_triggered", "project_id": p.ProjectID}, nil
		},
	})

	r.Register(&Tool{
		Name:        "create_project",
		Description: "Create a new deployment project from a Git repository. Automatically detects the framework and triggers the first build. Use this when a user asks to deploy a project or app.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Project name, e.g. 'my-app'",
				},
				"git_url": map[string]interface{}{
					"type":        "string",
					"description": "Git repository URL (HTTPS or SSH), e.g. 'https://github.com/user/repo.git'",
				},
				"git_branch": map[string]interface{}{
					"type":        "string",
					"description": "Git branch to deploy (default: 'main')",
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Domain for the project, e.g. 'app.example.com'. Leave empty to skip reverse proxy setup.",
				},
				"framework": map[string]interface{}{
					"type":        "string",
					"description": "Framework preset: nextjs, nuxt, vite, remix, express, go, laravel, flask, django, dockerfile, custom. Leave empty for auto-detection.",
				},
				"deploy_mode": map[string]interface{}{
					"type":        "string",
					"description": "Deployment mode: 'bare' (systemd process) or 'docker' (container). Default: 'bare'.",
					"enum":        []string{"bare", "docker"},
				},
			},
			"required": []string{"name", "git_url"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler:           createProjectHandler(r),
	})

	r.Register(&Tool{
		Name:        "suggest_env_vars",
		Description: "Get recommended environment variables for a specific framework. Returns common env vars with default values and descriptions.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"framework": map[string]interface{}{
					"type":        "string",
					"description": "Framework name: nextjs, nuxt, vite, remix, express, go, laravel, flask, django",
				},
			},
			"required": []string{"framework"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Framework string `json:"framework"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			suggestions, err := r.coreAPI.GetEnvSuggestions(p.Framework)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"framework":   p.Framework,
				"suggestions": suggestions,
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "generate_dockerfile",
		Description: "Generate an optimized, production-ready Dockerfile based on a project description. Uses multi-stage builds, slim base images, and layer caching best practices.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_description": map[string]interface{}{
					"type":        "string",
					"description": "Description of the project, e.g. 'A Next.js 14 app with Prisma ORM that connects to PostgreSQL' or 'A Go API server using Gin framework'",
				},
			},
			"required": []string{"project_description"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectDescription string `json:"project_description"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			// This tool needs access to the service for LLM generation.
			// We use the registry's svc reference.
			dockerfile, err := r.svc.GenerateDockerfileSync(ctx, p.ProjectDescription)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"dockerfile": dockerfile}, nil
		},
	})

	r.Register(&Tool{
		Name:        "docker_ps",
		Description: "List all Docker containers on the server, including their status, image, ports, and names.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.DockerPS()
		},
	})

	r.Register(&Tool{
		Name:        "docker_logs",
		Description: "Get recent logs from a Docker container. Useful for debugging container issues.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"container_id": map[string]interface{}{
					"type":        "string",
					"description": "The container ID or name",
				},
				"tail": map[string]interface{}{
					"type":        "number",
					"description": "Number of recent log lines (default: 100, max: 500)",
				},
			},
			"required": []string{"container_id"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ContainerID string `json:"container_id"`
				Tail        int    `json:"tail"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if p.Tail <= 0 {
				p.Tail = 100
			}
			if p.Tail > 500 {
				p.Tail = 500
			}
			log, err := r.coreAPI.DockerLogs(p.ContainerID, p.Tail)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"log": log}, nil
		},
	})

	r.Register(&Tool{
		Name:        "get_system_metrics",
		Description: "Get current server system metrics: CPU load, memory usage, and disk usage.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.GetMetrics()
		},
	})

	r.Register(&Tool{
		Name:        "run_command",
		Description: "Execute a shell command on the server. Use for diagnostics like 'df -h', 'free -m', 'ps aux', 'netstat -tlnp', etc. Dangerous commands are blocked.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The shell command to execute",
				},
				"timeout": map[string]interface{}{
					"type":        "number",
					"description": "Timeout in seconds (default: 30, max: 120)",
				},
			},
			"required": []string{"command"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			output, err := r.coreAPI.RunCommand(p.Command, p.Timeout)
			if err != nil {
				return map[string]interface{}{"output": output, "error": err.Error()}, nil
			}
			return map[string]interface{}{"output": output}, nil
		},
	})

	// ──────────────────────────────────────────────
	// Batch 2: read_file, list_dir, trigger_backup, update_host
	// ──────────────────────────────────────────────

	r.Register(&Tool{
		Name:        "read_file",
		Description: "Read the contents of a file on the server. Useful for inspecting config files, logs, source code, etc. Restricted to safe directories.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute file path to read",
				},
				"max_lines": map[string]interface{}{
					"type":        "number",
					"description": "Maximum number of lines to return (default: 200, max: 1000)",
				},
			},
			"required": []string{"path"},
		}),
		ReadOnly: true,
		Handler:  readFileHandler(),
	})

	r.Register(&Tool{
		Name:        "list_dir",
		Description: "List contents of a directory on the server. Returns file names, types, sizes and modification times. Restricted to safe directories.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute directory path to list",
				},
			},
			"required": []string{"path"},
		}),
		ReadOnly: true,
		Handler:  listDirHandler(),
	})

	r.Register(&Tool{
		Name:        "trigger_backup",
		Description: "Trigger an immediate system backup. The backup runs asynchronously in the background.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			if err := r.coreAPI.TriggerBackup(); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "backup_triggered"}, nil
		},
	})

	// Batch 3: Database, Docker, AppStore, File tools
	registerDatabaseTools(r)
	registerDockerExtraTools(r)
	registerAppStoreTools(r)
	registerFileWriteTools(r)

	// Batch 5: Memory tools
	registerMemoryTools(r)

	// Batch 4 tools
	registerBatch4Tools(r)

	r.Register(&Tool{
		Name:        "update_host",
		Description: "Update an existing reverse proxy site's configuration. Can change upstream address, TLS settings, WebSocket, compression, and enabled status. Caddy is reloaded automatically after changes.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The host ID to update",
				},
				"upstream": map[string]interface{}{
					"type":        "string",
					"description": "New upstream address, e.g. 'localhost:8080'",
				},
				"tls_mode": map[string]interface{}{
					"type":        "string",
					"description": "TLS mode: auto, dns, custom, off",
					"enum":        []string{"auto", "dns", "custom", "off"},
				},
				"force_https": map[string]interface{}{
					"type":        "boolean",
					"description": "Redirect HTTP to HTTPS",
				},
				"websocket": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable WebSocket proxying",
				},
				"compression": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable gzip/zstd compression",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable or disable the host",
				},
			},
			"required": []string{"id"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler:  updateHostHandler(r),
	})
}

// ──────────────────────────────────────────────
// Tool handler implementations
// ──────────────────────────────────────────────

// isPathSafe checks if a file path is within allowed directories.
// It resolves symlinks to prevent escape via symlink traversal and uses
// proper directory boundary checks (e.g. /home must not match /home2).
func isPathSafe(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Resolve symlinks to get the real path.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If the file doesn't exist yet, EvalSymlinks fails.
		// Try resolving the parent directory instead.
		dir := filepath.Dir(abs)
		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			return false
		}
		resolved = filepath.Join(resolvedDir, filepath.Base(abs))
	}

	// Block sensitive files
	blocked := []string{"/etc/shadow", "/etc/gshadow", "/etc/sudoers", "/etc/passwd"}
	for _, b := range blocked {
		if resolved == b {
			return false
		}
	}

	// Allow common safe directories.
	// Use filepath boundary check: resolved path must be exactly the allowed dir
	// or start with allowedDir + "/" to prevent /home matching /home2.
	allowed := []string{
		"/etc/caddy", "/etc/nginx",
		"/var/log",
		"/home", "/root",
		"/opt", "/srv",
		"/tmp",
	}
	for _, a := range allowed {
		if resolved == a || strings.HasPrefix(resolved, a+"/") {
			return true
		}
	}
	return false
}

func readFileHandler() ToolHandler {
	return func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var p struct {
			Path     string `json:"path"`
			MaxLines int    `json:"max_lines"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}
		if p.MaxLines <= 0 {
			p.MaxLines = 200
		}
		if p.MaxLines > 1000 {
			p.MaxLines = 1000
		}

		if !isPathSafe(p.Path) {
			return nil, fmt.Errorf("access denied: path %q is outside allowed directories", p.Path)
		}

		f, err := os.Open(p.Path)
		if err != nil {
			return nil, fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		var lines []string
		for scanner.Scan() && len(lines) < p.MaxLines {
			lines = append(lines, scanner.Text())
		}

		content := strings.Join(lines, "\n")
		if len(content) > 32000 {
			content = content[:32000] + "\n... (truncated)"
		}

		return map[string]interface{}{
			"path":       p.Path,
			"lines":      len(lines),
			"content":    content,
		}, nil
	}
}

func listDirHandler() ToolHandler {
	return func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}

		if !isPathSafe(p.Path) {
			return nil, fmt.Errorf("access denied: path %q is outside allowed directories", p.Path)
		}

		entries, err := os.ReadDir(p.Path)
		if err != nil {
			return nil, fmt.Errorf("read dir: %w", err)
		}

		type fileEntry struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Size     int64  `json:"size"`
			Modified string `json:"modified"`
		}

		result := make([]fileEntry, 0, len(entries))
		for _, e := range entries {
			info, _ := e.Info()
			entry := fileEntry{
				Name: e.Name(),
				Type: "file",
			}
			if e.IsDir() {
				entry.Type = "dir"
			} else if info != nil && info.Mode()&fs.ModeSymlink != 0 {
				entry.Type = "symlink"
			}
			if info != nil {
				entry.Size = info.Size()
				entry.Modified = info.ModTime().Format("2006-01-02 15:04:05")
			}
			result = append(result, entry)
		}

		return map[string]interface{}{
			"path":    p.Path,
			"entries": result,
			"count":   len(result),
		}, nil
	}
}

func updateHostHandler(r *ToolRegistry) ToolHandler {
	return func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var p struct {
			ID          uint   `json:"id"`
			Upstream    string `json:"upstream"`
			TLSMode     string `json:"tls_mode"`
			ForceHTTPS  *bool  `json:"force_https"`
			WebSocket   *bool  `json:"websocket"`
			Compression *bool  `json:"compression"`
			Enabled     *bool  `json:"enabled"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}

		err := r.coreAPI.UpdateHost(p.ID, pluginpkg.UpdateHostRequest{
			Upstream:    p.Upstream,
			TLSMode:     p.TLSMode,
			ForceHTTPS:  p.ForceHTTPS,
			WebSocket:   p.WebSocket,
			Compression: p.Compression,
			Enabled:     p.Enabled,
		})
		if err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"host_id": p.ID,
			"status":  "updated",
		}, nil
	}
}

// createHostHandler returns the handler for the create_host tool.
func createHostHandler(r *ToolRegistry) ToolHandler {
	return func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var p struct {
			Domain       string `json:"domain"`
			UpstreamAddr string `json:"upstream_addr"`
			TLSEnabled   bool   `json:"tls_enabled"`
			HTTPRedirect bool   `json:"http_redirect"`
			WebSocket    bool   `json:"websocket"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}

		hostID, err := r.coreAPI.CreateHost(pluginpkg.CreateHostRequest{
			Domain:       p.Domain,
			UpstreamAddr: p.UpstreamAddr,
			TLSEnabled:   p.TLSEnabled,
			HTTPRedirect: p.HTTPRedirect,
			WebSocket:    p.WebSocket,
		})
		if err != nil {
			return nil, err
		}

		// Auto-reload Caddy to apply the new config.
		if reloadErr := r.coreAPI.ReloadCaddy(); reloadErr != nil {
			return map[string]interface{}{
				"host_id": hostID,
				"warning": "Host created but Caddy reload failed: " + reloadErr.Error(),
			}, nil
		}

		return map[string]interface{}{
			"host_id": hostID,
			"domain":  p.Domain,
			"status":  "created",
		}, nil
	}
}

// createProjectHandler returns the handler for the create_project tool.
func createProjectHandler(r *ToolRegistry) ToolHandler {
	return func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var p struct {
			Name       string `json:"name"`
			GitURL     string `json:"git_url"`
			GitBranch  string `json:"git_branch"`
			Domain     string `json:"domain"`
			Framework  string `json:"framework"`
			DeployMode string `json:"deploy_mode"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}

		projectID, err := r.coreAPI.CreateProject(pluginpkg.CreateProjectRequest{
			Name:       p.Name,
			GitURL:     p.GitURL,
			GitBranch:  p.GitBranch,
			Domain:     p.Domain,
			Framework:  p.Framework,
			DeployMode: p.DeployMode,
			AutoDeploy: true,
		})
		if err != nil {
			return nil, err
		}

		// Trigger the first build asynchronously.
		if triggerErr := r.coreAPI.TriggerBuild(projectID); triggerErr != nil {
			return map[string]interface{}{
				"project_id": projectID,
				"status":     "created",
				"warning":    "Project created but first build trigger failed: " + triggerErr.Error(),
			}, nil
		}

		return map[string]interface{}{
			"project_id": projectID,
			"name":       p.Name,
			"status":     "created_and_building",
		}, nil
	}
}

// ──────────────────────────────────────────────
// Batch 4: diagnose_runtime, review_code, suggest_rollback, summarize_alerts
// ──────────────────────────────────────────────

func registerBatch4Tools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "diagnose_runtime",
		Description: "Analyze a running project's recent runtime logs with AI to diagnose errors, crashes, or performance issues. Complementary to build log diagnosis — this focuses on runtime behavior.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID to diagnose",
				},
			},
			"required": []string{"project_id"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID uint `json:"project_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			log, err := r.coreAPI.GetRuntimeLog(p.ProjectID, 100)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(log) == "" {
				return map[string]interface{}{"diagnosis": "No runtime logs available for this project."}, nil
			}
			// Get project info for context
			proj, _ := r.coreAPI.GetProject(p.ProjectID)
			contextStr := fmt.Sprintf("Project ID: %d", p.ProjectID)
			if proj != nil {
				if name, ok := proj["name"].(string); ok {
					contextStr += ", Name: " + name
				}
				if fw, ok := proj["framework"].(string); ok {
					contextStr += ", Framework: " + fw
				}
			}
			diagnosis, err := r.svc.DiagnoseSync(ctx, DiagnoseRequest{
				Logs:    log,
				Context: contextStr + " (runtime logs)",
			})
			if err != nil {
				return nil, fmt.Errorf("AI diagnosis failed: %w", err)
			}
			return map[string]interface{}{"diagnosis": diagnosis}, nil
		},
	})

	r.Register(&Tool{
		Name:        "review_code",
		Description: "Perform an AI code review on a project's source code before deployment. Checks for security issues, misconfigurations, performance problems, and best practices.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID to review",
				},
			},
			"required": []string{"project_id"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID uint `json:"project_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			review, err := r.svc.ReviewCodeSync(ctx, p.ProjectID)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"review": review}, nil
		},
	})

	r.Register(&Tool{
		Name:        "suggest_rollback",
		Description: "Analyze a project's recent deployment history and runtime status to suggest whether a rollback is needed, and if so, which version to rollback to.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID to analyze for rollback",
				},
			},
			"required": []string{"project_id"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID uint `json:"project_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			suggestion, err := r.svc.SuggestRollbackSync(ctx, p.ProjectID)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"suggestion": suggestion}, nil
		},
	})

	r.Register(&Tool{
		Name:        "summarize_alerts",
		Description: "Get a summary of recent monitoring alerts with AI analysis of trends, potential root causes, and recommended actions.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			summary, err := r.svc.SummarizeAlertsSync(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"summary": summary}, nil
		},
	})
}

// ──────────────────────────────────────────────
// Batch 3: Database tools
// ──────────────────────────────────────────────

func registerDatabaseTools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "database_list_instances",
		Description: "List all database instances (MySQL, PostgreSQL, MariaDB, Redis) managed by the panel, including their status and connection info.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.DatabaseListInstances()
		},
	})

	r.Register(&Tool{
		Name:        "database_create_instance",
		Description: "Create a new database instance (MySQL, PostgreSQL, MariaDB, or Redis) running in a Docker container.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"engine": map[string]interface{}{
					"type":        "string",
					"description": "Database engine: mysql, postgres, mariadb, redis",
					"enum":        []string{"mysql", "postgres", "mariadb", "redis"},
				},
				"version": map[string]interface{}{
					"type":        "string",
					"description": "Engine version, e.g. '8.0', '16', '7.2'",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Display name for this instance",
				},
				"port": map[string]interface{}{
					"type":        "number",
					"description": "Host port to bind (e.g. 3306, 5432, 6379)",
				},
				"root_password": map[string]interface{}{
					"type":        "string",
					"description": "Root/admin password for the database",
				},
				"memory_limit": map[string]interface{}{
					"type":        "string",
					"description": "Memory limit, e.g. '512m', '1g'",
				},
			},
			"required": []string{"engine", "name", "root_password"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p pluginpkg.DatabaseCreateInstanceRequest
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			id, err := r.coreAPI.DatabaseCreateInstance(p)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"status":      "instance_creation_triggered",
				"instance_id": id,
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "database_create_database",
		Description: "Create a new logical database within an existing database instance.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"instance_id": map[string]interface{}{
					"type":        "number",
					"description": "The database instance ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Database name to create",
				},
				"charset": map[string]interface{}{
					"type":        "string",
					"description": "Character set (default: utf8mb4 for MySQL, utf8 for PostgreSQL)",
				},
			},
			"required": []string{"instance_id", "name"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				InstanceID uint   `json:"instance_id"`
				Name       string `json:"name"`
				Charset    string `json:"charset"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.DatabaseCreateDatabase(p.InstanceID, p.Name, p.Charset); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "created", "database": p.Name}, nil
		},
	})

	r.Register(&Tool{
		Name:        "database_create_user",
		Description: "Create a database user and grant access to specified databases.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"instance_id": map[string]interface{}{
					"type":        "number",
					"description": "The database instance ID",
				},
				"username": map[string]interface{}{
					"type":        "string",
					"description": "Username to create",
				},
				"password": map[string]interface{}{
					"type":        "string",
					"description": "Password for the new user",
				},
				"databases": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "List of database names to grant access to",
				},
			},
			"required": []string{"instance_id", "username", "password"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				InstanceID uint     `json:"instance_id"`
				Username   string   `json:"username"`
				Password   string   `json:"password"`
				Databases  []string `json:"databases"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.DatabaseCreateUser(p.InstanceID, p.Username, p.Password, p.Databases); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "created", "username": p.Username}, nil
		},
	})

	r.Register(&Tool{
		Name:        "database_execute_query",
		Description: "Execute a read-only SQL query (SELECT, SHOW, DESCRIBE, EXPLAIN) on a database instance. Write operations are blocked for safety.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"instance_id": map[string]interface{}{
					"type":        "number",
					"description": "The database instance ID",
				},
				"database": map[string]interface{}{
					"type":        "string",
					"description": "Database name to query (optional for SHOW commands)",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "SQL query to execute (read-only: SELECT, SHOW, DESCRIBE, EXPLAIN)",
				},
			},
			"required": []string{"instance_id", "query"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				InstanceID uint   `json:"instance_id"`
				Database   string `json:"database"`
				Query      string `json:"query"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			return r.coreAPI.DatabaseExecuteQuery(p.InstanceID, p.Database, p.Query)
		},
	})
}

// ──────────────────────────────────────────────
// Batch 3: Docker extra tools
// ──────────────────────────────────────────────

func registerDockerExtraTools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "docker_list_stacks",
		Description: "List all Docker Compose stacks managed by the panel, including their status.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.DockerListStacks()
		},
	})

	r.Register(&Tool{
		Name:        "docker_manage_container",
		Description: "Start, stop, or restart a Docker container by its ID or name.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"container_id": map[string]interface{}{
					"type":        "string",
					"description": "The container ID or name",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"description": "Action to perform: start, stop, restart",
					"enum":        []string{"start", "stop", "restart"},
				},
			},
			"required": []string{"container_id", "action"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ContainerID string `json:"container_id"`
				Action      string `json:"action"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.DockerManageContainer(p.ContainerID, p.Action); err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"status":       p.Action + "ed",
				"container_id": p.ContainerID,
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "docker_run_container",
		Description: "Create and run a new Docker container from an image. Supports port mapping, environment variables, volumes, and restart policy.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image": map[string]interface{}{
					"type":        "string",
					"description": "Docker image, e.g. 'nginx:latest', 'redis:7-alpine'",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Container name",
				},
				"ports": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Port mappings, e.g. ['8080:80', '443:443']",
				},
				"env": map[string]interface{}{
					"type":        "object",
					"description": "Environment variables as key-value pairs",
				},
				"volumes": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Volume mounts, e.g. ['/data:/var/lib/data']",
				},
				"restart_policy": map[string]interface{}{
					"type":        "string",
					"description": "Restart policy: no, always, unless-stopped, on-failure",
					"enum":        []string{"no", "always", "unless-stopped", "on-failure"},
				},
			},
			"required": []string{"image"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p pluginpkg.DockerRunContainerRequest
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			containerID, err := r.coreAPI.DockerRunContainer(p)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"status":       "running",
				"container_id": containerID,
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "docker_pull_image",
		Description: "Pull a Docker image from a registry. Use this before running a container if the image is not available locally.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image": map[string]interface{}{
					"type":        "string",
					"description": "Image name with optional tag, e.g. 'nginx:latest', 'postgres:16'",
				},
			},
			"required": []string{"image"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Image string `json:"image"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.DockerPullImage(p.Image); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "pulled", "image": p.Image}, nil
		},
	})

	r.Register(&Tool{
		Name:        "docker_get_container_stats",
		Description: "Get real-time CPU, memory, network, and disk I/O statistics for a Docker container.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"container_id": map[string]interface{}{
					"type":        "string",
					"description": "The container ID or name",
				},
			},
			"required": []string{"container_id"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ContainerID string `json:"container_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			return r.coreAPI.DockerGetContainerStats(p.ContainerID)
		},
	})
}

// ──────────────────────────────────────────────
// Batch 3: App Store tools
// ──────────────────────────────────────────────

func registerAppStoreTools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "appstore_search_apps",
		Description: "Search the app store for available applications by name or description.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query (app name or keyword)",
				},
			},
			"required": []string{"query"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			return r.coreAPI.AppStoreSearchApps(p.Query)
		},
	})

	r.Register(&Tool{
		Name:        "appstore_install_app",
		Description: "Install an application from the app store. The installation runs asynchronously via Docker.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"app_id": map[string]interface{}{
					"type":        "string",
					"description": "The app ID to install",
				},
				"config": map[string]interface{}{
					"type":        "object",
					"description": "Configuration parameters for the app (varies per app)",
				},
			},
			"required": []string{"app_id"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				AppID  string                 `json:"app_id"`
				Config map[string]interface{} `json:"config"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			id, err := r.coreAPI.AppStoreInstallApp(p.AppID, p.Config)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"status":       "install_triggered",
				"installed_id": id,
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "appstore_list_installed",
		Description: "List all applications installed from the app store with their status.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.AppStoreListInstalled()
		},
	})
}

// ──────────────────────────────────────────────
// Batch 3: File write tools
// ──────────────────────────────────────────────

func registerFileWriteTools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "write_file",
		Description: "Create or overwrite a file on the server. Restricted to safe directories. Max file size: 1MB.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute file path to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "File content to write",
				},
			},
			"required": []string{"path", "content"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.FileWrite(p.Path, p.Content); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "written", "path": p.Path, "size": len(p.Content)}, nil
		},
	})

	r.Register(&Tool{
		Name:        "delete_file",
		Description: "Delete a file or directory on the server. Restricted to safe directories. Use with caution.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to delete",
				},
			},
			"required": []string{"path"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.FileDelete(p.Path); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "deleted", "path": p.Path}, nil
		},
	})

	r.Register(&Tool{
		Name:        "rename_file",
		Description: "Rename or move a file/directory on the server. Both source and destination must be in safe directories.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"old_path": map[string]interface{}{
					"type":        "string",
					"description": "Current absolute path",
				},
				"new_path": map[string]interface{}{
					"type":        "string",
					"description": "New absolute path",
				},
			},
			"required": []string{"old_path", "new_path"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				OldPath string `json:"old_path"`
				NewPath string `json:"new_path"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.FileRename(p.OldPath, p.NewPath); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "renamed", "old_path": p.OldPath, "new_path": p.NewPath}, nil
		},
	})
}

// ──────────────────────────────────────────────
// Batch 5: Memory tools
// ──────────────────────────────────────────────

func registerMemoryTools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "save_memory",
		Description: "Save a fact or piece of information to persistent memory for future conversations. Use this to remember server configurations, troubleshooting results, user preferences, and deployment details.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The fact or information to remember",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Category: server_config, troubleshooting, user_preference, deployment, general",
					"enum":        []string{"server_config", "troubleshooting", "user_preference", "deployment", "general"},
				},
				"importance": map[string]interface{}{
					"type":        "number",
					"description": "Importance score from 0.0 to 1.0 (default: 0.5)",
				},
			},
			"required": []string{"content"},
		}),
		ReadOnly: false,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Content    string  `json:"content"`
				Category   string  `json:"category"`
				Importance float32 `json:"importance"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if p.Importance == 0 {
				p.Importance = 0.5
			}
			mem, err := r.svc.memory.SaveMemory(p.Content, p.Category, p.Importance, nil)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"status":   "saved",
				"id":       mem.ID,
				"category": mem.Category,
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "search_memory",
		Description: "Search persistent memory for relevant facts from previous conversations. Use this to recall server configurations, past troubleshooting, user preferences, etc.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query describing what to look for",
				},
				"limit": map[string]interface{}{
					"type":        "number",
					"description": "Maximum number of results (default: 8)",
				},
			},
			"required": []string{"query"},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			memories, err := r.svc.memory.SearchMemories(p.Query, p.Limit)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"count":    len(memories),
				"memories": memories,
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "list_memories",
		Description: "List all stored memories with optional category filter. Returns paginated results.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Filter by category (optional)",
				},
				"page": map[string]interface{}{
					"type":        "number",
					"description": "Page number (default: 1)",
				},
			},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Category string `json:"category"`
				Page     int    `json:"page"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			memories, total, err := r.svc.memory.ListMemories(p.Page, 20, p.Category)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"total":    total,
				"page":     p.Page,
				"memories": memories,
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "delete_memory",
		Description: "Delete a specific memory by its ID. Use this to remove outdated or incorrect information.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The memory ID to delete",
				},
			},
			"required": []string{"id"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.svc.memory.DeleteMemory(p.ID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "deleted", "id": p.ID}, nil
		},
	})

	// ── Firewall tools ──

	r.Register(&Tool{
		Name:        "firewall_status",
		Description: "Get firewalld status including whether it is running, version, default zone, and active zones with their ports and services.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.FirewallStatus()
		},
	})

	r.Register(&Tool{
		Name:        "list_firewall_rules",
		Description: "List firewall rules (ports, services, rich rules) for a specific zone. If zone is empty, uses the default zone.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"zone": map[string]interface{}{
					"type":        "string",
					"description": "The firewalld zone name (leave empty for default zone)",
				},
			},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Zone string `json:"zone"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			return r.coreAPI.FirewallListRules(p.Zone)
		},
	})

	r.Register(&Tool{
		Name:        "manage_firewall_rule",
		Description: "Add or remove a firewall rule. Supports port rules (e.g. 8080/tcp), service rules (e.g. http, https), and rich rules. Note: Docker containers using -p port mapping bypass firewalld rules.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"add", "remove"},
					"description": "Whether to add or remove the rule",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"port", "service"},
					"description": "Type of rule: 'port' for port/protocol rules, 'service' for named services",
				},
				"zone": map[string]interface{}{
					"type":        "string",
					"description": "The firewalld zone (leave empty for default zone)",
				},
				"value": map[string]interface{}{
					"type":        "string",
					"description": "The value: port number (e.g. '8080') or port range (e.g. '8080-8090') for port type, or service name (e.g. 'http') for service type",
				},
				"protocol": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"tcp", "udp"},
					"description": "Protocol, required only for port type rules",
				},
			},
			"required": []string{"action", "type", "value"},
		}),
		ReadOnly:          false,
		NeedsConfirmation: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Action   string `json:"action"`
				Type     string `json:"type"`
				Zone     string `json:"zone"`
				Value    string `json:"value"`
				Protocol string `json:"protocol"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}

			switch p.Type {
			case "port":
				if p.Protocol == "" {
					p.Protocol = "tcp"
				}
				if p.Action == "add" {
					if err := r.coreAPI.FirewallAddPort(p.Zone, p.Value, p.Protocol); err != nil {
						return nil, err
					}
				} else {
					if err := r.coreAPI.FirewallRemovePort(p.Zone, p.Value, p.Protocol); err != nil {
						return nil, err
					}
				}
			case "service":
				if p.Action == "add" {
					if err := r.coreAPI.FirewallAddService(p.Zone, p.Value); err != nil {
						return nil, err
					}
				} else {
					if err := r.coreAPI.FirewallRemoveService(p.Zone, p.Value); err != nil {
						return nil, err
					}
				}
			default:
				return nil, fmt.Errorf("unsupported rule type: %s", p.Type)
			}

			return map[string]interface{}{
				"status":  "ok",
				"action":  p.Action,
				"type":    p.Type,
				"value":   p.Value,
				"zone":    p.Zone,
				"message": fmt.Sprintf("Successfully %sed %s rule: %s", p.Action, p.Type, p.Value),
			}, nil
		},
	})

	// ── PHP management tools ──
	registerPHPTools(r)

	// ── NLOps tools (AI direct operations) ──
	registerNLOpsTools(r)

	// ── Cron job tools ──
	registerCronJobTools(r)
}

// registerPHPTools adds PHP management AI tools.
func registerPHPTools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "php_list_runtimes",
		Description: "List all installed PHP runtimes (FPM and FrankenPHP) with their version, status, port, and extensions.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.PHPListRuntimes()
		},
	})

	r.Register(&Tool{
		Name:        "php_list_sites",
		Description: "List all PHP websites with their domain, PHP version, runtime type (FPM/FrankenPHP), and status.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.PHPListSites()
		},
	})
}

// registerNLOpsTools adds AI tools for direct panel operations (NLOps).
func registerNLOpsTools(r *ToolRegistry) {
	// ── Host management ──

	r.Register(&Tool{
		Name:        "delete_host",
		Description: "Delete a reverse proxy site (domain) by its ID. This is irreversible.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The host ID to delete",
				},
			},
			"required": []string{"id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.DeleteHost(p.ID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": fmt.Sprintf("Host %d deleted successfully", p.ID)}, nil
		},
	})

	r.Register(&Tool{
		Name:        "toggle_host",
		Description: "Enable or disable a reverse proxy site. Toggles the current enabled/disabled state.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The host ID to toggle",
				},
			},
			"required": []string{"id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.ToggleHost(p.ID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": fmt.Sprintf("Host %d toggled successfully", p.ID)}, nil
		},
	})

	r.Register(&Tool{
		Name:        "clone_host",
		Description: "Clone an existing reverse proxy site configuration to a new domain.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The source host ID to clone",
				},
				"new_domain": map[string]interface{}{
					"type":        "string",
					"description": "The new domain name for the cloned site",
				},
			},
			"required": []string{"id", "new_domain"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID        uint   `json:"id"`
				NewDomain string `json:"new_domain"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			newID, err := r.coreAPI.CloneHost(p.ID, p.NewDomain)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"new_host_id": newID,
				"domain":      p.NewDomain,
				"message":     fmt.Sprintf("Host cloned successfully. New host ID: %d", newID),
			}, nil
		},
	})

	// ── Caddy management ──

	r.Register(&Tool{
		Name:        "caddy_status",
		Description: "Get the current status of the Caddy web server, including version, running state, and configuration path.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.GetCaddyStatus()
		},
	})

	r.Register(&Tool{
		Name:        "caddy_restart",
		Description: "Restart the Caddy web server. Use this when Caddy is unresponsive or after major configuration changes.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			if err := r.coreAPI.RestartCaddy(); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": "Caddy restarted successfully"}, nil
		},
	})

	r.Register(&Tool{
		Name:        "caddy_reload",
		Description: "Reload Caddy configuration without downtime. Use this after making site changes.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		AdminOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			if err := r.coreAPI.ReloadCaddy(); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": "Caddy configuration reloaded successfully"}, nil
		},
	})

	// ── Deploy lifecycle ──

	r.Register(&Tool{
		Name:        "start_project",
		Description: "Start a stopped deployment project by its ID.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID to start",
				},
			},
			"required": []string{"project_id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID uint `json:"project_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.StartProject(p.ProjectID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": fmt.Sprintf("Project %d start requested", p.ProjectID)}, nil
		},
	})

	r.Register(&Tool{
		Name:        "stop_project",
		Description: "Stop a running deployment project by its ID.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID to stop",
				},
			},
			"required": []string{"project_id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID uint `json:"project_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.StopProject(p.ProjectID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": fmt.Sprintf("Project %d stop requested", p.ProjectID)}, nil
		},
	})

	r.Register(&Tool{
		Name:        "rollback_project",
		Description: "Rollback a deployment project to a specific build number. Check deployment history first to find the correct build number.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "number",
					"description": "The project ID",
				},
				"build_number": map[string]interface{}{
					"type":        "number",
					"description": "The build number to rollback to",
				},
			},
			"required": []string{"project_id", "build_number"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ProjectID   uint `json:"project_id"`
				BuildNumber int  `json:"build_number"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.RollbackProject(p.ProjectID, p.BuildNumber); err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"message": fmt.Sprintf("Project %d rollback to build #%d requested", p.ProjectID, p.BuildNumber),
			}, nil
		},
	})

	// ── Docker cleanup ──

	r.Register(&Tool{
		Name:        "docker_remove_container",
		Description: "Remove a Docker container by its ID or name. Use force=true to remove a running container.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"container_id": map[string]interface{}{
					"type":        "string",
					"description": "Container ID or name",
				},
				"force": map[string]interface{}{
					"type":        "boolean",
					"description": "Force remove even if running (default: false)",
				},
			},
			"required": []string{"container_id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ContainerID string `json:"container_id"`
				Force       bool   `json:"force"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.DockerRemoveContainer(p.ContainerID, p.Force); err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"message": fmt.Sprintf("Container %s removed successfully", p.ContainerID),
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "docker_prune",
		Description: "Clean up unused Docker resources. Specify what to prune: containers, images, volumes, or all.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"what": map[string]interface{}{
					"type":        "string",
					"description": "What to prune: containers, images, volumes, or all",
					"enum":        []string{"containers", "images", "volumes", "all"},
				},
			},
			"required": []string{"what"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				What string `json:"what"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			result, err := r.coreAPI.DockerPrune(p.What)
			if err != nil {
				return nil, err
			}
			return result, nil
		},
	})

	// ── Notification channels ──

	r.Register(&Tool{
		Name:        "list_notify_channels",
		Description: "List all configured notification channels (Webhook, Email, Discord, Telegram) and their status.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.ListNotifyChannels()
		},
	})

	r.Register(&Tool{
		Name:        "test_notify_channel",
		Description: "Send a test message to a notification channel to verify it works.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The notification channel ID to test",
				},
			},
			"required": []string{"id"},
		}),
		AdminOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.TestNotifyChannel(p.ID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": "Test notification sent successfully"}, nil
		},
	})

	// ── Monitoring alert rules ──

	r.Register(&Tool{
		Name:        "list_alert_rules",
		Description: "List all monitoring alert rules with their configuration and current status.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.ListAlertRules()
		},
	})

	r.Register(&Tool{
		Name:        "create_alert_rule",
		Description: "Create a new monitoring alert rule. Example: alert when cpu_percent > 90 for 5 minutes.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the alert rule",
				},
				"metric": map[string]interface{}{
					"type":        "string",
					"description": "Metric to monitor: cpu_percent, memory_percent, disk_percent, load1",
				},
				"operator": map[string]interface{}{
					"type":        "string",
					"description": "Comparison operator: >, <, >=, <=, ==",
				},
				"threshold": map[string]interface{}{
					"type":        "number",
					"description": "Threshold value",
				},
				"duration": map[string]interface{}{
					"type":        "number",
					"description": "Duration in minutes before alerting",
				},
			},
			"required": []string{"name", "metric", "operator", "threshold", "duration"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Name      string  `json:"name"`
				Metric    string  `json:"metric"`
				Operator  string  `json:"operator"`
				Threshold float64 `json:"threshold"`
				Duration  int     `json:"duration"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			id, err := r.coreAPI.CreateAlertRule(p.Name, p.Metric, p.Operator, p.Threshold, p.Duration)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"id":      id,
				"message": fmt.Sprintf("Alert rule '%s' created (ID: %d)", p.Name, id),
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "delete_alert_rule",
		Description: "Delete a monitoring alert rule by its ID.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "number",
					"description": "The alert rule ID to delete",
				},
			},
			"required": []string{"id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.DeleteAlertRule(p.ID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": fmt.Sprintf("Alert rule %d deleted", p.ID)}, nil
		},
	})

	// ── System information ──

	r.Register(&Tool{
		Name:        "get_system_info",
		Description: "Get detailed system information including hostname, OS, kernel version, uptime, CPU cores, and architecture.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return r.coreAPI.GetSystemInfo()
		},
	})

	// ── One-sentence deployment ──

	r.Register(&Tool{
		Name:        "auto_deploy",
		Description: "One-command deployment: given a Git URL and optional domain, automatically create a project, detect framework, trigger build, and set up reverse proxy with HTTPS. The build runs asynchronously — use get_project to check status afterward.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"git_url": map[string]interface{}{
					"type":        "string",
					"description": "Git repository URL (e.g. https://github.com/user/repo)",
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Domain to deploy to (e.g. app.example.com). If empty, no reverse proxy is created.",
				},
				"branch": map[string]interface{}{
					"type":        "string",
					"description": "Git branch (default: main)",
				},
				"deploy_mode": map[string]interface{}{
					"type":        "string",
					"description": "Deployment mode: bare or docker (default: docker)",
					"enum":        []string{"bare", "docker"},
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Project name. If empty, derived from the repository name.",
				},
			},
			"required": []string{"git_url"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				GitURL     string `json:"git_url"`
				Domain     string `json:"domain"`
				Branch     string `json:"branch"`
				DeployMode string `json:"deploy_mode"`
				Name       string `json:"name"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}

			// Defaults
			if p.Branch == "" {
				p.Branch = "main"
			}
			if p.DeployMode == "" {
				p.DeployMode = "docker"
			}
			if p.Name == "" {
				// Derive name from git URL: https://github.com/user/repo.git → repo
				parts := strings.Split(strings.TrimSuffix(p.GitURL, ".git"), "/")
				if len(parts) > 0 {
					p.Name = parts[len(parts)-1]
				}
				if p.Name == "" {
					p.Name = "auto-project"
				}
			}

			// Step 1: Create project
			projectID, err := r.coreAPI.CreateProject(pluginpkg.CreateProjectRequest{
				Name:       p.Name,
				GitURL:     p.GitURL,
				GitBranch:  p.Branch,
				Domain:     p.Domain,
				DeployMode: p.DeployMode,
				AutoDeploy: true,
			})
			if err != nil {
				return nil, fmt.Errorf("create project: %w", err)
			}

			// Step 2: Trigger build
			if err := r.coreAPI.TriggerBuild(projectID); err != nil {
				return map[string]interface{}{
					"project_id": projectID,
					"status":     "created_but_build_failed_to_start",
					"error":      err.Error(),
					"message":    fmt.Sprintf("Project created (ID: %d) but build trigger failed: %v. Try trigger_build manually.", projectID, err),
				}, nil
			}

			return map[string]interface{}{
				"project_id":  projectID,
				"name":        p.Name,
				"domain":      p.Domain,
				"deploy_mode": p.DeployMode,
				"status":      "building",
				"message":     fmt.Sprintf("Project '%s' created (ID: %d) and build started. Use get_project to check build status.", p.Name, projectID),
			}, nil
		},
	})

	// ── Inspection (placeholder — connected in Phase 3) ──

	r.Register(&Tool{
		Name:        "run_inspection",
		Description: "Run a system health inspection that checks disk, memory, containers, SSL certificates, and generates an AI-powered summary report.",
		Parameters: jsonSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		ReadOnly:  true,
		AdminOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			if r.inspection == nil {
				return nil, fmt.Errorf("inspection service not configured — enable daily inspection in AI settings")
			}
			return r.inspection.RunInspection()
		},
	})
}

// registerCronJobTools adds AI tools for cron job management.
func registerCronJobTools(r *ToolRegistry) {
	r.Register(&Tool{
		Name:        "list_cron_jobs",
		Description: "List all scheduled cron jobs. Optionally filter by tag (e.g. 'backup', 'cleanup', 'deploy').",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tag": map[string]interface{}{
					"type":        "string",
					"description": "Optional tag to filter jobs (e.g. 'backup')",
				},
			},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Tag string `json:"tag"`
			}
			_ = json.Unmarshal(args, &p)
			return r.coreAPI.CronJobList(p.Tag)
		},
	})

	r.Register(&Tool{
		Name:        "get_cron_job_logs",
		Description: "Get execution logs for a cron job. If task_id is 0, returns recent logs across all jobs.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "integer",
					"description": "Cron job ID (0 for all jobs)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Max number of log entries to return (default 20)",
				},
			},
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				TaskID uint `json:"task_id"`
				Limit  int  `json:"limit"`
			}
			_ = json.Unmarshal(args, &p)
			if p.Limit <= 0 {
				p.Limit = 20
			}
			return r.coreAPI.CronJobLogs(p.TaskID, p.Limit)
		},
	})

	r.Register(&Tool{
		Name:        "create_cron_job",
		Description: "Create a new scheduled cron job. Expression uses standard 5-field cron format (minute hour day month weekday). Examples: '*/5 * * * *' (every 5 min), '0 3 * * *' (daily at 3am), '0 0 * * 0' (weekly).",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable name for the job",
				},
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "Standard 5-field cron expression (minute hour day month weekday)",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to execute",
				},
				"working_dir": map[string]interface{}{
					"type":        "string",
					"description": "Working directory for the command (optional)",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Tags for categorisation (e.g. ['backup', 'cleanup'])",
				},
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"description": "Execution timeout in seconds (default 300)",
				},
			},
			"required": []string{"name", "expression", "command"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Name       string   `json:"name"`
				Expression string   `json:"expression"`
				Command    string   `json:"command"`
				WorkingDir string   `json:"working_dir"`
				Tags       []string `json:"tags"`
				TimeoutSec int      `json:"timeout_sec"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			id, err := r.coreAPI.CronJobCreate(p.Name, p.Expression, p.Command, p.WorkingDir, p.Tags, p.TimeoutSec)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"id":      id,
				"message": fmt.Sprintf("Cron job '%s' created (ID: %d)", p.Name, id),
			}, nil
		},
	})

	r.Register(&Tool{
		Name:        "update_cron_job",
		Description: "Update an existing cron job's settings (name, expression, command, enabled status, etc.).",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "integer",
					"description": "Cron job ID to update",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "New name (optional)",
				},
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "New cron expression (optional)",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "New command (optional)",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable or disable the job (optional)",
				},
			},
			"required": []string{"id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID         uint    `json:"id"`
				Name       *string `json:"name"`
				Expression *string `json:"expression"`
				Command    *string `json:"command"`
				Enabled    *bool   `json:"enabled"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			updates := make(map[string]interface{})
			if p.Name != nil {
				updates["name"] = *p.Name
			}
			if p.Expression != nil {
				updates["expression"] = *p.Expression
			}
			if p.Command != nil {
				updates["command"] = *p.Command
			}
			if p.Enabled != nil {
				updates["enabled"] = *p.Enabled
			}
			if err := r.coreAPI.CronJobUpdate(p.ID, updates); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": fmt.Sprintf("Cron job %d updated", p.ID)}, nil
		},
	})

	r.Register(&Tool{
		Name:        "delete_cron_job",
		Description: "Delete a cron job and all its execution logs.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "integer",
					"description": "Cron job ID to delete",
				},
			},
			"required": []string{"id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.CronJobDelete(p.ID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": fmt.Sprintf("Cron job %d deleted", p.ID)}, nil
		},
	})

	r.Register(&Tool{
		Name:        "trigger_cron_job",
		Description: "Manually trigger a cron job to run immediately, regardless of its schedule.",
		Parameters: jsonSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "integer",
					"description": "Cron job ID to trigger",
				},
			},
			"required": []string{"id"},
		}),
		NeedsConfirmation: true,
		AdminOnly:         true,
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				ID uint `json:"id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if err := r.coreAPI.CronJobTrigger(p.ID); err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": fmt.Sprintf("Cron job %d triggered", p.ID)}, nil
		},
	})
}

// jsonSchema is a helper that returns the map as-is (for readability in tool definitions).
func jsonSchema(schema map[string]interface{}) map[string]interface{} {
	return schema
}
