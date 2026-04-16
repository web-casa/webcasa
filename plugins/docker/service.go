package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Service implements the business logic for Docker management.
type Service struct {
	db      *gorm.DB
	client  *Client
	dataDir string
	logger  *slog.Logger
}

// NewService creates a Docker Service.
func NewService(db *gorm.DB, client *Client, dataDir string, logger *slog.Logger) *Service {
	return &Service{
		db:      db,
		client:  client,
		dataDir: dataDir,
		logger:  logger,
	}
}

// ── Stack CRUD ──

// ListStacks returns all stacks.
func (s *Service) ListStacks() ([]Stack, error) {
	var stacks []Stack
	if err := s.db.Order("id ASC").Find(&stacks).Error; err != nil {
		return nil, err
	}

	// Refresh status from Docker — fetch containers once, then match in memory.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	containers, err := s.client.ListContainers(ctx, true)
	if err != nil {
		// If Docker is unreachable, mark all as unknown.
		for i := range stacks {
			stacks[i].Status = "unknown"
		}
		return stacks, nil
	}

	for i := range stacks {
		stacks[i].Status = matchStackStatus(stacks[i].Name, containers)
	}
	return stacks, nil
}

// matchStackStatus determines stack status from a pre-fetched container list
// using a priority-based state machine (inspired by Coolify's ContainerStatusAggregator).
// Returns: "running", "degraded", "starting", "paused", or "stopped".
func matchStackStatus(name string, containers []ContainerInfo) string {
	sanitized := sanitizeName(name)

	var states []string
	for _, c := range containers {
		project := c.Labels["com.docker.compose.project"]
		if project == name || project == sanitized {
			states = append(states, c.State)
		}
	}

	if len(states) == 0 {
		return "stopped"
	}

	running, restarting, exited, paused, dead, created := 0, 0, 0, 0, 0, 0
	for _, st := range states {
		switch st {
		case "running":
			running++
		case "restarting":
			restarting++
		case "exited":
			exited++
		case "dead", "removing":
			dead++
		case "paused":
			paused++
		case "created":
			created++
		}
	}

	switch {
	case dead > 0:
		return "degraded"
	case restarting > 0:
		return "degraded"
	case running > 0 && exited > 0:
		return "degraded"
	case running > 0 && created > 0:
		return "starting"
	case running > 0 && running == len(states):
		return "running"
	case paused > 0 && paused == len(states):
		return "paused"
	case created > 0:
		return "starting"
	case running > 0:
		return "starting"
	default:
		return "stopped"
	}
}

// GetStack returns a single stack.
func (s *Service) GetStack(id uint) (*Stack, error) {
	var stack Stack
	if err := s.db.First(&stack, id).Error; err != nil {
		return nil, err
	}
	stack.Status = s.resolveStackStatus(stack.Name)
	return &stack, nil
}

// CreateStackRequest is the input for creating a stack.
type CreateStackRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	ComposeFile string `json:"compose_file" binding:"required"`
	EnvFile     string `json:"env_file"`
	AutoStart   bool   `json:"auto_start"`
}

// CreateStack creates a new stack and optionally starts it.
func (s *Service) CreateStack(req *CreateStackRequest) (*Stack, error) {
	// Check name uniqueness.
	var count int64
	s.db.Model(&Stack{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("stack name %q already exists", req.Name)
	}

	// Validate that the sanitized name is not empty/generic after cleaning.
	sanitized := sanitizeName(req.Name)
	if sanitized == "unnamed" {
		return nil, fmt.Errorf("stack name must contain at least one letter or digit")
	}

	// Check slug collision: different names like "Foo!" and "Foo?" map to the same
	// compose project name, causing cross-interference.
	var existingStacks []Stack
	s.db.Select("name").Find(&existingStacks)
	for _, ex := range existingStacks {
		if sanitizeName(ex.Name) == sanitized {
			return nil, fmt.Errorf("stack name %q conflicts with existing stack %q (same compose project name %q)", req.Name, ex.Name, sanitized)
		}
	}

	stackDir := filepath.Join(s.dataDir, "stacks", sanitized)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return nil, fmt.Errorf("create stack dir: %w", err)
	}

	// Write compose file.
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(req.ComposeFile), 0600); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}

	// Write env file if provided.
	if req.EnvFile != "" {
		envPath := filepath.Join(stackDir, ".env")
		if err := os.WriteFile(envPath, []byte(req.EnvFile), 0600); err != nil {
			return nil, fmt.Errorf("write env file: %w", err)
		}
	}

	stack := &Stack{
		Name:        req.Name,
		Description: req.Description,
		ComposeFile: req.ComposeFile,
		EnvFile:     req.EnvFile,
		Status:      "stopped",
		DataDir:     stackDir,
	}

	if err := s.db.Create(stack).Error; err != nil {
		return nil, fmt.Errorf("create stack record: %w", err)
	}

	if req.AutoStart {
		if err := s.StackUp(stack.ID); err != nil {
			s.logger.Error("auto-start failed", "stack", req.Name, "err", err)
			s.db.Model(stack).Update("status", "error")
			// Return the stack with error status so the caller sees the failure.
			stack, _ = s.GetStack(stack.ID)
			return stack, fmt.Errorf("stack created but auto-start failed: %w", err)
		}
	}

	return s.GetStack(stack.ID)
}

