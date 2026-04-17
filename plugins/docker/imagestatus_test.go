package docker

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
)

func TestCompareImageStatus(t *testing.T) {
	cases := []struct {
		name             string
		imageRef         string
		containerImageID string
		tagImageID       string
		want             string
	}{
		{"tag resolve failed", "nginx:latest", "sha256:aaa", "", ImageStatusUnknown},
		{"digest pinned", "nginx@sha256:deadbeef", "sha256:aaa", "sha256:aaa", ImageStatusUnknown},
		{"digest prefix ref", "sha256:cafe", "sha256:cafe", "sha256:cafe", ImageStatusUnknown},
		{"empty container id", "nginx:latest", "", "sha256:aaa", ImageStatusUnknown},
		{"empty image ref", "", "sha256:aaa", "sha256:aaa", ImageStatusUnknown},
		{"matching sha", "nginx:latest", "sha256:abc", "sha256:abc", ImageStatusUpdated},
		{"newer local pull", "nginx:latest", "sha256:old", "sha256:new", ImageStatusOutdated},
	}
	for _, tc := range cases {
		got := compareImageStatus(tc.imageRef, tc.containerImageID, tc.tagImageID)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// fakeInspector stands in for the Docker SDK client in cache tests.
type fakeInspector struct {
	calls   atomic.Int32
	results map[string]string
	err     error
}

func (f *fakeInspector) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	f.calls.Add(1)
	if f.err != nil {
		return types.ImageInspect{}, nil, f.err
	}
	id, ok := f.results[imageID]
	if !ok {
		return types.ImageInspect{}, nil, errors.New("not found")
	}
	return types.ImageInspect{ID: id}, nil, nil
}

func TestImageStatusCache_DeduplicatesPerTTL(t *testing.T) {
	cache := &imageStatusCache{entries: make(map[string]cacheEntry)}
	fake := &fakeInspector{results: map[string]string{"nginx:latest": "sha256:abc"}}

	for i := 0; i < 5; i++ {
		got := cache.resolveTagImageID(context.Background(), fake, "nginx:latest")
		if got != "sha256:abc" {
			t.Fatalf("iter %d: got %q", i, got)
		}
	}
	if n := fake.calls.Load(); n != 1 {
		t.Errorf("expected 1 inspect call over 5 resolves within TTL, got %d", n)
	}
}

func TestImageStatusCache_MissCachesNegative(t *testing.T) {
	cache := &imageStatusCache{entries: make(map[string]cacheEntry)}
	fake := &fakeInspector{err: errors.New("not found")}

	// Two calls in quick succession; the second must NOT re-inspect.
	_ = cache.resolveTagImageID(context.Background(), fake, "missing:tag")
	_ = cache.resolveTagImageID(context.Background(), fake, "missing:tag")

	if n := fake.calls.Load(); n != 1 {
		t.Errorf("expected 1 inspect call (negative result should be cached), got %d", n)
	}
}

func TestImageStatusCache_Invalidate(t *testing.T) {
	cache := &imageStatusCache{entries: make(map[string]cacheEntry)}
	fake := &fakeInspector{results: map[string]string{"nginx:latest": "sha256:abc"}}

	cache.resolveTagImageID(context.Background(), fake, "nginx:latest")
	cache.Invalidate()

	// After invalidate, next call must re-inspect.
	fake.results["nginx:latest"] = "sha256:new"
	got := cache.resolveTagImageID(context.Background(), fake, "nginx:latest")
	if got != "sha256:new" {
		t.Errorf("after invalidate: got %q, want sha256:new", got)
	}
	if n := fake.calls.Load(); n != 2 {
		t.Errorf("expected 2 inspect calls after invalidate, got %d", n)
	}
}

func TestImageStatusCache_TTLExpiry(t *testing.T) {
	cache := &imageStatusCache{entries: make(map[string]cacheEntry)}
	fake := &fakeInspector{results: map[string]string{"nginx:latest": "sha256:abc"}}

	// Prime with a forcibly-expired entry.
	cache.entries["nginx:latest"] = cacheEntry{
		imageID:   "sha256:stale",
		expiresAt: time.Now().Add(-1 * time.Second),
	}

	got := cache.resolveTagImageID(context.Background(), fake, "nginx:latest")
	if got != "sha256:abc" {
		t.Errorf("expired entry should refresh: got %q", got)
	}
	if n := fake.calls.Load(); n != 1 {
		t.Errorf("expected 1 inspect call after TTL expiry, got %d", n)
	}
}

// blockingInspector lets a test freeze an Inspect call mid-flight so we
// can simulate an Invalidate landing between the inspect start and its
// write-back.
type blockingInspector struct {
	release chan struct{}
	result  string
	err     error
}

func (b *blockingInspector) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	<-b.release // wait for test to release
	if b.err != nil {
		return types.ImageInspect{}, nil, b.err
	}
	return types.ImageInspect{ID: b.result}, nil, nil
}

