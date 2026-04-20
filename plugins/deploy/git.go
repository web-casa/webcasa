package deploy

import (
	"context"
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

// CloneToDir clones a git repository into an arbitrary destination directory
// (rather than the per-project default path returned by ProjectDir). Used
// by the preview deploy flow which maintains a separate source tree per
// preview so concurrent main-project builds don't stomp the PR checkout.
//
// Credentials:
//   - SSH: `deployKey` is written to a temp file and used via GIT_SSH_COMMAND
//     (same as Clone)
//   - HTTPS: pass the clean URL (no `x-access-token:<token>@` prefix) and
//     the token separately in `httpsToken`. It is passed to git through
//     the GIT_CONFIG_COUNT env-var ladder (env is NOT visible in `ps`),
//     scoped to the requesting host only via `http.<host>.extraHeader`
//     so the token cannot leak to an unexpected redirect target (Codex
//     R6-H1; supersedes the earlier `-c http.extraHeader` approach that
//     still embedded the secret in argv).
func (g *GitClient) CloneToDir(ctx context.Context, url, branch, deployKey, httpsToken, dstDir string, logWriter *LogWriter) error {
	if dstDir == "" {
		return fmt.Errorf("dstDir is required")
	}
	if _, err := os.Stat(dstDir); err == nil {
		if err := os.RemoveAll(dstDir); err != nil {
			return fmt.Errorf("cleanup existing dir: %w", err)
		}
	}

	gitArgs := []string{"clone", "--depth", "1", "--branch", branch, url, dstDir}
	cmd := exec.CommandContext(ctx, "git", gitArgs...)

	// Start from the base environment so setupDeployKey's GIT_SSH_COMMAND
	// is preserved. If only httpsToken is set, setupDeployKey is a no-op
	// and cmd.Env stays at nil (inherit) until we append below.
	cleanup, err := g.setupDeployKey(cmd, deployKey)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	if httpsToken != "" {
		// Use GIT_CONFIG_COUNT env-var ladder rather than `-c <kv>` argv.
		// Env vars are NOT exposed in `ps` on Linux (unlike argv). Scope
		// the extra header to the repo's host so a redirect to a
		// different origin doesn't inherit the Authorization header.
		host := extractHost(url)
		headerKey := "http.extraHeader"
		if host != "" {
			// http.<origin>.extraHeader targets only requests to that origin.
			headerKey = fmt.Sprintf("http.https://%s/.extraHeader", host)
		}
		env := cmd.Env
		if env == nil {
			env = os.Environ()
		}
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0="+headerKey,
			"GIT_CONFIG_VALUE_0=Authorization: Bearer "+httpsToken,
		)
		cmd.Env = env
	}

	if logWriter != nil {
		cmd.Stdout = logWriter
		cmd.Stderr = logWriter
		// Log a redacted form so the token doesn't hit persistent logs.
		logWriter.Write([]byte(fmt.Sprintf("$ git clone --depth 1 --branch %s %s %s\n",
			branch, sanitizeURL(url), dstDir)))
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

// extractHost returns the host portion of an HTTPS URL (e.g.
// "github.com" for "https://github.com/owner/repo.git"), or "" if the
// URL isn't in a recognizable HTTPS form. Used to scope git's
// extraHeader config to the requesting origin only.
func extractHost(u string) string {
	idx := strings.Index(u, "://")
	if idx == -1 {
		return ""
	}
	rest := u[idx+3:]
	// Strip any user:pass@ prefix (shouldn't be present in the clean URL
	// path used for preview, but defensive).
	if at := strings.Index(rest, "@"); at != -1 {
		rest = rest[at+1:]
	}
	if slash := strings.Index(rest, "/"); slash != -1 {
		rest = rest[:slash]
	}
	return rest
}

// Pull performs a git pull in the project directory.
// If httpsURL is non-empty, the remote origin is temporarily updated to use the fresh
// token-authenticated URL (for GitHub App auth where tokens expire after 1 hour).
func (g *GitClient) Pull(deployKey string, httpsURL string, projectID uint, logWriter *LogWriter) error {
	dir := g.ProjectDir(projectID)

	// If an HTTPS URL with a fresh token is provided, update the remote before pulling.
	if httpsURL != "" {
		setCmd := exec.Command("git", "remote", "set-url", "origin", httpsURL)
		setCmd.Dir = dir
		if out, err := setCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("set remote URL: %s: %w", string(out), err)
		}
	}

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

	// After pull, sanitize the remote URL to remove the token.
	if httpsURL != "" {
		sanitized := sanitizeRemoteURL(httpsURL)
		cleanCmd := exec.Command("git", "remote", "set-url", "origin", sanitized)
		cleanCmd.Dir = dir
		_ = cleanCmd.Run() // best-effort cleanup
	}

	return nil
}

// sanitizeRemoteURL removes embedded credentials from an HTTPS git URL.
func sanitizeRemoteURL(u string) string {
	if idx := strings.Index(u, "://"); idx != -1 {
		rest := u[idx+3:]
		if atIdx := strings.Index(rest, "@"); atIdx != -1 {
			return u[:idx+3] + rest[atIdx+1:]
		}
	}
	return u
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
