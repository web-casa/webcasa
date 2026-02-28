package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitClient handles git clone and pull operations.
type GitClient struct {
	workDir string // base directory for project sources
}

// NewGitClient creates a new git client with the given work directory.
func NewGitClient(workDir string) *GitClient {
	return &GitClient{workDir: workDir}
}

// ProjectDir returns the absolute path of a project's source code.
func (g *GitClient) ProjectDir(projectID uint) string {
	return filepath.Join(g.workDir, fmt.Sprintf("project_%d", projectID))
}

// Clone clones a git repository. If deployKey is provided, it's used for SSH auth.
func (g *GitClient) Clone(url, branch, deployKey string, projectID uint, logWriter *LogWriter) error {
	dir := g.ProjectDir(projectID)

	// Clean up existing directory if present
	if _, err := os.Stat(dir); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("cleanup existing dir: %w", err)
		}
	}

	args := []string{"clone", "--depth", "1", "--branch", branch, url, dir}
	cmd := exec.Command("git", args...)

	// Set up deploy key if provided
	cleanup, err := g.setupDeployKey(cmd, deployKey)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	logWriter.Write([]byte(fmt.Sprintf("$ git clone --depth 1 --branch %s %s\n", branch, sanitizeURL(url))))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

// Pull performs a git pull in the project directory.
func (g *GitClient) Pull(deployKey string, projectID uint, logWriter *LogWriter) error {
	dir := g.ProjectDir(projectID)

	cmd := exec.Command("git", "pull", "--ff-only")
	cmd.Dir = dir

	cleanup, err := g.setupDeployKey(cmd, deployKey)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	logWriter.Write([]byte("$ git pull --ff-only\n"))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}
	return nil
}

// GetCommitHash returns the current commit hash of the project directory.
func (g *GitClient) GetCommitHash(projectID uint) (string, error) {
	dir := g.ProjectDir(projectID)
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// setupDeployKey writes the deploy key to a temp file and configures GIT_SSH_COMMAND.
func (g *GitClient) setupDeployKey(cmd *exec.Cmd, deployKey string) (func(), error) {
	if deployKey == "" {
		return nil, nil
	}

	tmpFile, err := os.CreateTemp("", "deploy_key_*")
	if err != nil {
		return nil, fmt.Errorf("create temp deploy key: %w", err)
	}

	if _, err := tmpFile.WriteString(deployKey); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return nil, fmt.Errorf("write deploy key: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpFile.Name(), 0600); err != nil {
		os.Remove(tmpFile.Name())
		return nil, fmt.Errorf("chmod deploy key: %w", err)
	}

	sshCmd := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=~/.ssh/known_hosts", tmpFile.Name())
	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+sshCmd)

	return func() { os.Remove(tmpFile.Name()) }, nil
}

// sanitizeURL redacts credentials from a git URL for logging.
func sanitizeURL(url string) string {
	// Redact user:pass from https://user:pass@host/repo
	if idx := strings.Index(url, "://"); idx != -1 {
		rest := url[idx+3:]
		if atIdx := strings.Index(rest, "@"); atIdx != -1 {
			return url[:idx+3] + "***@" + rest[atIdx+1:]
		}
	}
	return url
}