// TestImageStatusCache_InvalidateDuringResolve_RejectsStaleWrite is a
// regression test for the Codex-flagged MEDIUM finding: an in-flight
// resolveTagImageID could repopulate the cache with a pre-invalidation
// SHA, defeating the PullImage/RemoveImage invalidation.
func TestImageStatusCache_InvalidateDuringResolve_RejectsStaleWrite(t *testing.T) {
	cache := &imageStatusCache{entries: make(map[string]cacheEntry)}
	blocker := &blockingInspector{release: make(chan struct{}), result: "sha256:old"}

	// Start a resolve that will block inside Inspect until we release it.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cache.resolveTagImageID(context.Background(), blocker, "nginx:latest")
	}()

	// Give the goroutine time to enter Inspect and capture gen snapshot.
	time.Sleep(20 * time.Millisecond)

	// Simulate PullImage firing Invalidate while the resolve is still blocked.
	cache.Invalidate()

	// Now let the inspect return with the pre-invalidation result.
	close(blocker.release)
	wg.Wait()

	// The cache MUST NOT contain the stale entry — the resolver should
	// have detected the generation change and skipped its write.
	cache.mu.RLock()
	_, has := cache.entries["nginx:latest"]
	cache.mu.RUnlock()
	if has {
		t.Fatal("regression: resolveTagImageID wrote back a stale entry after Invalidate; post-pull cache will serve the pre-pull SHA for up to cacheTTL")
	}
}

// TestImageStatusCache_ContextErrorNotNegativeCached is a regression test
// for the Codex-flagged LOW finding: context.Canceled / DeadlineExceeded
// must not poison the cache with "" for cacheTTL seconds.
func TestImageStatusCache_ContextErrorNotNegativeCached(t *testing.T) {
	cache := &imageStatusCache{entries: make(map[string]cacheEntry)}
	fakeCancel := &fakeInspector{err: context.DeadlineExceeded}

	got := cache.resolveTagImageID(context.Background(), fakeCancel, "nginx:latest")
	if got != "" {
		t.Errorf("context-error path should return empty string, got %q", got)
	}

	cache.mu.RLock()
	_, has := cache.entries["nginx:latest"]
	cache.mu.RUnlock()
	if has {
		t.Fatal("context.DeadlineExceeded must NOT produce a negative cache entry; next call must be allowed to retry")
	}

	// Follow-up: after the transient error, a successful inspect must
	// populate the cache normally.
	fakeOK := &fakeInspector{results: map[string]string{"nginx:latest": "sha256:abc"}}
	got2 := cache.resolveTagImageID(context.Background(), fakeOK, "nginx:latest")
	if got2 != "sha256:abc" {
		t.Errorf("follow-up inspect after context error: got %q, want sha256:abc", got2)
	}
}

// TestInvalidatingReader_InvalidatesOnClose is a regression test for the
// Codex-flagged MEDIUM finding: image-status cache was invalidated when
// PullImage returned, not when the pull completed. This let a concurrent
// ListContainers cache the pre-pull SHA. The fix wraps the progress reader
// so invalidation fires on Close.
func TestInvalidatingReader_InvalidatesOnClose(t *testing.T) {
	var called atomic.Int32
	rc := &invalidatingReader{
		ReadCloser: io.NopCloser(nil),
		onClose:    func() { called.Add(1) },
	}

	if called.Load() != 0 {
		t.Error("onClose must not fire before Close()")
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if called.Load() != 1 {
		t.Fatalf("onClose should have fired once, got %d", called.Load())
	}

	// Double-close must not double-invalidate (no side effects from extra closes).
	_ = rc.Close()
	if called.Load() != 1 {
		t.Errorf("second Close() must be a no-op, onClose fired %d times", called.Load())
	}
}

// Silence unused import warning; errors used by the stale-write test via fakeInspector.
var _ = errors.Is
