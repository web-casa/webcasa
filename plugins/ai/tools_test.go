package ai

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// stubCoreAPI implements CoreAPI for testing AI tools.
type stubCoreAPI struct{}

func (s *stubCoreAPI) CreateHost(req pluginpkg.CreateHostRequest) (uint, error) { return 1, nil }
func (s *stubCoreAPI) DeleteHost(id uint) error                                 { return nil }
func (s *stubCoreAPI) ListHosts() ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{"id": uint(1), "domain": "example.com", "enabled": true},
	}, nil
}
func (s *stubCoreAPI) GetHost(id uint) (map[string]interface{}, error) {
	return map[string]interface{}{"id": id, "domain": "example.com"}, nil
}
func (s *stubCoreAPI) UpdateHostUpstream(hostID uint, newUpstream string) error { return nil }
func (s *stubCoreAPI) ReloadCaddy() error                                       { return nil }
func (s *stubCoreAPI) GetSetting(key string) (string, error)                    { return "", nil }
func (s *stubCoreAPI) SetSetting(key, value string) error                       { return nil }
func (s *stubCoreAPI) GetDB() *gorm.DB                                          { return nil }
func (s *stubCoreAPI) ListProjects() ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{"id": float64(1), "name": "test-project", "status": "running"},
	}, nil
}
func (s *stubCoreAPI) GetProject(id uint) (map[string]interface{}, error) {
	return map[string]interface{}{"id": float64(id), "name": "test-project"}, nil
}
func (s *stubCoreAPI) GetBuildLog(projectID uint, buildNum int) (string, error) {
	return "build log content", nil
}
func (s *stubCoreAPI) GetRuntimeLog(projectID uint, lines int) (string, error) {
	return "runtime log content", nil
}
func (s *stubCoreAPI) TriggerBuild(projectID uint) error                       { return nil }
func (s *stubCoreAPI) CreateProject(req pluginpkg.CreateProjectRequest) (uint, error) {
	return 1, nil
}
func (s *stubCoreAPI) GetEnvSuggestions(framework string) ([]map[string]interface{}, error) {
	if framework == "nextjs" {
		return []map[string]interface{}{
			{"key": "NODE_ENV", "default_value": "production", "description": "Node.js environment mode", "required": true},
		}, nil
	}
	return []map[string]interface{}{}, nil
}
func (s *stubCoreAPI) DockerPS() ([]map[string]interface{}, error)             { return nil, nil }
func (s *stubCoreAPI) DockerLogs(containerID string, tail int) (string, error) { return "", nil }
func (s *stubCoreAPI) GetMetrics() (map[string]interface{}, error) {
	return map[string]interface{}{"num_cpu": 4}, nil
}
func (s *stubCoreAPI) RunCommand(cmd string, timeoutSec int) (string, error) {
	return "command output", nil
}
func (s *stubCoreAPI) TriggerBackup() error                                    { return nil }
func (s *stubCoreAPI) UpdateHost(id uint, req pluginpkg.UpdateHostRequest) error { return nil }
func (s *stubCoreAPI) GetRecentAlerts() ([]map[string]interface{}, error)      { return nil, nil }
func (s *stubCoreAPI) DatabaseListInstances() ([]map[string]interface{}, error) { return nil, nil }
func (s *stubCoreAPI) DatabaseCreateInstance(req pluginpkg.DatabaseCreateInstanceRequest) (uint, error) {
	return 0, nil
}
func (s *stubCoreAPI) DatabaseCreateDatabase(instanceID uint, name, charset string) error { return nil }
func (s *stubCoreAPI) DatabaseCreateUser(instanceID uint, username, password string, databases []string) error {
	return nil
}
func (s *stubCoreAPI) DatabaseExecuteQuery(instanceID uint, database, query string) (map[string]interface{}, error) {
	return nil, nil
}
func (s *stubCoreAPI) DockerListStacks() ([]map[string]interface{}, error)                  { return nil, nil }
func (s *stubCoreAPI) DockerManageContainer(containerID, action string) error               { return nil }
func (s *stubCoreAPI) DockerRunContainer(req pluginpkg.DockerRunContainerRequest) (string, error) {
	return "", nil
}
func (s *stubCoreAPI) DockerPullImage(image string) error { return nil }
func (s *stubCoreAPI) DockerGetContainerStats(containerID string) (map[string]interface{}, error) {
	return nil, nil
}
func (s *stubCoreAPI) AppStoreSearchApps(query string) ([]map[string]interface{}, error) { return nil, nil }
func (s *stubCoreAPI) AppStoreInstallApp(appID string, config map[string]interface{}) (uint, error) {
	return 0, nil
}
func (s *stubCoreAPI) AppStoreListInstalled() ([]map[string]interface{}, error) { return nil, nil }
func (s *stubCoreAPI) FileWrite(path, content string) error                     { return nil }
func (s *stubCoreAPI) FileDelete(path string) error                             { return nil }
func (s *stubCoreAPI) FileRename(oldPath, newPath string) error                 { return nil }
func (s *stubCoreAPI) FirewallStatus() (map[string]interface{}, error)          { return nil, nil }
func (s *stubCoreAPI) FirewallListRules(zone string) (map[string]interface{}, error) { return nil, nil }
func (s *stubCoreAPI) FirewallAddPort(zone, port, protocol string) error        { return nil }
func (s *stubCoreAPI) FirewallRemovePort(zone, port, protocol string) error     { return nil }
func (s *stubCoreAPI) FirewallAddService(zone, service string) error            { return nil }
func (s *stubCoreAPI) FirewallRemoveService(zone, service string) error         { return nil }
func (s *stubCoreAPI) PHPListRuntimes() ([]map[string]interface{}, error)       { return nil, nil }
func (s *stubCoreAPI) PHPListSites() ([]map[string]interface{}, error)          { return nil, nil }

