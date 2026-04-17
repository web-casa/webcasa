package deploy

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openPollerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Per-test private in-memory DB (no cache=shared) so subtests don't
	// collide on unique indexes like plugin_deploy_projects.webhook_token.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Project{}, &Deployment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newPollerTestService(t *testing.T, db *gorm.DB) *Service {
	t.Helper()
	return &Service{
		db:            db,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		buildInflight: make(map[uint]bool),
		buildPending:  make(map[uint]bool),
	}
}

func TestEffectivePollInterval(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, DefaultPollIntervalSec},
		{-1, DefaultPollIntervalSec},
		{30, MinPollIntervalSec},
		{59, MinPollIntervalSec},
		{60, 60},
		{300, 300},
		{3600, 3600},
	}
	for _, tc := range cases {
		if got := effectivePollInterval(tc.in); got != tc.want {
			t.Errorf("effectivePollInterval(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestValidateGitPollTarget is a regression test for the Codex-flagged
// HIGH finding: the poller used to invoke `git ls-remote` against any
// user-supplied URL, enabling SSRF-style probes of the host's own network.
// This test locks in the scheme allowlist and IP blocklist.
func TestValidateGitPollTarget(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Happy paths — public git hosts and private RFC1918 (internal Gitea).
		{"https github", "https://github.com/user/repo.git", false},
		{"ssh scheme github", "ssh://git@github.com/user/repo.git", false},
		{"scp style", "git@github.com:user/repo.git", false},
		{"internal gitea rfc1918", "https://10.0.0.5/user/repo.git", false},

		// Blocked scheme.
		{"file scheme", "file:///etc/passwd", true},
		{"ftp scheme", "ftp://example.com/repo", true},

		// Blocked IPs — loopback, link-local, metadata endpoint.
		{"loopback v4", "https://127.0.0.1/repo.git", true},
		{"loopback v6", "https://[::1]/repo.git", true},
		{"link-local v4", "https://169.254.0.1/repo.git", true},
		{"metadata endpoint", "http://169.254.169.254/latest/meta-data/", true},

		// Empty URL rejected.
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGitPollTarget(tc.url)
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestShortSHA(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"abcdefgh", "abcdefgh"},
		{"abcdefghijklmn", "abcdefgh"},
	}
	for _, tc := range cases {
		if got := shortSHA(tc.in); got != tc.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestPoller_TriggersBuildOnNewCommit drives pollOne end-to-end via an
// injected lsRemoteFn stub and asserts Build is invoked once when the
// remote commit differs from LastDeployedCommit.
func TestPoller_TriggersBuildOnNewCommit(t *testing.T) {
	db := openPollerTestDB(t)
	svc := newPollerTestService(t, db)

	var buildCalls atomic.Int32
	svc.buildRunner = func(projectID uint) error {
		buildCalls.Add(1)
		return nil
	}

	pr := Project{
		Name:               "test",
		GitURL:             "https://example.invalid/repo.git",
		GitBranch:          "main",
		GitPollEnabled:     true,
		LastDeployedCommit: "oldsha",
	}
	if err := db.Create(&pr).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	poller := NewPoller(svc)
	poller.lsRemoteFn = func(_ context.Context, _ *Project, _ string) (string, error) {
		return "newsha", nil
	}

	// Drive the real pollOne path — this exercises the re-read guard,
	// inflight check, and Build() call.
	poller.pollOne(&pr)

	// Wait for the buildLoop goroutine to consume the queued work.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if buildCalls.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if buildCalls.Load() != 1 {
		t.Fatalf("expected 1 build, got %d", buildCalls.Load())
	}

	waitInflightClear(t, svc, pr.ID, 2*time.Second)
}

// TestPoller_ConfigChangedDuringPoll_SkipsBuild drives pollOne end-to-end
// with an lsRemoteFn stub that simulates ~lengthy network I/O. During that
// I/O the test flips GitPollEnabled to false, simulating a concurrent
// UpdateProject. pollOne's re-read guard must detect the config change
// and skip the Build() trigger. Regression test for the Codex-flagged
// HIGH TOCTOU finding.
func TestPoller_ConfigChangedDuringPoll_SkipsBuild(t *testing.T) {
	db := openPollerTestDB(t)
	svc := newPollerTestService(t, db)

	var buildCalls atomic.Int32
	svc.buildRunner = func(projectID uint) error {
		buildCalls.Add(1)
		return nil
	}

	pr := Project{
		Name:               "drift",
		GitURL:             "https://example.invalid/repo.git",
		GitBranch:          "main",
		GitPollEnabled:     true,
		LastDeployedCommit: "oldsha",
	}
	if err := db.Create(&pr).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	poller := NewPoller(svc)
	poller.lsRemoteFn = func(_ context.Context, _ *Project, _ string) (string, error) {
		// Simulate concurrent config change during the "network" call.
		db.Model(&Project{}).Where("id = ?", pr.ID).Update("git_poll_enabled", false)
		return "newsha", nil
	}

	poller.pollOne(&pr)

	// Wait briefly for any spurious build goroutine.
	time.Sleep(100 * time.Millisecond)
	if buildCalls.Load() != 0 {
		t.Fatalf("build should NOT have been triggered after concurrent GitPollEnabled flip; got %d calls", buildCalls.Load())
	}
}

// TestPoller_SameSHAInFlight_NoDoubleBuild is a regression test for the
// final-sweep HIGH finding: a poller observing a commit SHA that matches
// what's currently being built by a webhook/manual trigger must NOT queue
// a buildPending run. Without the IsBuildInflight guard the pre-fix code
// would turn one user push into two identical builds.
func TestPoller_SameSHAInFlight_NoDoubleBuild(t *testing.T) {
	db := openPollerTestDB(t)
	svc := newPollerTestService(t, db)

	var buildCalls atomic.Int32
	release := make(chan struct{})
	svc.buildRunner = func(projectID uint) error {
		buildCalls.Add(1)
		<-release
		return nil
	}

	pr := Project{
		Name:               "same-sha",
		GitURL:             "https://example.invalid/repo.git",
		GitBranch:          "main",
		GitPollEnabled:     true,
		LastDeployedCommit: "oldsha",
	}
	if err := db.Create(&pr).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate webhook-triggered build in flight.
	if err := svc.Build(pr.ID); err != nil {
		t.Fatalf("seed Build: %v", err)
	}

	// Poller fires during the in-flight build and observes the same SHA.
	poller := NewPoller(svc)
	poller.lsRemoteFn = func(_ context.Context, _ *Project, _ string) (string, error) {
		return "pending-build-sha", nil
	}
	poller.pollOne(&pr)

	// Release the first build so buildLoop finishes.
	release <- struct{}{}

	// Drain the build goroutine. If the regression is back, a second
	// runBuildOnce fires here (stranded buildPending) and buildCalls == 2.
	waitInflightClear(t, svc, pr.ID, 2*time.Second)

	if got := buildCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 build (poller should have deferred to in-flight), got %d", got)
	}
}

// TestPoller_RespectsInterval verifies the scheduler skips a project whose
// interval has not elapsed since the last poll.
func TestPoller_RespectsInterval(t *testing.T) {
	db := openPollerTestDB(t)
	svc := newPollerTestService(t, db)
	svc.buildRunner = func(uint) error { return nil }

	last := time.Now()
	pr := Project{
		Name:               "recent",
		GitURL:             "https://example.invalid/repo.git",
		GitBranch:          "main",
		GitPollEnabled:     true,
		GitPollIntervalSec: 600,
		LastPolledAt:       &last,
	}
	if err := db.Create(&pr).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	poller := NewPoller(svc)

	// Simulate a scheduler tick directly — no real network calls because
	// the interval gate should short-circuit before lsRemoteHead is invoked.
	var called atomic.Bool
	var mu sync.Mutex
	// Count poller decisions by inspecting DB updates to last_polled_at:
	// if the gate works, LastPolledAt stays equal to its original value.
	poller.runOnce()

	var after Project
	if err := db.First(&after, pr.ID).Error; err != nil {
		t.Fatalf("refetch: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if after.LastPolledAt == nil || !after.LastPolledAt.Equal(last) {
		called.Store(true)
	}
	if called.Load() {
		t.Errorf("poller should have skipped project whose interval has not elapsed (last=%s, interval=600s)", last)
	}
}
