package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/web-casa/webcasa/internal/model"
	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ── richStubCoreAPI embeds stubCoreAPI with meaningful returns ──

type richStubCoreAPI struct {
	stubCoreAPI
	db              *gorm.DB
	restartCalled   int32
	reloadCalled    int32
	metricsOverride map[string]interface{}
}

func (r *richStubCoreAPI) GetDB() *gorm.DB { return r.db }

func (r *richStubCoreAPI) GetCaddyStatus() (map[string]interface{}, error) {
	return map[string]interface{}{"running": true, "version": "2.7.0"}, nil
}

func (r *richStubCoreAPI) GetSystemInfo() (map[string]interface{}, error) {
	return map[string]interface{}{
		"hostname": "test-server",
		"kernel":   "6.1.0",
		"os":       "AlmaLinux 10",
	}, nil
}

func (r *richStubCoreAPI) ListNotifyChannels() ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{"id": uint(1), "type": "webhook", "name": "test-channel", "enabled": true},
	}, nil
}

func (r *richStubCoreAPI) ListAlertRules() ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{"id": uint(1), "name": "High CPU", "metric": "cpu_percent", "threshold": float64(90)},
	}, nil
}

func (r *richStubCoreAPI) GetMetrics() (map[string]interface{}, error) {
	if r.metricsOverride != nil {
		return r.metricsOverride, nil
	}
	return map[string]interface{}{
		"disk_total":      "100000000000",
		"disk_used":       "92000000000",
		"disk_available":  "8000000000",
		"mem_total_kb":    "8000000",
		"mem_available_kb": "500000",
		"load_1":          "6.5",
		"num_cpu":         4,
	}, nil
}

func (r *richStubCoreAPI) DockerPS() ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{"name": "web", "state": "running"},
		{"name": "stuck", "state": "restarting"},
	}, nil
}

func (r *richStubCoreAPI) GetRecentAlerts() ([]map[string]interface{}, error) {
	alerts := make([]map[string]interface{}, 6)
	for i := range alerts {
		alerts[i] = map[string]interface{}{"rule_name": fmt.Sprintf("rule-%d", i), "message": "triggered"}
	}
	return alerts, nil
}

func (r *richStubCoreAPI) RestartCaddy() error {
	atomic.AddInt32(&r.restartCalled, 1)
	return nil
}

func (r *richStubCoreAPI) ReloadCaddy() error {
	atomic.AddInt32(&r.reloadCalled, 1)
	return nil
}

func (r *richStubCoreAPI) ListHosts() ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{"id": uint(1), "domain": "example.com", "enabled": true},
	}, nil
}

// ── test helpers ──

func setupFeatureTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })

	db.AutoMigrate(&model.Setting{}, &InspectionRecord{})
	db.Exec(`CREATE TABLE IF NOT EXISTS plugin_monitoring_alert_rules (
		id INTEGER PRIMARY KEY, name TEXT, last_diagnosis TEXT, last_heal_at DATETIME,
		auto_heal_mode TEXT DEFAULT 'notify'
	)`)
	return db
}

func featureTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newRichTestRegistry(t *testing.T) (*ToolRegistry, *richStubCoreAPI) {
	t.Helper()
	db := setupFeatureTestDB(t)
	api := &richStubCoreAPI{db: db}
	lg := featureTestLogger()
	r := NewToolRegistry(api, lg)
	RegisterBuiltinTools(r)
	return r, api
}

// ============================================================================
// Section A: NLOps Tool Registration and Execution
// ============================================================================

var nlOpsToolNames = []string{
	"delete_host", "toggle_host", "clone_host",
	"caddy_status", "caddy_restart", "caddy_reload",
	"start_project", "stop_project", "rollback_project",
	"docker_remove_container", "docker_prune",
	"list_notify_channels", "test_notify_channel",
	"list_alert_rules", "create_alert_rule", "delete_alert_rule",
	"get_system_info",
	"auto_deploy",
	"run_inspection",
}

