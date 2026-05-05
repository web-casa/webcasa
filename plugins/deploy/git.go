package deploy

import (
	"context"
	"fmt"
	"net/url"
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

// Clone clones a git repository. Pass deployKey for SSH auth and/or
// httpsToken for HTTPS auth (token delivered via GIT_CONFIG_COUNT env
// var, never in argv — see injectHTTPSTokenEnv).
//
// v0.16 R8-M4 fix: previously the main Build path embedded the token
// directly in the URL via ConvertToHTTPS, which surfaced it in
// `git remote -v` and worker process listings. The clean-URL +
// env-var path matches what preview deploy has used since v0.14.
func (g *GitClient) Clone(url, branch, deployKey, httpsToken string, projectID uint, logWriter *LogWriter) error {
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

	if httpsToken != "" {
		injectHTTPSTokenEnv(cmd, url, httpsToken)
	}

	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	logWriter.Write([]byte(fmt.Sprintf("$ git clone --depth 1 --branch %s %s\n", branch, sanitizeURL(url))))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

// injectHTTPSTokenEnv appends the GIT_CONFIG_COUNT env-var ladder to
// cmd.Env so git uses the token as Authorization without exposing it
// to argv or the on-disk repo config (`git remote -v`).
//
// Scope is narrowed to the requesting origin via
// `http.https://<host>/.extraHeader` so a redirect to a different
// host doesn't inherit the credential.
//
// Preserves any env already set by setupDeployKey (GIT_SSH_COMMAND).
func injectHTTPSTokenEnv(cmd *exec.Cmd, url, token string) {
	host := extractHost(url)
	headerKey := "http.extraHeader"
	if host != "" {
		headerKey = fmt.Sprintf("http.https://%s/.extraHeader", host)
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	env = append(env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0="+headerKey,
		"GIT_CONFIG_VALUE_0=Authorization: Bearer "+token,
	)
	cmd.Env = env
}

// CloneAtSHA clones a git repository AT A SPECIFIC COMMIT, not at branch
// HEAD. Used by the preview deploy flow when an explicit head_sha is
// known from the webhook payload — eliminates the race window where a
// fork author can force-push between admin approval and the build's
// `git clone` execution (security review v0.19 R10-H1).
//
// Implementation: `git init` + `git fetch --depth 1 <url> <sha>` +
// `git checkout FETCH_HEAD`. GitHub serves the SHA explicitly via the
// uploadpack.allowReachableSHA1InWant capability (enabled by default).
//
// Falls back to CloneToDir (branch HEAD) when sha is empty.
func (g *GitClient) CloneAtSHA(ctx context.Context, url, sha, deployKey, httpsToken, dstDir string, logWriter *LogWriter) error {
	if sha == "" {
		return fmt.Errorf("CloneAtSHA: sha is required (use CloneToDir for branch HEAD)")
	}
	if dstDir == "" {
		return fmt.Errorf("dstDir is required")
	}
	if _, err := os.Stat(dstDir); err == nil {
		if err := os.RemoveAll(dstDir); err != nil {
			return fmt.Errorf("cleanup existing dir: %w", err)
		}
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("mkdir dst: %w", err)
	}
	// v019-R11-L1: clean up partial state on failure. Any of git
	// init/fetch/checkout/verify can leave a half-populated dstDir
	// (.git stub, partial tree). Caller's preview row would later
	// reference srcDir as if it had the right content; subsequent
	// runs would skip the clone (no, they re-RemoveAll above) — but
	// admin manual retry might use stale partial content. Defer
	// cleanup that only fires on error.
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = os.RemoveAll(dstDir)
		}
	}()

	// Helper to invoke git in dstDir with the same auth env CloneToDir uses.
	runGit := func(args []string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dstDir
		cleanup, err := g.setupDeployKey(cmd, deployKey)
		if err != nil {
			return err
		}
		if cleanup != nil {
			defer cleanup()
		}
		if httpsToken != "" {
			injectHTTPSTokenEnv(cmd, url, httpsToken)
		}
		if logWriter != nil {
			cmd.Stdout = logWriter
			cmd.Stderr = logWriter
		}
		return cmd.Run()
	}

	if logWriter != nil {
		logWriter.Write([]byte(fmt.Sprintf("$ git init && git fetch --depth 1 %s %s && git checkout %s\n",
			sanitizeURL(url), sha, sha)))
	}
	if err := runGit([]string{"init", "-q"}); err != nil {
		return fmt.Errorf("git init failed: %w", err)
	}
	if err := runGit([]string{"fetch", "--depth", "1", url, sha}); err != nil {
		return fmt.Errorf("git fetch %s failed: %w (the SHA may not exist on the fork — fork author may have force-pushed before our build started)", sha, err)
	}
	if err := runGit([]string{"checkout", "-q", "FETCH_HEAD"}); err != nil {
		return fmt.Errorf("git checkout %s failed: %w", sha, err)
	}
	// Defense-in-depth: verify the actual checked-out SHA matches what we
	// asked for. fetch+checkout-FETCH_HEAD should always resolve to the
	// requested SHA, but a misconfigured server or git client behavior
	// change should not silently let a different commit through.
	verify := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	verify.Dir = dstDir
	out, err := verify.Output()
	if err != nil {
		return fmt.Errorf("verify checkout: %w", err)
	}
	got := strings.TrimSpace(string(out))
	if got != sha {
		return fmt.Errorf("checked-out SHA %q != requested %q (server returned a different commit)", got, sha)
	}
	cleanupOnError = false
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
		injectHTTPSTokenEnv(cmd, url, httpsToken)
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
// extraHeader config to the requesting origin only AND to validate
// that fork-PR clone URLs target github.com (v0.19 R1-H2).
//
// v019-R2-H2 fix: previously this stripped ALL `@` from the rest
// string then took the substring up to the next `/`. That trivially
// allowed `https://evil.test/a@github.com/repo.git` to be reported
// as host=`github.com` because the `@` in the PATH was treated as a
// userinfo separator. Use net/url.Parse so the @ in the path stays
// in the path and the real authority is what's compared.
func extractHost(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	// url.Parse accepts schemeless inputs (e.g. "github.com/foo") with
	// Host=="" — those should not match the github.com guard. We also
	// require an https/http scheme since git's extraHeader scope and
	// the fork-PR validator both target HTTP(S) origins.
	switch parsed.Scheme {
	case "http", "https":
		// Host can include a port (`github.com:443`); strip via Hostname().
		return parsed.Hostname()
	default:
		return ""
	}
}

// Pull performs a git pull in the project directory.
//
// v0.16 R8-M4 fix: callers pass `cleanHTTPSURL` (no embedded token)
// and `httpsToken` separately. The remote is set to the clean URL
// (so `git remote -v` and on-disk config never expose the token),
// and the token is delivered via env var on each pull. This handles
// GitHub App token rotation naturally — the URL stays stable, only
// the env var changes.
//
// Backwards compat: if cleanHTTPSURL is "" the remote isn't touched
// (deploy_key SSH path).
func (g *GitClient) Pull(deployKey, cleanHTTPSURL, httpsToken string, projectID uint, logWriter *LogWriter) error {
	dir := g.ProjectDir(projectID)

	// On the HTTPS path, ensure the remote points at the clean URL.
	// First Pull after a Clone-with-SSH (e.g. user changed auth method)
	// or after a v0.15 install where remotes have embedded tokens —
	// always normalize to clean.
	if cleanHTTPSURL != "" {
		setCmd := exec.Command("git", "remote", "set-url", "origin", cleanHTTPSURL)
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

	if httpsToken != "" {
		injectHTTPSTokenEnv(cmd, cleanHTTPSURL, httpsToken)
	}

	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	logWriter.Write([]byte("$ git pull --ff-only\n"))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
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
