package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// allowedGitPollSchemes is the set of URL schemes the poller will invoke
// git against. `file://` and unrecognized schemes are rejected — a file-URL
// poll target would let a user probe the container's own filesystem, and
// unknown schemes bypass the SSRF ladder below.
var allowedGitPollSchemes = map[string]bool{
	"https": true,
	"http":  true, // allowed but noisy; most public hosts redirect to https
	"ssh":   true,
	"git":   true,
}

// Polling cadence constants. The global tick decides how often the scheduler
// wakes up to consider whether any individual project is due; per-project
// intervals are still honoured down to MinPollIntervalSec.
const (
	// Minimum per-project interval. Any value below this is clamped up to
	// avoid hammering remotes (Docker Hub / GitHub rate-limit aggressive
	// polling and would defeat the SingleFlight build dedup anyway).
	MinPollIntervalSec = 60

	// Default per-project interval used when GitPollIntervalSec is zero.
	DefaultPollIntervalSec = 300

	// Global tick — coarse cadence at which the poller wakes to scan all
	// enabled projects. Smaller than MinPollIntervalSec so we never miss
	// a project's due time by more than ~30s.
	pollerTick = 30 * time.Second

	// Per-`git ls-remote` timeout. Slow remotes should not stall the
	// scheduler loop; the project simply gets re-checked on the next tick.
	lsRemoteTimeout = 15 * time.Second
)

// pollerConcurrency caps simultaneous ls-remote calls so N slow/hostile
// remotes can't spawn N subprocesses + N TCP connections per tick. Small
// value is intentional: typical WebCasa deployments manage <50 projects,
// 4 concurrent git ls-remote calls keep resource use bounded while cutting
// worst-case tick latency by ~4x vs serial.
const pollerConcurrency = 4

// Poller periodically polls each git-poll-enabled project's remote for new
// commits and triggers a build when the remote HEAD differs from the last
// deployed commit. Lifecycle is owned by the Service; Stop() blocks until
// the loop has exited cleanly.
//
// Per-tick execution is bounded by a worker pool (pollerConcurrency) so a
// single slow remote does not stall every other project.
type Poller struct {
	db     *gorm.DB
	svc    *Service
	logger *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewPoller wires a Poller around the given Service.
func NewPoller(svc *Service) *Poller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Poller{
		db:     svc.db,
		svc:    svc,
		logger: svc.logger,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

// Start kicks off the polling loop in its own goroutine. Safe to call once.
func (p *Poller) Start() {
	go p.loop()
}

// Stop signals the loop to exit and cancels any in-flight ls-remote calls.
// Blocks until the loop goroutine has returned.
func (p *Poller) Stop() {
	p.cancel()
	<-p.done
}

func (p *Poller) loop() {
	defer close(p.done)

	t := time.NewTicker(pollerTick)
	defer t.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
			p.runOnce()
		}
	}
}