// NLOps stubs
func (s *stubCoreAPI) ToggleHost(id uint) error                                          { return nil }
func (s *stubCoreAPI) CloneHost(id uint, newDomain string) (uint, error)                 { return 1, nil }
func (s *stubCoreAPI) GetCaddyStatus() (map[string]interface{}, error)                   { return nil, nil }
func (s *stubCoreAPI) RestartCaddy() error                                               { return nil }
func (s *stubCoreAPI) StartProject(id uint) error                                        { return nil }
func (s *stubCoreAPI) StopProject(id uint) error                                         { return nil }
func (s *stubCoreAPI) RollbackProject(projectID uint, buildNum int) error                { return nil }
func (s *stubCoreAPI) DockerRemoveContainer(containerID string, force bool) error        { return nil }
func (s *stubCoreAPI) DockerPrune(what string) (map[string]interface{}, error)           { return nil, nil }
func (s *stubCoreAPI) ListNotifyChannels() ([]map[string]interface{}, error)             { return nil, nil }
func (s *stubCoreAPI) TestNotifyChannel(id uint) error                                   { return nil }
func (s *stubCoreAPI) ListAlertRules() ([]map[string]interface{}, error)                 { return nil, nil }
func (s *stubCoreAPI) CreateAlertRule(name, metric, operator string, threshold float64, duration int) (uint, error) {
	return 1, nil
}
func (s *stubCoreAPI) DeleteAlertRule(id uint) error                                     { return nil }
func (s *stubCoreAPI) GetSystemInfo() (map[string]interface{}, error)                    { return nil, nil }
func (s *stubCoreAPI) CronJobList(tag string) ([]map[string]interface{}, error)         { return nil, nil }
func (s *stubCoreAPI) CronJobCreate(name, expression, command, workingDir string, tags []string, timeoutSec int) (uint, error) {
	return 0, nil
}
func (s *stubCoreAPI) CronJobUpdate(id uint, updates map[string]interface{}) error       { return nil }
func (s *stubCoreAPI) CronJobDelete(id uint) error                                       { return nil }
func (s *stubCoreAPI) CronJobLogs(taskID uint, limit int) ([]map[string]interface{}, error) { return nil, nil }
func (s *stubCoreAPI) CronJobTrigger(id uint) error                                      { return nil }
func (s *stubCoreAPI) EncryptSecret(plaintext string) (string, error)                    { return plaintext, nil }
func (s *stubCoreAPI) DecryptSecret(ciphertext string) (string, error)                   { return ciphertext, nil }

func newTestRegistry() *ToolRegistry {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := NewToolRegistry(&stubCoreAPI{}, logger)
	RegisterBuiltinTools(r)
	return r
}

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := newTestRegistry()

	tool := r.Get("list_hosts")
	if tool == nil {
		t.Fatal("expected list_hosts tool to be registered")
	}
	if tool.Name != "list_hosts" {
		t.Fatalf("expected name list_hosts, got %s", tool.Name)
	}
}

func TestToolRegistry_UnknownTool(t *testing.T) {
	r := newTestRegistry()

	tool := r.Get("nonexistent_tool")
	if tool != nil {
		t.Fatal("expected nil for unknown tool")
	}
}

