package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Builder orchestrates the build pipeline for a project.
type Builder struct {
	git     *GitClient
	dataDir string
}

// NewBuilder creates a new Builder.
func NewBuilder(git *GitClient, dataDir string) *Builder {
	return &Builder{git: git, dataDir: dataDir}
}

// BuildResult holds the outcome of a build.
type BuildResult struct {
	Success   bool
	Commit    string
	Duration  time.Duration
	ErrorMsg  string
}

// CacheDir returns the shared cache directory for a project.
func (b *Builder) CacheDir(projectID uint) string {
	return filepath.Join(b.dataDir, "cache", fmt.Sprintf("project_%d", projectID))
}

// ClearCache removes the build cache for a project.
func (b *Builder) ClearCache(projectID uint) error {
	return os.RemoveAll(b.CacheDir(projectID))
}

// CacheSize returns the total size of the build cache for a project in bytes.
func (b *Builder) CacheSize(projectID uint) int64 {
	var size int64
	filepath.Walk(b.CacheDir(projectID), func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// setupBuildCache prepares cache directories and returns extra env vars for the build.
func (b *Builder) setupBuildCache(project *Project, logWriter *LogWriter) []string {
	cacheDir := b.CacheDir(project.ID)
	os.MkdirAll(cacheDir, 0755)

	var extraEnv []string

	switch {
	case project.Framework == "go":
		// Persistent Go module cache
		goCache := filepath.Join(cacheDir, "gomod")
		os.MkdirAll(goCache, 0755)
		extraEnv = append(extraEnv, fmt.Sprintf("GOMODCACHE=%s", goCache))
		goBuildCache := filepath.Join(cacheDir, "gobuild")
		os.MkdirAll(goBuildCache, 0755)
		extraEnv = append(extraEnv, fmt.Sprintf("GOCACHE=%s", goBuildCache))
		logWriter.Write([]byte(fmt.Sprintf("==> Build cache: GOMODCACHE=%s, GOCACHE=%s\n", goCache, goBuildCache)))

	case project.Framework == "nextjs" || project.Framework == "nuxt" || project.Framework == "vite" ||
		project.Framework == "remix" || project.Framework == "express":
		// npm cache for Node.js projects
		npmCache := filepath.Join(cacheDir, "npm")
		os.MkdirAll(npmCache, 0755)
		extraEnv = append(extraEnv, fmt.Sprintf("npm_config_cache=%s", npmCache))
		logWriter.Write([]byte(fmt.Sprintf("==> Build cache: npm_config_cache=%s\n", npmCache)))

	case project.Framework == "flask" || project.Framework == "django":
		// pip cache for Python projects
		pipCache := filepath.Join(cacheDir, "pip")
		os.MkdirAll(pipCache, 0755)
		extraEnv = append(extraEnv, fmt.Sprintf("PIP_CACHE_DIR=%s", pipCache))
		logWriter.Write([]byte(fmt.Sprintf("==> Build cache: PIP_CACHE_DIR=%s\n", pipCache)))

	case project.Framework == "laravel":
		// composer cache for PHP projects
		composerCache := filepath.Join(cacheDir, "composer")
		os.MkdirAll(composerCache, 0755)
		extraEnv = append(extraEnv, fmt.Sprintf("COMPOSER_CACHE_DIR=%s", composerCache))
		logWriter.Write([]byte(fmt.Sprintf("==> Build cache: COMPOSER_CACHE_DIR=%s\n", composerCache)))
	}

	return extraEnv
}

// Build executes the full build pipeline: clone/pull → install → build.
// It writes all output to the provided LogWriter.
func (b *Builder) Build(ctx context.Context, project *Project, logWriter *LogWriter) BuildResult {
	start := time.Now()
	projectDir := b.git.ProjectDir(project.ID)

	// Setup build cache
	cacheEnv := b.setupBuildCache(project, logWriter)

	// Step 1: Clone or pull
	logWriter.Write([]byte("=== Step 1/3: Fetching source code ===\n"))
	if _, err := os.Stat(filepath.Join(projectDir, ".git")); err == nil {
		// Directory exists, pull (pass GitURL for HTTPS token refresh)
		if err := b.git.Pull(project.DeployKey, project.GitURL, project.ID, logWriter); err != nil {
			return BuildResult{ErrorMsg: fmt.Sprintf("git pull failed: %v", err), Duration: time.Since(start)}
		}
	} else {
		// Fresh clone
		branch := project.GitBranch
		if branch == "" {
			branch = "main"
		}
		if err := b.git.Clone(project.GitURL, branch, project.DeployKey, project.ID, logWriter); err != nil {
			return BuildResult{ErrorMsg: fmt.Sprintf("git clone failed: %v", err), Duration: time.Since(start)}
		}
	}

	// Get commit hash
	commit, _ := b.git.GetCommitHash(project.ID)
	logWriter.Write([]byte(fmt.Sprintf("Commit: %s\n\n", commit)))

	// Step 2: Install dependencies
	if project.InstallCmd != "" {
		logWriter.Write([]byte("=== Step 2/3: Installing dependencies ===\n"))
		if err := b.runCommand(ctx, projectDir, project.InstallCmd, project.EnvVarList, cacheEnv, logWriter); err != nil {
			return BuildResult{Commit: commit, ErrorMsg: fmt.Sprintf("install failed: %v", err), Duration: time.Since(start)}
		}
		logWriter.Write([]byte("\n"))
	} else {
		logWriter.Write([]byte("=== Step 2/3: No install command, skipping ===\n\n"))
	}

	// Step 3: Build
	if project.BuildCommand != "" {
		logWriter.Write([]byte("=== Step 3/3: Building project ===\n"))
		if err := b.runCommand(ctx, projectDir, project.BuildCommand, project.EnvVarList, cacheEnv, logWriter); err != nil {
			return BuildResult{Commit: commit, ErrorMsg: fmt.Sprintf("build failed: %v", err), Duration: time.Since(start)}
		}
		logWriter.Write([]byte("\n"))
	} else {
		logWriter.Write([]byte("=== Step 3/3: No build command, skipping ===\n\n"))
	}

	logWriter.Write([]byte(fmt.Sprintf("=== Build completed in %s ===\n", time.Since(start).Round(time.Millisecond))))
	return BuildResult{
		Success:  true,
		Commit:   commit,
		Duration: time.Since(start),
	}
}

// runCommand executes a shell command in the given directory with env vars and extra env.
func (b *Builder) runCommand(ctx context.Context, dir, command string, envVars []EnvVar, extraEnv []string, logWriter *LogWriter) error {
	logWriter.Write([]byte(fmt.Sprintf("$ %s\n", command)))

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	// Build environment
	env := os.Environ()
	env = append(env, fmt.Sprintf("HOME=%s", dir))
	env = append(env, "NODE_ENV=production")
	for _, ev := range envVars {
		env = append(env, fmt.Sprintf("%s=%s", ev.Key, ev.Value))
	}
	env = append(env, extraEnv...)
	cmd.Env = env

	return cmd.Run()
}

// LogDir returns the log directory for a project.
func (b *Builder) LogDir(projectID uint) string {
	return filepath.Join(b.dataDir, "logs", fmt.Sprintf("project_%d", projectID))
}

// LogPath returns the log file path for a specific build.
func (b *Builder) LogPath(projectID uint, buildNum int) string {
	return filepath.Join(b.LogDir(projectID), fmt.Sprintf("build_%d.log", buildNum))
}

// ReadLog reads the full content of a build log file.
func (b *Builder) ReadLog(projectID uint, buildNum int) (string, error) {
	data, err := os.ReadFile(b.LogPath(projectID, buildNum))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// PortAllocator finds a free port for a project.
type PortAllocator struct {
	basePort int
}

// NewPortAllocator creates a port allocator starting from the given base port.
func NewPortAllocator(basePort int) *PortAllocator {
	return &PortAllocator{basePort: basePort}
}

// AllocatePort assigns a port based on the project ID to avoid conflicts.
func (pa *PortAllocator) AllocatePort(projectID uint) int {
	return pa.basePort + int(projectID)
}

// AlternatePort returns a different port for zero-downtime deployment.
// It alternates between the primary range (basePort+ID) and secondary range (basePort+5000+ID).
func (pa *PortAllocator) AlternatePort(currentPort int, projectID uint) int {
	primary := pa.basePort + int(projectID)
	alternate := primary + 5000
	if currentPort == alternate {
		return primary
	}
	return alternate
}

// GenerateEnvFile creates a .env file from env vars.
func GenerateEnvFile(dir string, envVars []EnvVar) error {
	if len(envVars) == 0 {
		return nil
	}
	var lines []string
	for _, ev := range envVars {
		lines = append(lines, fmt.Sprintf("%s=%s", ev.Key, ev.Value))
	}
	return os.WriteFile(filepath.Join(dir, ".env"), []byte(strings.Join(lines, "\n")+"\n"), 0600)
}