// runOnce scans all enabled projects and polls those whose interval has
// elapsed. Polls run concurrently bounded by pollerConcurrency.
func (p *Poller) runOnce() {
	var projects []Project
	if err := p.db.Where("git_poll_enabled = ?", true).Find(&projects).Error; err != nil {
		p.logger.Error("git poll: load projects failed", "err", err)
		return
	}

	now := time.Now()
	sem := make(chan struct{}, pollerConcurrency)
	var wg sync.WaitGroup

	for i := range projects {
		pr := projects[i] // value copy; pollOne re-reads by ID before deciding
		interval := effectivePollInterval(pr.GitPollIntervalSec)
		if pr.LastPolledAt != nil && now.Sub(*pr.LastPolledAt) < time.Duration(interval)*time.Second {
			continue
		}

		select {
		case <-p.ctx.Done():
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(pr Project) {
			defer wg.Done()
			defer func() { <-sem }()
			p.pollOne(&pr)
		}(pr)
	}

	wg.Wait()
}

// pollOne checks one project's remote HEAD and triggers Build when changed.
// Failures are logged but never propagate — the next tick retries naturally.
//
// After network I/O, the project is re-fetched before the Build trigger so
// a mid-poll config change (user disables polling, rotates URL/branch)
// cannot cause a stale-URL build or a LastPolledAt write for a config the
// user no longer intends.
func (p *Poller) pollOne(pr *Project) {
	now := time.Now()
	// Always update LastPolledAt regardless of outcome so a hard-failing
	// remote does not get hammered every tick.
	defer func() {
		p.db.Model(&Project{}).Where("id = ?", pr.ID).Update("last_polled_at", now)
	}()

	if strings.TrimSpace(pr.GitURL) == "" {
		return
	}

	branch := pr.GitBranch
	if branch == "" {
		branch = "main"
	}

	commit, err := p.lsRemoteHead(p.ctx, pr, branch)
	if err != nil {
		p.logger.Warn("git poll: ls-remote failed", "project_id", pr.ID, "err", err)
		return
	}

	if commit == "" {
		return
	}

	// Re-read project state before comparing / triggering. User may have
	// disabled polling or rotated the remote while the ls-remote above was
	// in flight; using stale data here would either trigger an unwanted
	// build or overwrite LastDeployedCommit for a different remote.
	var fresh Project
	if err := p.db.First(&fresh, pr.ID).Error; err != nil {
		return
	}
	if !fresh.GitPollEnabled {
		return
	}
	if fresh.GitURL != pr.GitURL || fresh.GitBranch != pr.GitBranch {
		p.logger.Info("git poll: config changed during poll — skipping build",
			"project_id", pr.ID)
		return
	}
	if commit == fresh.LastDeployedCommit {
		return
	}

	p.logger.Info("git poll: new commit detected, triggering build",
		"project_id", pr.ID,
		"old", shortSHA(fresh.LastDeployedCommit),
		"new", shortSHA(commit),
	)

	if err := p.svc.Build(pr.ID); err != nil && !errors.Is(err, ErrBuildCoalesced) {
		p.logger.Error("git poll: triggered build failed to enqueue", "project_id", pr.ID, "err", err)
	}
}

// lsRemoteHead resolves the remote SHA for a project's configured branch
// without cloning. Honours the project's auth method (SSH key / GitHub App
// token) by reusing GetGitCredentials and binding the appropriate env.
//
// The caller-supplied `ctx` is used for the subprocess so shutdown /
// per-tick scoping cancels in-flight ls-remote calls; previously this
// function used context.Background() and could stall shutdown by up to
// lsRemoteTimeout per in-flight project.
func (p *Poller) lsRemoteHead(ctx context.Context, pr *Project, branch string) (string, error) {
	authMethod, deployKey, httpsToken, err := p.svc.GetGitCredentials(pr)
	if err != nil {
		return "", err
	}

	target := pr.GitURL
	if (authMethod == "github_app" || authMethod == "github_oauth") && httpsToken != "" {
		target = ConvertToHTTPS(pr.GitURL, httpsToken)
	}

	// SSRF guard: reject loopback / link-local / metadata-endpoint targets
	// before spawning git. Private RFC1918 ranges are allowed so self-hosted
	// Gitea / Gitlab on an internal network keep working.
	if err := validateGitPollTarget(target); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, lsRemoteTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", target, branch)

	// SSH key auth: write to a temp file and tell git to use it.
	if authMethod == "ssh_key" && deployKey != "" {
		envCleanup, err := configureGitSSH(cmd, deployKey)
		if err != nil {
			return "", err
		}
		defer envCleanup()
	}

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Output format: "<sha>\trefs/heads/<branch>\n"
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", nil
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], nil
}