func TestNLOpsTools_AllRegistered(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	for _, name := range nlOpsToolNames {
		if r.Get(name) == nil {
			t.Errorf("NLOps tool %q not registered", name)
		}
	}
}

func TestNLOpsTools_AdminOnlyFlags(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	adminTools := []string{
		"delete_host", "toggle_host", "clone_host",
		"caddy_restart", "caddy_reload",
		"start_project", "stop_project", "rollback_project",
		"docker_remove_container", "docker_prune",
		"test_notify_channel",
		"create_alert_rule", "delete_alert_rule",
		"auto_deploy", "run_inspection",
	}
	for _, name := range adminTools {
		tool := r.Get(name)
		if tool == nil {
			t.Fatalf("tool %q not found", name)
		}
		if !tool.AdminOnly {
			t.Errorf("expected %q to be AdminOnly", name)
		}
	}
}

func TestNLOpsTools_NeedsConfirmationFlags(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	confirmTools := map[string]bool{
		"delete_host": true, "toggle_host": true, "clone_host": true,
		"caddy_restart": true, "caddy_reload": false,
		"start_project": true, "stop_project": true, "rollback_project": true,
		"docker_remove_container": true, "docker_prune": true,
		"create_alert_rule": true, "delete_alert_rule": true,
		"auto_deploy": true, "run_inspection": false,
	}
	for name, expected := range confirmTools {
		tool := r.Get(name)
		if tool == nil {
			t.Fatalf("tool %q not found", name)
		}
		if tool.NeedsConfirmation != expected {
			t.Errorf("tool %q: NeedsConfirmation = %v, want %v", name, tool.NeedsConfirmation, expected)
		}
	}
}

func TestNLOpsTools_ReadOnlyFlags(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	readOnlyTools := []string{"caddy_status", "list_notify_channels", "list_alert_rules", "get_system_info", "run_inspection"}
	for _, name := range readOnlyTools {
		tool := r.Get(name)
		if tool == nil {
			t.Fatalf("tool %q not found", name)
		}
		if !tool.ReadOnly {
			t.Errorf("expected %q to be ReadOnly", name)
		}
	}
}

func TestNLOpsTools_Execute_DeleteHost(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "delete_host", json.RawMessage(`{"id": 1}`))
	if err != nil {
		t.Fatalf("Execute delete_host: %v", err)
	}
	m := result.(map[string]interface{})
	if msg, _ := m["message"].(string); msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestNLOpsTools_Execute_ToggleHost(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "toggle_host", json.RawMessage(`{"id": 1}`))
	if err != nil {
		t.Fatalf("Execute toggle_host: %v", err)
	}
	m := result.(map[string]interface{})
	if msg, _ := m["message"].(string); msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestNLOpsTools_Execute_CloneHost(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "clone_host", json.RawMessage(`{"id": 1, "new_domain": "new.example.com"}`))
	if err != nil {
		t.Fatalf("Execute clone_host: %v", err)
	}
	m := result.(map[string]interface{})
	if m["new_host_id"] == nil {
		t.Fatal("expected new_host_id in result")
	}
	if m["domain"] != "new.example.com" {
		t.Fatalf("expected domain new.example.com, got %v", m["domain"])
	}
}

func TestNLOpsTools_Execute_CaddyStatus(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "caddy_status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute caddy_status: %v", err)
	}
	m := result.(map[string]interface{})
	if m["running"] != true {
		t.Fatalf("expected running=true, got %v", m["running"])
	}
}

func TestNLOpsTools_Execute_CaddyRestart(t *testing.T) {
	r, api := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "caddy_restart", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute caddy_restart: %v", err)
	}
	m := result.(map[string]interface{})
	if m["message"] == nil {
		t.Fatal("expected message")
	}
	if atomic.LoadInt32(&api.restartCalled) != 1 {
		t.Fatal("RestartCaddy was not called")
	}
}