func TestToolRegistry_AllTools(t *testing.T) {
	r := newTestRegistry()

	tools := r.All()
	if len(tools) < 10 {
		t.Fatalf("expected at least 10 builtin tools, got %d", len(tools))
	}
}

func TestToolRegistry_Execute_ListHosts(t *testing.T) {
	r := newTestRegistry()

	result, err := r.Execute(context.Background(), "list_hosts", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("Execute list_hosts failed: %v", err)
	}
	hosts, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
}

func TestToolRegistry_Execute_GetHost(t *testing.T) {
	r := newTestRegistry()

	args := json.RawMessage(`{"id": 1}`)
	result, err := r.Execute(context.Background(), "get_host", args)
	if err != nil {
		t.Fatalf("Execute get_host failed: %v", err)
	}
	host, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}
	if host["domain"] != "example.com" {
		t.Fatalf("expected domain example.com, got %v", host["domain"])
	}
}

func TestToolRegistry_Execute_ListProjects(t *testing.T) {
	r := newTestRegistry()

	result, err := r.Execute(context.Background(), "list_projects", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("Execute list_projects failed: %v", err)
	}
	projects, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
}

func TestToolRegistry_Execute_GetBuildLog(t *testing.T) {
	r := newTestRegistry()

	args := json.RawMessage(`{"project_id": 1, "build_num": 1}`)
	result, err := r.Execute(context.Background(), "get_build_log", args)
	if err != nil {
		t.Fatalf("Execute get_build_log failed: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["log"] != "build log content" {
		t.Fatalf("unexpected log content: %v", m["log"])
	}
}

func TestToolRegistry_Execute_GetSystemMetrics(t *testing.T) {
	r := newTestRegistry()

	result, err := r.Execute(context.Background(), "get_system_metrics", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("Execute get_system_metrics failed: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["num_cpu"] != 4 {
		t.Fatalf("expected num_cpu=4, got %v", m["num_cpu"])
	}
}

func TestToolRegistry_Execute_UnknownTool(t *testing.T) {
	r := newTestRegistry()

	_, err := r.Execute(context.Background(), "nonexistent", json.RawMessage("{}"))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestToolRegistry_Execute_DeployProject(t *testing.T) {
	r := newTestRegistry()

	args := json.RawMessage(`{"project_id": 1}`)
	result, err := r.Execute(context.Background(), "deploy_project", args)
	if err != nil {
		t.Fatalf("Execute deploy_project failed: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["status"] != "build_triggered" {
		t.Fatalf("expected build_triggered, got %v", m["status"])
	}
}

func TestToolRegistry_OpenAISchema(t *testing.T) {
	r := newTestRegistry()

	schema := r.OpenAIToolSchema()
	if len(schema) < 10 {
		t.Fatalf("expected at least 10 tools in schema, got %d", len(schema))
	}

	// Verify schema structure
	for _, tool := range schema {
		if tool["type"] != "function" {
			t.Fatalf("expected type=function, got %v", tool["type"])
		}
		fn, ok := tool["function"].(map[string]interface{})
		if !ok {
			t.Fatal("expected function field to be a map")
		}
		if fn["name"] == nil || fn["name"] == "" {
			t.Fatal("expected non-empty tool name")
		}
	}
}

func TestToolRegistry_AnthropicSchema(t *testing.T) {
	r := newTestRegistry()

	schema := r.AnthropicToolSchema()
	if len(schema) < 10 {
		t.Fatalf("expected at least 10 tools in schema, got %d", len(schema))
	}

	// Verify Anthropic schema structure
	for _, tool := range schema {
		if tool["name"] == nil || tool["name"] == "" {
			t.Fatal("expected non-empty tool name")
		}
		if tool["input_schema"] == nil {
			t.Fatalf("expected input_schema for tool %v", tool["name"])
		}
	}
}

func TestToolRegistry_ReadOnlyFlag(t *testing.T) {
	r := newTestRegistry()

	readOnlyTools := []string{"list_hosts", "get_host", "list_projects", "get_project", "get_build_log", "get_runtime_log", "docker_ps", "docker_logs", "get_system_metrics"}
	for _, name := range readOnlyTools {
		tool := r.Get(name)
		if tool == nil {
			t.Fatalf("tool %s not found", name)
		}
		if !tool.ReadOnly {
			t.Errorf("expected %s to be read-only", name)
		}
	}

	writeTools := []string{"create_host", "deploy_project", "run_command"}
	for _, name := range writeTools {
		tool := r.Get(name)
		if tool == nil {
			t.Fatalf("tool %s not found", name)
		}
		if tool.ReadOnly {
			t.Errorf("expected %s to be writable", name)
		}
	}
}
