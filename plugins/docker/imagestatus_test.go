package docker

import (
	"context"
	"errors"
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
