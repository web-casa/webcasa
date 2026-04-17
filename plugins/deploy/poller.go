package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

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

// Poller periodically polls each git-poll-enabled project's remote for new
// commits and triggers a build when the remote HEAD differs from the last
// deployed commit. Lifecycle is owned by the Service; Stop() blocks until
// the loop has exited cleanly.
type Poller struct {
	db     *gorm.DB
	svc    *Service
	logger *slog.Logger

	stop chan struct{}
	done chan struct{}
}

// NewPoller wires a Poller around the given Service.
func NewPoller(svc *Service) *Poller {
	return &Poller{
		db:     svc.db,
		svc:    svc,
		logger: svc.logger,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Start kicks off the polling loop in its own goroutine. Safe to call once.
func (p *Poller) Start() {
	go p.loop()
}

// Stop signals the loop to exit and blocks until it does.
func (p *Poller) Stop() {
	close(p.stop)
	<-p.done
}

func (p *Poller) loop() {
	defer close(p.done)

	t := time.NewTicker(pollerTick)
	defer t.Stop()

	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.runOnce()
		}
	}
}

// runOnce scans all enabled projects and polls those whose interval has elapsed.
func (p *Poller) runOnce() {
	var projects []Project
	if err := p.db.Where("git_poll_enabled = ?", true).Find(&projects).Error; err != nil {
		p.logger.Error("git poll: load projects failed", "err", err)
		return
	}

	now := time.Now()
	for i := range projects {
		pr := &projects[i]
		interval := effectivePollInterval(pr.GitPollIntervalSec)
		if pr.LastPolledAt != nil && now.Sub(*pr.LastPolledAt) < time.Duration(interval)*time.Second {
			continue
		}
		p.pollOne(pr)
	}
}

// pollOne checks one project's remote HEAD and triggers Build when changed.
// Failures are logged but never propagate — the next tick retries naturally.
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

	commit, err := p.lsRemoteHead(pr, branch)
	if err != nil {
		p.logger.Warn("git poll: ls-remote failed", "project_id", pr.ID, "err", err)
		return
	}

	if commit == "" || commit == pr.LastDeployedCommit {
		return
	}

	p.logger.Info("git poll: new commit detected, triggering build",
		"project_id", pr.ID,
		"old", shortSHA(pr.LastDeployedCommit),
		"new", shortSHA(commit),
	)

	if err := p.svc.Build(pr.ID); err != nil && !errors.Is(err, ErrBuildCoalesced) {
		p.logger.Error("git poll: triggered build failed to enqueue", "project_id", pr.ID, "err", err)
	}
}

// lsRemoteHead resolves the remote SHA for a project's configured branch
// without cloning. Honours the project's auth method (SSH key / GitHub App
// token) by reusing GetGitCredentials and binding the appropriate env.
func (p *Poller) lsRemoteHead(pr *Project, branch string) (string, error) {
	authMethod, deployKey, httpsToken, err := p.svc.GetGitCredentials(pr)
	if err != nil {
		return "", err
	}

	url := pr.GitURL
	if (authMethod == "github_app" || authMethod == "github_oauth") && httpsToken != "" {
		url = ConvertToHTTPS(pr.GitURL, httpsToken)
	}

	ctx, cancel := context.WithTimeout(context.Background(), lsRemoteTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", url, branch)

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

// configureGitSSH writes the deploy key to a private temp file and points
// the supplied cmd at it via GIT_SSH_COMMAND. Returns a cleanup func that
// removes the temp file; callers must defer it.
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

	gitSSH := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o IdentitiesOnly=yes", keyPath)
	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+gitSSH)
	return cleanup, nil
}
