package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// ── Detector tests ──

func TestDetectFramework_Dockerfile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node:18\nCOPY . .\nCMD [\"node\", \"index.js\"]"), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "dockerfile" {
		t.Fatalf("expected dockerfile, got %s", preset.Framework)
	}
}

func TestDetectFramework_NextJS(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"next":"14.0.0","react":"18.0.0"}}`), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "nextjs" {
		t.Fatalf("expected nextjs, got %s", preset.Framework)
	}
}

func TestDetectFramework_Nuxt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"nuxt":"3.0.0"}}`), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "nuxt" {
		t.Fatalf("expected nuxt, got %s", preset.Framework)
	}
}

func TestDetectFramework_Go(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.21"), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "go" {
		t.Fatalf("expected go, got %s", preset.Framework)
	}
}

func TestDetectFramework_Express(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"express":"4.18.0"}}`), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "express" {
		t.Fatalf("expected express, got %s", preset.Framework)
	}
}

func TestDetectFramework_Vite(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"devDependencies":{"vite":"5.0.0"}}`), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "vite" {
		t.Fatalf("expected vite, got %s", preset.Framework)
	}
}

func TestDetectFramework_Django(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "manage.py"), []byte("#!/usr/bin/env python"), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "django" {
		t.Fatalf("expected django, got %s", preset.Framework)
	}
}

func TestDetectFramework_Flask(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask==3.0.0\nrequests==2.31.0"), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "flask" {
		t.Fatalf("expected flask, got %s", preset.Framework)
	}
}

func TestDetectFramework_Laravel(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laravel/framework":"^10.0"}}`), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "laravel" {
		t.Fatalf("expected laravel, got %s", preset.Framework)
	}
}

func TestDetectFramework_Custom(t *testing.T) {
	dir := t.TempDir()
	// Empty directory — no recognizable framework

	preset := DetectFramework(dir)
	if preset.Framework != "custom" {
		t.Fatalf("expected custom, got %s", preset.Framework)
	}
}

func TestDetectFramework_DockerfilePriority(t *testing.T) {
	dir := t.TempDir()
	// Both Dockerfile and package.json exist; Dockerfile should win.
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node:18"), 0644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"next":"14.0.0"}}`), 0644)

	preset := DetectFramework(dir)
	if preset.Framework != "dockerfile" {
		t.Fatalf("expected dockerfile (highest priority), got %s", preset.Framework)
	}
}

// ── Model tests ──

func TestGetEnvSuggestions(t *testing.T) {
	tests := []struct {
		framework string
		expectLen int
	}{
		{"nextjs", 3},
		{"laravel", 9},
		{"go", 2},
		{"flask", 3},
		{"django", 5},
		{"custom", 0},
		{"unknown", 0},
	}

	for _, tt := range tests {
		suggestions := GetEnvSuggestions(tt.framework)
		if len(suggestions) != tt.expectLen {
			t.Errorf("GetEnvSuggestions(%s): expected %d, got %d", tt.framework, tt.expectLen, len(suggestions))
		}
	}
}

func TestGetEnvSuggestions_RequiredFields(t *testing.T) {
	suggestions := GetEnvSuggestions("laravel")
	requiredCount := 0
	for _, s := range suggestions {
		if s.Required {
			requiredCount++
		}
	}
	if requiredCount < 3 {
		t.Fatalf("expected at least 3 required env vars for Laravel, got %d", requiredCount)
	}
}

// ── Builder helper tests ──

func TestPortAllocator(t *testing.T) {
	pa := NewPortAllocator(10000)

	port := pa.AllocatePort(5)
	if port != 10005 {
		t.Fatalf("expected 10005, got %d", port)
	}
}

func TestPortAllocator_AlternatePort(t *testing.T) {
	pa := NewPortAllocator(10000)

	primary := pa.AllocatePort(5) // 10005
	alt := pa.AlternatePort(primary, 5)
	if alt != 15005 {
		t.Fatalf("expected 15005, got %d", alt)
	}

	// Alternate of alternate should return primary
	back := pa.AlternatePort(alt, 5)
	if back != primary {
		t.Fatalf("expected %d, got %d", primary, back)
	}
}

func TestCacheDir(t *testing.T) {
	dataDir := t.TempDir()
	git := NewGitClient(filepath.Join(dataDir, "sources"))
	b := NewBuilder(git, dataDir)

	dir := b.CacheDir(42)
	expected := filepath.Join(dataDir, "cache", "project_42")
	if dir != expected {
		t.Fatalf("expected %s, got %s", expected, dir)
	}
}

func TestClearCache(t *testing.T) {
	dataDir := t.TempDir()
	git := NewGitClient(filepath.Join(dataDir, "sources"))
	b := NewBuilder(git, dataDir)

	// Create a cache dir with a file
	cacheDir := b.CacheDir(1)
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "test.cache"), []byte("data"), 0644)

	if b.CacheSize(1) == 0 {
		t.Fatal("expected non-zero cache size before clear")
	}

	if err := b.ClearCache(1); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}

	if b.CacheSize(1) != 0 {
		t.Fatal("expected zero cache size after clear")
	}
}

func TestGenerateEnvFile(t *testing.T) {
	dir := t.TempDir()
	envVars := []EnvVar{
		{Key: "NODE_ENV", Value: "production"},
		{Key: "PORT", Value: "3000"},
	}

	err := GenerateEnvFile(dir, envVars)
	if err != nil {
		t.Fatalf("GenerateEnvFile failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("read .env failed: %v", err)
	}

	content := string(data)
	if !contains(content, "NODE_ENV=production") {
		t.Fatal("expected NODE_ENV=production in .env")
	}
	if !contains(content, "PORT=3000") {
		t.Fatal("expected PORT=3000 in .env")
	}
}

func TestGenerateEnvFile_Empty(t *testing.T) {
	dir := t.TempDir()
	err := GenerateEnvFile(dir, nil)
	if err != nil {
		t.Fatalf("expected nil error for empty env vars, got: %v", err)
	}

	// Should not create .env file for empty env vars
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatal("expected no .env file for empty env vars")
	}
}

// ── HealthChecker tests ──

func TestHealthChecker_SkipNoPort(t *testing.T) {
	hc := NewHealthChecker()
	err := hc.WaitHealthy(0, "/", 1, 1)
	if err != nil {
		t.Fatalf("expected nil for port 0, got: %v", err)
	}
}

func TestHealthChecker_UnreachablePort(t *testing.T) {
	hc := NewHealthChecker()
	// Use a port that's very unlikely to be in use
	err := hc.WaitHealthy(59999, "/", 1, 2)
	if err == nil {
		t.Fatal("expected error for unreachable port")
	}
}

// ── Framework presets tests ──

func TestFrameworkPresets_Completeness(t *testing.T) {
	expected := []string{"nextjs", "nuxt", "vite", "remix", "express", "go", "laravel", "flask", "django", "dockerfile", "custom"}
	for _, fw := range expected {
		if _, ok := frameworkPresets[fw]; !ok {
			t.Errorf("missing framework preset: %s", fw)
		}
	}
}

func TestContains(t *testing.T) {
	if !contains("hello world", "world") {
		t.Fatal("expected true")
	}
	if contains("hello", "world") {
		t.Fatal("expected false")
	}
	if contains("", "world") {
		t.Fatal("expected false for empty string")
	}
}
