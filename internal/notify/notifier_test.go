package notify

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestServer creates an httptest server that listens on IPv4 only,
// avoiding failures in environments where IPv6 (::1) is not permitted.
func newTestServer(handler http.Handler) *httptest.Server {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic("failed to listen on IPv4: " + err.Error())
	}
	server := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: handler},
	}
	server.Start()
	return server
}

func setupTestDB(t *testing.T) *gorm.DB {
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

	if err := db.AutoMigrate(&Channel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newTestNotifier(t *testing.T) (*Notifier, *gorm.DB) {
	t.Helper()
	db := setupTestDB(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	n := NewNotifier(db, log)
	n.skipSSRF = true // tests use 127.0.0.1 which SSRF check would block
	return n, db
}

func TestNotifier_CRUD(t *testing.T) {
	n, _ := newTestNotifier(t)

	// Create
	ch := &Channel{
		Type:    "webhook",
		Name:    "Test Webhook",
		Config:  `{"url":"https://example.com/webhook"}`,
		Enabled: true,
		Events:  `["deploy.*"]`,
	}
	if err := n.CreateChannel(ch); err != nil {
		t.Fatalf("create: %v", err)
	}
	if ch.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	// List
	channels, err := n.ListChannels()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}

	// Get
	got, err := n.GetChannel(ch.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Test Webhook" {
		t.Fatalf("expected name 'Test Webhook', got '%s'", got.Name)
	}

	// Update
	if err := n.UpdateChannel(ch.ID, map[string]interface{}{"name": "Updated"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = n.GetChannel(ch.ID)
	if got.Name != "Updated" {
		t.Fatalf("expected name 'Updated', got '%s'", got.Name)
	}

	// Delete
	if err := n.DeleteChannel(ch.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	channels, _ = n.ListChannels()
	if len(channels) != 0 {
		t.Fatalf("expected 0 channels after delete, got %d", len(channels))
	}
}

func TestNotifier_MatchesEvent(t *testing.T) {
	n, _ := newTestNotifier(t)

	tests := []struct {
		events    string
		eventType string
		expected  bool
	}{
		{`["deploy.*"]`, "deploy.build.failed", true},
		{`["deploy.*"]`, "backup.completed", false},
		{`["*"]`, "anything.here", true},
		{`["deploy.build.failed"]`, "deploy.build.failed", true},
		{`["deploy.build.failed"]`, "deploy.build.success", false},
		{``, "anything", true},              // empty = match all
		{`["backup.*", "deploy.*"]`, "deploy.started", true},
		{`["backup.*", "deploy.*"]`, "monitoring.alert", false},
	}

	for _, tt := range tests {
		ch := Channel{Events: tt.events}
		result := n.matchesEvent(ch, tt.eventType)
		if result != tt.expected {
			t.Errorf("matchesEvent(events=%s, type=%s): expected %v, got %v", tt.events, tt.eventType, tt.expected, result)
		}
	}
}

func TestNotifier_SendWebhook(t *testing.T) {
	var receivedBody []byte
	var receivedContentType string

	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.WriteHeader(200)
	}))
	defer server.Close()

	n, _ := newTestNotifier(t)

	cfg, _ := json.Marshal(WebhookConfig{URL: server.URL})
	ch := Channel{
		Type:   "webhook",
		Config: string(cfg),
	}

	event := NotifyEvent{
		Type:    "deploy.build.failed",
		Title:   "Build Failed",
		Message: "Project test failed to build",
		Time:    time.Now(),
	}

	err := n.sendWebhook(ch, event)
	if err != nil {
		t.Fatalf("sendWebhook failed: %v", err)
	}
	if receivedContentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", receivedContentType)
	}

	var received NotifyEvent
	if err := json.Unmarshal(receivedBody, &received); err != nil {
		t.Fatalf("unmarshal received body: %v", err)
	}
	if received.Type != "deploy.build.failed" {
		t.Fatalf("expected type deploy.build.failed, got %s", received.Type)
	}
}

func TestNotifier_SendWebhook_CustomHeaders(t *testing.T) {
	var receivedAuth string

	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer server.Close()

	n, _ := newTestNotifier(t)

	cfg, _ := json.Marshal(WebhookConfig{
		URL:     server.URL,
		Headers: map[string]string{"Authorization": "Bearer test-token"},
	})
	ch := Channel{Type: "webhook", Config: string(cfg)}

	err := n.sendWebhook(ch, NotifyEvent{Type: "test", Time: time.Now()})
	if err != nil {
		t.Fatalf("sendWebhook failed: %v", err)
	}
	if receivedAuth != "Bearer test-token" {
		t.Fatalf("expected Authorization header 'Bearer test-token', got '%s'", receivedAuth)
	}
}

func TestNotifier_SendWebhook_ServerError(t *testing.T) {
	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	n, _ := newTestNotifier(t)

	cfg, _ := json.Marshal(WebhookConfig{URL: server.URL})
	ch := Channel{Type: "webhook", Config: string(cfg)}

	err := n.sendWebhook(ch, NotifyEvent{Type: "test", Time: time.Now()})
	if err == nil {
		t.Fatal("expected error for server 500 response")
	}
}

func TestNotifier_SendWebhook_EmptyURL(t *testing.T) {
	n, _ := newTestNotifier(t)

	cfg, _ := json.Marshal(WebhookConfig{URL: ""})
	ch := Channel{Type: "webhook", Config: string(cfg)}

	err := n.sendWebhook(ch, NotifyEvent{Type: "test", Time: time.Now()})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestNotifier_TestChannel(t *testing.T) {
	received := false
	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(200)
	}))
	defer server.Close()

	n, _ := newTestNotifier(t)

	cfg, _ := json.Marshal(WebhookConfig{URL: server.URL})
	ch := Channel{Type: "webhook", Config: string(cfg)}

	err := n.TestChannel(ch)
	if err != nil {
		t.Fatalf("TestChannel failed: %v", err)
	}
	if !received {
		t.Fatal("expected webhook to be called")
	}
}