func TestNLOpsTools_Execute_CaddyReload(t *testing.T) {
	r, api := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "caddy_reload", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute caddy_reload: %v", err)
	}
	m := result.(map[string]interface{})
	if m["message"] == nil {
		t.Fatal("expected message")
	}
	if atomic.LoadInt32(&api.reloadCalled) != 1 {
		t.Fatal("ReloadCaddy was not called")
	}
}

func TestNLOpsTools_Execute_StartProject(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "start_project", json.RawMessage(`{"project_id": 1}`))
	if err != nil {
		t.Fatalf("Execute start_project: %v", err)
	}
	m := result.(map[string]interface{})
	if msg, _ := m["message"].(string); msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestNLOpsTools_Execute_StopProject(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "stop_project", json.RawMessage(`{"project_id": 1}`))
	if err != nil {
		t.Fatalf("Execute stop_project: %v", err)
	}
	m := result.(map[string]interface{})
	if msg, _ := m["message"].(string); msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestNLOpsTools_Execute_RollbackProject(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "rollback_project", json.RawMessage(`{"project_id": 1, "build_number": 3}`))
	if err != nil {
		t.Fatalf("Execute rollback_project: %v", err)
	}
	m := result.(map[string]interface{})
	if msg, _ := m["message"].(string); msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestNLOpsTools_Execute_DockerRemoveContainer(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "docker_remove_container", json.RawMessage(`{"container_id": "abc123"}`))
	if err != nil {
		t.Fatalf("Execute docker_remove_container: %v", err)
	}
	m := result.(map[string]interface{})
	if msg, _ := m["message"].(string); msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestNLOpsTools_Execute_DockerPrune(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	_, err := r.Execute(context.Background(), "docker_prune", json.RawMessage(`{"what": "all"}`))
	if err != nil {
		t.Fatalf("Execute docker_prune: %v", err)
	}
}

func TestNLOpsTools_Execute_ListNotifyChannels(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "list_notify_channels", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	channels, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map, got %T", result)
	}
	if len(channels) < 1 {
		t.Fatal("expected at least 1 channel")
	}
}

