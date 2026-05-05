package deploy

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestService returns a minimal Service sufficient for exercising Build()
// dedup semantics. DB and downstream fields are left nil; the test must inject
// a buildRunner so runBuildOnce is never reached.
//
// buildSem is sized at 64 (the production cap) so dedup-concurrency tests
// can exercise multi-project parallelism without hitting the v0.16
// queue-depth limit. Tests that specifically want to verify queue-full
// behaviour should construct a Service directly with a smaller channel.
func newTestService() *Service {
	return &Service{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		buildInflight: make(map[uint]bool),
		buildPending:  make(map[uint]bool),
		buildSem:      make(chan struct{}, 64),
	}
}

// TestBuild_NeverBlocks verifies Build() returns immediately even when a
// long-running build is in flight. Fix for the pre-v0.11 5-minute blocking bug.
func TestBuild_NeverBlocks(t *testing.T) {
	s := newTestService()

	release := make(chan struct{})
	s.buildRunner = func(projectID uint) error {
		<-release
		return nil
	}

	// First call kicks off the long build.
	if err := s.Build(1); err != nil {
		t.Fatalf("first Build returned %v, want nil", err)
	}

	// Subsequent calls must return ErrBuildCoalesced within a few ms.
	for i := 0; i < 5; i++ {
		done := make(chan error, 1)
		go func() { done <- s.Build(1) }()

		select {
		case err := <-done:
			if !errors.Is(err, ErrBuildCoalesced) {
				t.Fatalf("concurrent Build #%d returned %v, want ErrBuildCoalesced", i, err)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("Build() blocked on iteration %d (should be non-blocking)", i)
		}
	}

	close(release)

	// Wait for the goroutine to drain and clear inflight.
	waitInflightClear(t, s, 1, 2*time.Second)
}

// TestBuild_PendingTriggersRerun verifies that a single coalesced request
// causes exactly one additional build after the current one finishes,
// ensuring the latest code is always eventually built.
func TestBuild_PendingTriggersRerun(t *testing.T) {
	s := newTestService()

	var runs atomic.Int32
	starts := make(chan struct{}, 10)
	release := make(chan struct{})

	s.buildRunner = func(projectID uint) error {
		runs.Add(1)
		starts <- struct{}{}
		<-release
		return nil
	}

	if err := s.Build(1); err != nil {
		t.Fatalf("first Build: %v", err)
	}

	// Wait for first build to start.
	select {
	case <-starts:
	case <-time.After(1 * time.Second):
		t.Fatal("first build never started")
	}

	// While first runs, enqueue two coalesced requests (should cause exactly one rerun).
	if err := s.Build(1); !errors.Is(err, ErrBuildCoalesced) {
		t.Fatalf("second Build: want ErrBuildCoalesced, got %v", err)
	}
	if err := s.Build(1); !errors.Is(err, ErrBuildCoalesced) {
		t.Fatalf("third Build: want ErrBuildCoalesced, got %v", err)
	}

	// Release first build; the pending flag should trigger one more run.
	release <- struct{}{}

	select {
	case <-starts:
	case <-time.After(1 * time.Second):
		t.Fatal("pending rerun never started")
	}
	release <- struct{}{}

	// Now loop should drain. No third run (only one pending slot).
	waitInflightClear(t, s, 1, 2*time.Second)

	if got := runs.Load(); got != 2 {
		t.Fatalf("expected 2 runs (1 original + 1 pending), got %d", got)
	}
}

// TestBuild_ParallelProjectsIndependent verifies dedup is per-project:
// concurrent Build() for different project IDs all proceed.
func TestBuild_ParallelProjectsIndependent(t *testing.T) {
	s := newTestService()

	var runs atomic.Int32
	release := make(chan struct{})
	s.buildRunner = func(projectID uint) error {
		runs.Add(1)
		<-release
		return nil
	}

	var wg sync.WaitGroup
	for id := uint(1); id <= 5; id++ {
		wg.Add(1)
		go func(id uint) {
			defer wg.Done()
			if err := s.Build(id); err != nil {
				t.Errorf("Build(%d) = %v, want nil", id, err)
			}
		}(id)
	}
	wg.Wait()

	// All 5 builds should be in flight simultaneously.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if runs.Load() == 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := runs.Load(); got != 5 {
		t.Fatalf("expected 5 concurrent runs, got %d", got)
	}

	close(release)

	for id := uint(1); id <= 5; id++ {
		waitInflightClear(t, s, id, 2*time.Second)
	}
}

