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
	// Refresh status from Docker.
	for i := range stacks {
		stacks[i].Status = s.resolveStackStatus(stacks[i].Name)
	}
	return stacks, nil
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

	// Prepare data directory.
	stackDir := filepath.Join(s.dataDir, "stacks", sanitizeName(req.Name))
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return nil, fmt.Errorf("create stack dir: %w", err)
	}

	// Write compose file.
	composePath := filepath.Join(stackDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(req.ComposeFile), 0644); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}

	// Write env file if provided.
	if req.EnvFile != "" {
		envPath := filepath.Join(stackDir, ".env")
		if err := os.WriteFile(envPath, []byte(req.EnvFile), 0644); err != nil {
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
			stack.Status = "error"
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

	stack.Description = req.Description
	stack.ComposeFile = req.ComposeFile
	stack.EnvFile = req.EnvFile

	// Rewrite files.
	composePath := filepath.Join(stack.DataDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(req.ComposeFile), 0644); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}
	envPath := filepath.Join(stack.DataDir, ".env")
	if req.EnvFile != "" {
		if err := os.WriteFile(envPath, []byte(req.EnvFile), 0644); err != nil {
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

	// Stop containers.
	_ = s.runCompose(stack.DataDir, "down", "--remove-orphans")

	// Remove data directory.
	os.RemoveAll(stack.DataDir)

	return s.db.Delete(&Stack{}, id).Error
}

// ── Stack Lifecycle ──

// StackUp starts a stack (docker compose up -d).
func (s *Service) StackUp(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}
	return s.runCompose(stack.DataDir, "up", "-d", "--remove-orphans")
}

// StackDown stops a stack (docker compose down).
func (s *Service) StackDown(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}
	return s.runCompose(stack.DataDir, "down")
}

// StackRestart restarts a stack.
func (s *Service) StackRestart(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}
	return s.runCompose(stack.DataDir, "restart")
}

// StackPull pulls the latest images for a stack.
func (s *Service) StackPull(id uint) error {
	stack, err := s.GetStack(id)
	if err != nil {
		return err
	}
	return s.runCompose(stack.DataDir, "pull")
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
	output, err := s.runComposeOutput(stack.DataDir, "logs", "--tail", tail, "--no-color")
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
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(stack.DataDir))

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
func (s *Service) resolveStackStatus(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	containers, err := s.client.ListContainers(ctx, true)
	if err != nil {
		return "unknown"
	}

	total, running := 0, 0
	for _, c := range containers {
		project := c.Labels["com.docker.compose.project"]
		if project == name || project == sanitizeName(name) {
			total++
			if c.State == "running" {
				running++
			}
		}
	}

	switch {
	case total == 0:
		return "stopped"
	case running == total:
		return "running"
	case running > 0:
		return "partial"
	default:
		return "stopped"
	}
}

// runCompose executes a docker compose command in the given directory.
func (s *Service) runCompose(dir string, args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("docker compose failed",
			"dir", dir,
			"args", strings.Join(args, " "),
			"output", string(output),
			"err", err,
		)
		return fmt.Errorf("docker compose %s: %s", args[0], strings.TrimSpace(string(output)))
	}
	return nil
}

// runComposeOutput executes a compose command and returns stdout.
func (s *Service) runComposeOutput(dir string, args ...string) (string, error) {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

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
	return strings.Trim(name, "-")
}