func TestNLOpsTools_Execute_TestNotifyChannel(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "test_notify_channel", json.RawMessage(`{"id": 1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := result.(map[string]interface{})
	if m["message"] == nil {
		t.Fatal("expected message")
	}
}

func TestNLOpsTools_Execute_ListAlertRules(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "list_alert_rules", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rules, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map, got %T", result)
	}
	if len(rules) < 1 {
		t.Fatal("expected at least 1 rule")
	}
}

func TestNLOpsTools_Execute_CreateAlertRule(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "create_alert_rule",
		json.RawMessage(`{"name":"test","metric":"cpu_percent","operator":">","threshold":90,"duration":5}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := result.(map[string]interface{})
	if m["id"] == nil {
		t.Fatal("expected id in result")
	}
}

func TestNLOpsTools_Execute_DeleteAlertRule(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "delete_alert_rule", json.RawMessage(`{"id": 1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := result.(map[string]interface{})
	if m["message"] == nil {
		t.Fatal("expected message")
	}
}

func TestNLOpsTools_Execute_GetSystemInfo(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "get_system_info", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := result.(map[string]interface{})
	if m["hostname"] != "test-server" {
		t.Fatalf("expected hostname=test-server, got %v", m["hostname"])
	}
}

func TestNLOpsTools_Execute_AutoDeploy(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	result, err := r.Execute(context.Background(), "auto_deploy",
		json.RawMessage(`{"git_url": "https://github.com/user/myapp.git", "domain": "app.example.com"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := result.(map[string]interface{})
	if m["name"] != "myapp" {
		t.Fatalf("expected name=myapp, got %v", m["name"])
	}
	if m["status"] != "building" {
		t.Fatalf("expected status=building, got %v", m["status"])
	}
}

func TestNLOpsTools_Execute_AutoDeploy_NameDerivation(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/user/my-app.git", "my-app"},
		{"https://github.com/user/repo", "repo"},
		{"https://github.com/org/project.git", "project"},
	}
	for _, tc := range tests {
		result, err := r.Execute(context.Background(), "auto_deploy",
			json.RawMessage(fmt.Sprintf(`{"git_url": %q}`, tc.url)))
		if err != nil {
			t.Fatalf("Execute auto_deploy(%s): %v", tc.url, err)
		}
		m := result.(map[string]interface{})
		if m["name"] != tc.want {
			t.Errorf("auto_deploy(%s): name = %v, want %v", tc.url, m["name"], tc.want)
		}
	}
}

func TestNLOpsTools_Execute_RunInspection_Nil(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	// inspection is nil by default
	_, err := r.Execute(context.Background(), "run_inspection", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error when inspection is nil")
	}
}

// ============================================================================
// Section B: InspectionService
// ============================================================================

func newTestInspectionService(t *testing.T, api pluginpkg.CoreAPI) (*InspectionService, *gorm.DB, *pluginpkg.EventBus) {
	t.Helper()
	db := setupFeatureTestDB(t)
	lg := featureTestLogger()
	cs := pluginpkg.NewConfigStore(db, "ai")
	cs.Set("inspection_ai_summary", "false") // skip LLM
	eb := pluginpkg.NewEventBus(lg)
	is := NewInspectionService(nil, api, cs, eb, db, lg)
	return is, db, eb
}

func TestInspection_RunBasic(t *testing.T) {
	api := &richStubCoreAPI{db: setupFeatureTestDB(t)}
	is, _, _ := newTestInspectionService(t, api)
	report, err := is.RunInspection()
	if err != nil {
		t.Fatalf("RunInspection: %v", err)
	}
	if report.Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}
	if report.Metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if len(report.Hosts) < 1 {
		t.Fatal("expected at least 1 host")
	}
	if report.AISummary != "" {
		t.Fatal("AI summary should be empty when disabled")
	}
}

func TestInspection_Findings_HighDisk(t *testing.T) {
	api := &richStubCoreAPI{
		db: setupFeatureTestDB(t),
		metricsOverride: map[string]interface{}{
			"disk_total": "100000000000", "disk_used": "92000000000",
			"mem_total_kb": "8000000", "mem_available_kb": "6000000",
			"load_1": "1.0",
		},
	}
	is, _, _ := newTestInspectionService(t, api)
	report, _ := is.RunInspection()
	found := false
	for _, f := range report.Findings {
		if f.Category == "disk" && f.Severity == "warning" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected disk warning finding, got %+v", report.Findings)
	}
}

func TestInspection_Findings_CriticalDisk(t *testing.T) {
	api := &richStubCoreAPI{
		db: setupFeatureTestDB(t),
		metricsOverride: map[string]interface{}{
			"disk_total": "100000000000", "disk_used": "97000000000",
			"mem_total_kb": "8000000", "mem_available_kb": "6000000",
			"load_1": "1.0",
		},
	}
	is, _, _ := newTestInspectionService(t, api)
	report, _ := is.RunInspection()
	found := false
	for _, f := range report.Findings {
		if f.Category == "disk" && f.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected critical disk finding, got %+v", report.Findings)
	}
}

func TestInspection_Findings_HighMemory(t *testing.T) {
	api := &richStubCoreAPI{
		db: setupFeatureTestDB(t),
		metricsOverride: map[string]interface{}{
			"disk_total": "100000000000", "disk_used": "50000000000",
			"mem_total_kb": "8000000", "mem_available_kb": "500000",
			"load_1": "1.0",
		},
	}
	is, _, _ := newTestInspectionService(t, api)
	report, _ := is.RunInspection()
	found := false
	for _, f := range report.Findings {
		if f.Category == "memory" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected memory finding, got %+v", report.Findings)
	}
}

func TestInspection_Findings_HighLoad(t *testing.T) {
	api := &richStubCoreAPI{
		db: setupFeatureTestDB(t),
		metricsOverride: map[string]interface{}{
			"disk_total": "100000000000", "disk_used": "50000000000",
			"mem_total_kb": "8000000", "mem_available_kb": "6000000",
			"load_1": "6.5",
		},
	}
	is, _, _ := newTestInspectionService(t, api)
	report, _ := is.RunInspection()
	found := false
	for _, f := range report.Findings {
		if f.Category == "cpu" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cpu/load finding, got %+v", report.Findings)
	}
}

func TestInspection_Findings_AbnormalContainer(t *testing.T) {
	api := &richStubCoreAPI{db: setupFeatureTestDB(t)}
	is, _, _ := newTestInspectionService(t, api)
	report, _ := is.RunInspection()
	found := false
	for _, f := range report.Findings {
		if f.Category == "container" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected container finding for restarting container, got %+v", report.Findings)
	}
}

func TestInspection_Findings_ManyAlerts(t *testing.T) {
	api := &richStubCoreAPI{db: setupFeatureTestDB(t)}
	is, _, _ := newTestInspectionService(t, api)
	report, _ := is.RunInspection()
	found := false
	for _, f := range report.Findings {
		if f.Category == "monitoring" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected monitoring finding for 6+ alerts, got %+v", report.Findings)
	}
}

func TestInspection_ScoreFindings(t *testing.T) {
	is := &InspectionService{}
	tests := []struct {
		name     string
		findings []InspectionFinding
		want     string
	}{
		{"no findings", nil, "healthy"},
		{"warning only", []InspectionFinding{{Severity: "warning"}}, "warning"},
		{"critical", []InspectionFinding{{Severity: "warning"}, {Severity: "critical"}}, "critical"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := is.scoreFindings(tc.findings)
			if got != tc.want {
				t.Errorf("scoreFindings = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInspection_SavesRecord(t *testing.T) {
	api := &richStubCoreAPI{db: setupFeatureTestDB(t)}
	is, db, _ := newTestInspectionService(t, api)
	is.db = db
	_, err := is.RunInspection()
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&InspectionRecord{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 record, got %d", count)
	}
}

func TestInspection_PublishesEvent(t *testing.T) {
	api := &richStubCoreAPI{db: setupFeatureTestDB(t)}
	is, db, eb := newTestInspectionService(t, api)
	is.db = db

	var received int32
	eb.Subscribe("system.inspection.completed", func(e pluginpkg.Event) {
		atomic.AddInt32(&received, 1)
	})

	is.RunInspection()

	if atomic.LoadInt32(&received) != 1 {
		t.Fatal("expected system.inspection.completed event")
	}
}

func TestInspection_GetHistory(t *testing.T) {
	api := &richStubCoreAPI{db: setupFeatureTestDB(t)}
	is, db, _ := newTestInspectionService(t, api)
	is.db = db
	is.RunInspection()
	is.RunInspection()

	records, err := is.GetHistory(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	// Should be DESC order
	if !records[0].Timestamp.After(records[1].Timestamp) && !records[0].Timestamp.Equal(records[1].Timestamp) {
		t.Fatal("expected DESC order")
	}
}

func TestInspection_GetHour(t *testing.T) {
	db := setupFeatureTestDB(t)
	cs := pluginpkg.NewConfigStore(db, "ai")
	is := &InspectionService{configStore: cs}

	tests := []struct {
		val  string
		want int
	}{
		{"", 8},    // default
		{"3", 3},
		{"25", 8},  // invalid
		{"-1", 8},  // invalid
		{"abc", 8}, // parse error
	}
	for _, tc := range tests {
		if tc.val != "" {
			cs.Set("inspection_hour", tc.val)
		} else {
			cs.Delete("inspection_hour")
		}
		if got := is.getHour(); got != tc.want {
			t.Errorf("getHour(%q) = %d, want %d", tc.val, got, tc.want)
		}
	}
}

func TestInspection_Reschedule(t *testing.T) {
	db := setupFeatureTestDB(t)
	cs := pluginpkg.NewConfigStore(db, "ai")
	lg := featureTestLogger()
	eb := pluginpkg.NewEventBus(lg)
	is := NewInspectionService(nil, &richStubCoreAPI{db: db}, cs, eb, db, lg)

	// Disabled — should not start
	cs.Set("inspection_enabled", "false")
	is.Start()
	if is.running {
		t.Fatal("should not be running when disabled")
	}

	// Enable — should start
	cs.Set("inspection_enabled", "true")
	is.Reschedule()
	if !is.running {
		t.Fatal("should be running after Reschedule with enabled=true")
	}

	// Stop — should not panic
	is.Stop()
	is.Stop() // double stop safe
	if is.running {
		t.Fatal("should not be running after Stop")
	}
}

// ============================================================================
// Section C: SelfHealEngine
// ============================================================================

func TestSelfHeal_ParseActions(t *testing.T) {
	sh := &SelfHealEngine{logger: featureTestLogger()}

	diagnosis := `## Diagnosis
Something is wrong.

## Auto-Heal Actions
` + "```json" + `
{"actions": [{"type": "restart_caddy"}, {"type": "restart_container", "container_id": "abc123"}]}
` + "```"

	actions := sh.parseActions(diagnosis)
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0].Type != "restart_caddy" {
		t.Errorf("action[0] type = %q, want restart_caddy", actions[0].Type)
	}
	if actions[1].ContainerID != "abc123" {
		t.Errorf("action[1] container_id = %q, want abc123", actions[1].ContainerID)
	}
}

func TestSelfHeal_ParseActions_NoBlock(t *testing.T) {
	sh := &SelfHealEngine{logger: featureTestLogger()}
	actions := sh.parseActions("No JSON block here, just text.")
	if actions != nil {
		t.Fatalf("expected nil, got %+v", actions)
	}
}

func TestSelfHeal_ParseActions_Malformed(t *testing.T) {
	sh := &SelfHealEngine{logger: featureTestLogger()}
	diagnosis := "```json\n{invalid json}\n```"
	actions := sh.parseActions(diagnosis)
	if actions != nil {
		t.Fatalf("expected nil for malformed JSON, got %+v", actions)
	}
}

func TestSelfHeal_SafeActions_Whitelist(t *testing.T) {
	expected := map[string]bool{
		"restart_caddy":     true,
		"reload_caddy":      true,
		"restart_container": true,
	}
	if len(safeActions) != len(expected) {
		t.Fatalf("safeActions has %d entries, want %d", len(safeActions), len(expected))
	}
	for k := range expected {
		if !safeActions[k] {
			t.Errorf("expected %q in safeActions", k)
		}
	}
	// Verify unsafe actions are NOT in whitelist
	for _, unsafe := range []string{"delete_container", "rm", "run_command", "docker_prune"} {
		if safeActions[unsafe] {
			t.Errorf("unsafe action %q should not be in whitelist", unsafe)
		}
	}
}

func TestSelfHeal_RuleID_TypeSwitch(t *testing.T) {
	lg := featureTestLogger()
	db := setupFeatureTestDB(t)
	eb := pluginpkg.NewEventBus(lg)
	api := &richStubCoreAPI{db: db}
	sh := NewSelfHealEngine(nil, api, eb, lg)

	tests := []struct {
		name    string
		ruleID  interface{}
		wantUID uint
	}{
		{"uint", uint(42), 42},
		{"float64", float64(42), 42},
		{"int", int(42), 42},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := pluginpkg.Event{
				Type: "monitoring.alert.fired",
				Payload: map[string]interface{}{
					"rule_id":        tc.ruleID,
					"auto_heal_mode": "notify", // notify = skip actual processing
				},
			}
			// handleAlert should not panic for any type
			sh.handleAlert(e)
		})
	}
}

func TestSelfHeal_NotifyMode_Skip(t *testing.T) {
	lg := featureTestLogger()
	db := setupFeatureTestDB(t)
	eb := pluginpkg.NewEventBus(lg)
	api := &richStubCoreAPI{db: db}
	sh := NewSelfHealEngine(nil, api, eb, lg)

	// notify mode should return immediately (no svc.DiagnoseSync call)
	// If it tried to call DiagnoseSync with nil svc, it would panic
	e := pluginpkg.Event{
		Type: "monitoring.alert.fired",
		Payload: map[string]interface{}{
			"rule_id":        uint(1),
			"auto_heal_mode": "notify",
		},
	}
	// Should not panic
	sh.handleAlert(e)
}

func TestSelfHeal_RateLimiting(t *testing.T) {
	lg := featureTestLogger()
	db := setupFeatureTestDB(t)
	eb := pluginpkg.NewEventBus(lg)
	api := &richStubCoreAPI{db: db}
	sh := NewSelfHealEngine(nil, api, eb, lg)

	// Set lastHealTime to now — should be rate-limited
	sh.lastHealTime = time.Now()

	e := pluginpkg.Event{
		Type: "monitoring.alert.fired",
		Payload: map[string]interface{}{
			"rule_id":        uint(1),
			"auto_heal_mode": "suggest",
			"rule_name":      "test",
			"metric":         "cpu",
			"value":          float64(95),
			"threshold":      float64(90),
		},
	}
	// Would panic calling DiagnoseSync(nil svc) if not rate-limited
	sh.handleAlert(e)
}

// ============================================================================
// Section D: Tool Count and Classification
// ============================================================================

func TestToolCount_Total(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	count := len(r.All())
	if count < 65 {
		t.Fatalf("expected at least 65 tools, got %d", count)
	}
}

func TestToolCount_NLOps(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	found := 0
	for _, name := range nlOpsToolNames {
		if r.Get(name) != nil {
			found++
		}
	}
	if found != 19 {
		t.Fatalf("expected 19 NLOps tools, found %d", found)
	}
}

func TestTools_AllHaveDescription(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	for _, tool := range r.All() {
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
	}
}

func TestTools_AllHaveHandler(t *testing.T) {
	r, _ := newRichTestRegistry(t)
	for _, tool := range r.All() {
		if tool.Handler == nil {
			t.Errorf("tool %q has nil handler", tool.Name)
		}
	}
}

// ============================================================================
// Section E: Utility Functions
// ============================================================================

func TestParseStringToFloat(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want float64
	}{
		{"nil", nil, 0},
		{"string number", "123.45", 123.45},
		{"string invalid", "abc", 0},
		{"float64", float64(42.5), 42.5},
		{"int", int(10), 10},
		{"uint64", uint64(999), 999},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStringToFloat(tc.val)
			if got != tc.want {
				t.Errorf("parseStringToFloat(%v) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want float64
		ok   bool
	}{
		{"float64", float64(1.5), 1.5, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", int(3), 3, true},
		{"int64", int64(4), 4, true},
		{"uint", uint(5), 5, true},
		{"uint64", uint64(6), 6, true},
		{"json.Number", json.Number("7.5"), 7.5, true},
		{"string", "8", 0, false},
		{"nil", nil, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toFloat64(tc.val)
			if ok != tc.ok {
				t.Errorf("toFloat64(%v): ok = %v, want %v", tc.val, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("toFloat64(%v) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if got := truncate(short, 10); got != short {
		t.Errorf("truncate(%q, 10) = %q, want %q", short, got, short)
	}

	long := "abcdefghij"
	got := truncate(long, 5)
	if got != "abcde..." {
		t.Errorf("truncate(%q, 5) = %q, want %q", long, got, "abcde...")
	}
}