// UpdateStack updates a stack's compose file and env.
func (s *Service) UpdateStack(id uint, req *CreateStackRequest) (*Stack, error) {
	stack, err := s.GetStack(id)
	if err != nil {
		return nil, err
	}

	if stack.ManagedBy != "" {
		return nil, fmt.Errorf("stack is managed by %s, please use the %s plugin to manage it", stack.ManagedBy, stack.ManagedBy)
	}

	stack.Description = req.Description
	stack.ComposeFile = req.ComposeFile
	stack.EnvFile = req.EnvFile

	// Rewrite files.
	composePath := filepath.Join(stack.DataDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(req.ComposeFile), 0600); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}
	envPath := filepath.Join(stack.DataDir, ".env")
	if req.EnvFile != "" {
		if err := os.WriteFile(envPath, []byte(req.EnvFile), 0600); err != nil {
			return nil, fmt.Errorf("write .env file: %w", err)
		}
	} else {
		os.Remove(envPath)
	}

	if err := s.db.Save(stack).Error; err != nil {
		return nil, err
	}
	return s.GetStack(id)
}

// DeleteStack stops and removes a stack.
func (s *Service) DeleteStack(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}

	if stack.ManagedBy != "" {
		return fmt.Errorf("stack is managed by %s, please use the %s plugin to manage it", stack.ManagedBy, stack.ManagedBy)
	}

	// Stop containers — if this fails, do not proceed with cleanup so the
	// stack remains manageable from the UI.
	if err := s.runCompose(stack.Name, stack.DataDir, "down", "--remove-orphans"); err != nil {
		s.logger.Error("compose down failed", "stack", stack.Name, "err", err)
		s.db.Model(stack).Update("status", "error")
		return fmt.Errorf("failed to stop stack: %w (stack kept in UI for manual cleanup)", err)
	}

	// Remove data directory.
	os.RemoveAll(stack.DataDir)

	return s.db.Delete(&Stack{}, id).Error
}

// ── Stack Lifecycle ──

// StackUp starts a stack (docker compose up -d).
// Pulls images first to avoid failures on first start.
func (s *Service) StackUp(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}
	// Pull images first (ignore errors — image may be local/built).
	_ = s.runCompose(stack.Name, stack.DataDir, "pull")
	return s.runCompose(stack.Name, stack.DataDir, "up", "-d", "--remove-orphans")
}

// StackDown stops a stack (docker compose down).
func (s *Service) StackDown(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}
	return s.runCompose(stack.Name, stack.DataDir, "down")
}

// StackRestart restarts a stack.
func (s *Service) StackRestart(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}
	return s.runCompose(stack.Name, stack.DataDir, "restart")
}

// StackPull pulls the latest images for a stack.
func (s *Service) StackPull(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}
	return s.runCompose(stack.Name, stack.DataDir, "pull")
}

// StackLogs returns recent logs for a stack.
func (s *Service) StackLogs(id uint, tail string) (string, error) {
	stack, err := s.GetStack(id)
	if err != nil {
		return "", err
	}
	if tail == "" {
		tail = "200"
	}
	output, err := s.runComposeOutput(stack.Name, stack.DataDir, "logs", "--tail", tail, "--no-color")
	if err != nil {
		return "", err
	}
	return output, nil
}

// StackLogsFollow starts a streaming docker compose logs --follow process.
// Returns an io.ReadCloser that the caller should close when done.
func (s *Service) StackLogsFollow(ctx context.Context, id uint, tail string) (io.ReadCloser, error) {
	stack, err := s.GetStack(id)
	if err != nil {
		return nil, err
	}
	if tail == "" {
		tail = "100"
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--follow", "--tail", tail, "--no-color")
	cmd.Dir = stack.DataDir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+sanitizeName(stack.Name))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Clean up process when reader is closed
	go func() {
		cmd.Wait()
	}()

	return stdout, nil
}

// ── Helpers ──

// resolveStackStatus checks Docker for actual container states of a compose project.
// Uses the same aggregation logic as ListStacks for consistency.
func (s *Service) resolveStackStatus(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	containers, err := s.client.ListContainers(ctx, true)
	if err != nil {
		return "unknown"
	}

	return matchStackStatus(name, containers)
}

// runCompose executes a docker compose command in the given directory.
// name is used as the COMPOSE_PROJECT_NAME to ensure consistency.
func (s *Service) runCompose(name, dir string, args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+sanitizeName(name))

	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(output))
		s.logger.Error("docker compose failed",
			"dir", dir,
			"args", strings.Join(args, " "),
			"output", outStr,
			"err", err,
		)
		if outStr != "" {
			return fmt.Errorf("docker compose %s: %s", args[0], outStr)
		}
		return fmt.Errorf("docker compose %s: %v", args[0], err)
	}
	return nil
}

// runComposeOutput executes a compose command and returns stdout.
func (s *Service) runComposeOutput(name, dir string, args ...string) (string, error) {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+sanitizeName(name))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose %s: %s", args[0], strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// sanitizeName converts a stack name to a filesystem-safe string.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "unnamed"
	}
	return name
}