func TestNotifier_Send_MatchesEvents(t *testing.T) {
	callCount := 0
	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(200)
	}))
	defer server.Close()

	n, db := newTestNotifier(t)

	cfg, _ := json.Marshal(WebhookConfig{URL: server.URL})

	// Channel that only listens to deploy events
	ch := &Channel{
		Type:    "webhook",
		Name:    "Deploy Only",
		Config:  string(cfg),
		Enabled: true,
		Events:  `["deploy.*"]`,
	}
	db.Create(ch)

	// Send a deploy event — should match
	n.Send(NotifyEvent{Type: "deploy.build.failed", Title: "Build Failed", Time: time.Now()})
	time.Sleep(200 * time.Millisecond) // async send
	if callCount != 1 {
		t.Fatalf("expected 1 webhook call for deploy event, got %d", callCount)
	}

	// Send a backup event — should NOT match
	n.Send(NotifyEvent{Type: "backup.completed", Title: "Backup Done", Time: time.Now()})
	time.Sleep(200 * time.Millisecond)
	if callCount != 1 {
		t.Fatalf("expected still 1 webhook call (backup event should not match), got %d", callCount)
	}
}

func TestNotifier_Send_DisabledChannel(t *testing.T) {
	callCount := 0
	server := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(200)
	}))
	defer server.Close()

	n, db := newTestNotifier(t)

	cfg, _ := json.Marshal(WebhookConfig{URL: server.URL})
	ch := &Channel{
		Type:    "webhook",
		Name:    "Disabled",
		Config:  string(cfg),
		Enabled: true, // create as enabled first
		Events:  `["*"]`,
	}
	db.Create(ch)
	// Disable after creation to avoid GORM zero-value default behavior
	db.Model(ch).Update("enabled", false)

	n.Send(NotifyEvent{Type: "test", Title: "Test", Time: time.Now()})
	time.Sleep(200 * time.Millisecond)
	if callCount != 0 {
		t.Fatalf("expected 0 calls for disabled channel, got %d", callCount)
	}
}

// ── Model tests ──

func TestChannel_TableName(t *testing.T) {
	ch := Channel{}
	if ch.TableName() != "notify_channels" {
		t.Fatalf("expected notify_channels, got %s", ch.TableName())
	}
}
