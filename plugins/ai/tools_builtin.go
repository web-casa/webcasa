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
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			if err := r.coreAPI.TriggerBackup(); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "backup_triggered"}, nil
		},
	})

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
		Handler:  updateHostHandler(r),
	})
}

// ──────────────────────────────────────────────
// Tool handler implementations
// ──────────────────────────────────────────────

// isPathSafe checks if a file path is within allowed directories.
func isPathSafe(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	// Block sensitive files
	blocked := []string{"/etc/shadow", "/etc/gshadow", "/etc/sudoers"}
	for _, b := range blocked {
		if abs == b {
			return false
		}
	}
	// Allow common safe directories
	allowed := []string{
		"/etc/caddy", "/etc/nginx",
		"/var/log",
		"/home", "/root",
		"/opt", "/srv",
		"/tmp",
	}
	for _, a := range allowed {
		if strings.HasPrefix(abs, a) {
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

// jsonSchema is a helper that returns the map as-is (for readability in tool definitions).
func jsonSchema(schema map[string]interface{}) map[string]interface{} {
	return schema
}