// effectivePollInterval clamps the configured interval up to MinPollIntervalSec
// and falls back to DefaultPollIntervalSec for unset/legacy projects.
func effectivePollInterval(configured int) int {
	if configured <= 0 {
		return DefaultPollIntervalSec
	}
	if configured < MinPollIntervalSec {
		return MinPollIntervalSec
	}
	return configured
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// validateGitPollTarget rejects Git URLs that would let a malicious project
// configuration probe the host's own network. Called from lsRemoteHead
// before invoking git.
//
// Policy:
//   - Scheme must be in allowedGitPollSchemes (https / http / ssh / git).
//     scp-style URLs like `git@host:owner/repo` pass through unparsed;
//     host extraction is best-effort and a parse failure permits the poll
//     (git itself will either resolve safely or fail cleanly).
//   - Literal IP hosts are rejected if loopback / link-local / metadata.
//   - DNS names are resolved once; any answer in the blocked ranges
//     fails closed.
//   - Private RFC1918 ranges are intentionally allowed so self-hosted
//     git servers (internal Gitea/GitLab) keep working.
func validateGitPollTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("git URL is empty")
	}

	// scp-style (`user@host:path`) — no scheme. Attempt host extraction but
	// don't reject on parse failure; defer to git's own error handling.
	var host string
	if strings.Contains(target, "://") {
		u, err := url.Parse(target)
		if err != nil {
			return fmt.Errorf("cannot parse git URL: %w", err)
		}
		if !allowedGitPollSchemes[strings.ToLower(u.Scheme)] {
			return fmt.Errorf("git URL scheme %q not allowed for polling (use https/http/ssh/git)", u.Scheme)
		}
		host = u.Hostname()
	} else if at := strings.IndexByte(target, '@'); at >= 0 {
		if colon := strings.IndexByte(target[at+1:], ':'); colon >= 0 {
			host = target[at+1 : at+1+colon]
		}
	}

	if host == "" {
		// Couldn't extract host (unusual form); let git fail naturally.
		return nil
	}

	// Literal IP case.
	if ip := net.ParseIP(host); ip != nil {
		return checkBlockedPollIP(ip, host)
	}

	// DNS resolution.
	ips, err := net.LookupIP(host)
	if err != nil {
		// Resolution failure itself is not an SSRF — git will report the
		// same error. Let the poll proceed to produce a clean error path.
		return nil
	}
	for _, ip := range ips {
		if err := checkBlockedPollIP(ip, host); err != nil {
			return err
		}
	}
	return nil
}

func checkBlockedPollIP(ip net.IP, host string) error {
	if ip.IsLoopback() {
		return fmt.Errorf("git URL host %q resolves to loopback %s (blocked)", host, ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("git URL host %q resolves to link-local %s (blocked)", host, ip)
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 169 && ip4[1] == 254 {
		return fmt.Errorf("git URL host %q resolves to metadata endpoint %s (blocked)", host, ip)
	}
	return nil
}

// configureGitSSH writes the deploy key to a private temp file and points
// the supplied cmd at it via GIT_SSH_COMMAND. Returns a cleanup func that
// removes the temp dir; callers must defer it.
//
// Host-key policy mirrors the build path (plugins/deploy/git.go):
// StrictHostKeyChecking=accept-new + UserKnownHostsFile=~/.ssh/known_hosts.
// This trusts a new host on first connection but rejects any subsequent
// host-key change, defeating MITM on known remotes. The prior poller
// implementation used StrictHostKeyChecking=no + /dev/null known_hosts,
// which accepted any host key — an avoidable MITM window for every poll.
func configureGitSSH(cmd *exec.Cmd, deployKey string) (func(), error) {
	dir, err := os.MkdirTemp("", "webcasa-poll-*")
	if err != nil {
		return func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	keyPath := filepath.Join(dir, "id_rsa")
	if err := os.WriteFile(keyPath, []byte(deployKey), 0600); err != nil {
		cleanup()
		return func() {}, err
	}

	gitSSH := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=~/.ssh/known_hosts -o IdentitiesOnly=yes", keyPath)
	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+gitSSH)
	return cleanup, nil
}