// TestBuild_QueueFull_RejectsExcess verifies the v0.16 panel-wide
// concurrency cap. With buildSem sized at 2, the third concurrent
// build for a different project must return ErrBuildQueueFull
// immediately (no in-memory queue grows). After one of the held
// builds releases its slot, a fresh Build() succeeds.
func TestBuild_QueueFull_RejectsExcess(t *testing.T) {
	s := &Service{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		buildInflight: make(map[uint]bool),
		buildPending:  make(map[uint]bool),
		buildSem:      make(chan struct{}, 2),
	}

	release := make(chan struct{})
	s.buildRunner = func(projectID uint) error {
		<-release
		return nil
	}

	// Fill the slots.
	if err := s.Build(1); err != nil {
		t.Fatalf("Build(1) = %v, want nil", err)
	}
	if err := s.Build(2); err != nil {
		t.Fatalf("Build(2) = %v, want nil", err)
	}

	// Wait until both builds are actually inflight (semaphore acquired).
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		s.buildMu.Lock()
		n := len(s.buildInflight)
		s.buildMu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Third project: should reject.
	if err := s.Build(3); !errors.Is(err, ErrBuildQueueFull) {
		t.Fatalf("Build(3) at capacity = %v, want ErrBuildQueueFull", err)
	}

	// Release one slot — either goroutine could receive first.
	// Wait until inflight count drops to 1 instead of asserting which.
	release <- struct{}{}
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		s.buildMu.Lock()
		n := len(s.buildInflight)
		s.buildMu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now a fresh Build should succeed (slot freed).
	if err := s.Build(4); err != nil {
		t.Fatalf("Build(4) after one slot freed = %v, want nil", err)
	}

	close(release)
	// Drain remaining slots without caring about order.
	for time.Now().Before(time.Now().Add(2 * time.Second)) {
		s.buildMu.Lock()
		n := len(s.buildInflight)
		s.buildMu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestBuild_ExitRace_PendingNotLost is a regression test for the Codex-flagged
// HIGH finding: buildLoop used to clear buildInflight via defer AFTER
// releasing the lock around the pending check, leaving a window where a
// concurrent Build() could set buildPending=true and get ErrBuildCoalesced
// while the loop had already decided to exit. The pending flag would then
// sit stranded until some later Build() happened to wake it up.
//
// The fix moves the inflight clear under the same lock as the pending check
// (no defer). This test drives the loop through many tight start/finish
// cycles under parallel pressure and asserts buildPending stays empty after
// the system quiesces.
func TestBuild_ExitRace_PendingNotLost(t *testing.T) {
	s := newTestService()

	var runs atomic.Int32
	s.buildRunner = func(projectID uint) error {
		runs.Add(1)
		// Tiny sleep makes the check-vs-clear race window observable. Keep
		// short so the test finishes quickly even under -race.
		time.Sleep(1 * time.Millisecond)
		return nil
	}

	// Drive many cycles; each iteration kicks off a short burst of
	// concurrent Build() calls, waits for the system to quiesce, then
	// checks the pending flag. With the pre-fix code this reliably strands
	// pending=true after a few iterations on -race builds.
	for cycle := 0; cycle < 50; cycle++ {
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = s.Build(1)
			}()
		}
		wg.Wait()

		waitInflightClear(t, s, 1, 2*time.Second)

		s.buildMu.Lock()
		leaked := s.buildPending[1]
		s.buildMu.Unlock()
		if leaked {
			t.Fatalf("regression at cycle %d: buildPending[1]=true after buildInflight cleared — coalesced request stranded", cycle)
		}
	}
}

func waitInflightClear(t *testing.T, s *Service, projectID uint, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.buildMu.Lock()
		inflight := s.buildInflight[projectID]
		s.buildMu.Unlock()
		if !inflight {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("buildInflight[%d] never cleared within %v", projectID, timeout)
}
