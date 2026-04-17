package docker

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
)

// ImageStatus values reported on ContainerInfo.
const (
	ImageStatusUpdated  = "updated"  // container runs the same SHA as the current local tag
	ImageStatusOutdated = "outdated" // a newer local image exists for this tag (pull after container create)
	ImageStatusUnknown  = "unknown"  // could not determine (untagged image, pinned by digest, inspect error)
)

// imageStatusCache caches tag -> current-local-imageID resolution for a short
// window so a single ListContainers call doesn't re-query Docker once per
// container. Entries live cacheTTL; after expiry a fresh ImageInspect is
// performed on demand (not eagerly refreshed).
type imageStatusCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	imageID   string
	expiresAt time.Time
}

// cacheTTL is short on purpose: users running `docker pull` expect the new
// state to reflect within seconds. The cache exists only to collapse N
// containers sharing one image reference into a single inspect per list call.
const cacheTTL = 5 * time.Second

var defaultImageStatusCache = &imageStatusCache{
	entries: make(map[string]cacheEntry),
}

// imageInspector is the minimal Docker SDK surface resolveTagImageID needs.
// Declared as an interface so tests can substitute a fake without spinning
// up a real Docker daemon.
type imageInspector interface {
	ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
}

// resolveTagImageID returns the current local SHA for the given image
// reference (e.g. "nginx:latest", "ghcr.io/org/app:v2"). Cache hits are
// free; misses trigger a single ImageInspect. Returns "" when the tag is
// not present locally or inspect fails.
func (c *imageStatusCache) resolveTagImageID(ctx context.Context, cli imageInspector, ref string) string {
	if ref == "" {
		return ""
	}

	now := time.Now()
	c.mu.RLock()
	ent, ok := c.entries[ref]
	c.mu.RUnlock()
	if ok && now.Before(ent.expiresAt) {
		return ent.imageID
	}

	// Cache miss or expired. Inspect and fill.
	id := ""
	info, _, err := cli.ImageInspectWithRaw(ctx, ref)
	if err == nil {
		id = info.ID
	}

	c.mu.Lock()
	c.entries[ref] = cacheEntry{imageID: id, expiresAt: now.Add(cacheTTL)}
	c.mu.Unlock()

	return id
}

// Invalidate clears all cached entries. Call after a local ImagePull or
// ImageRemove so subsequent status queries reflect the new state immediately
// instead of waiting for cacheTTL expiry.
func (c *imageStatusCache) Invalidate() {
	c.mu.Lock()
	c.entries = make(map[string]cacheEntry)
	c.mu.Unlock()
}

// AnnotateImageStatuses fills in ImageStatus on each container by comparing
// its ImageID against the current local SHA for its tag reference. Unique
// image refs are resolved once per call (shared across containers that run
// the same image); results live in a short-lived cache so successive list
// requests don't repeat inspect work.
//
// Must not error: if resolution fails for any reason, the corresponding
// container's ImageStatus stays "unknown" and the list response is still
// served.
func (c *Client) AnnotateImageStatuses(ctx context.Context, containers []ContainerInfo) {
	if len(containers) == 0 {
		return
	}

	// Collapse repeated image refs to one resolve per unique tag.
	uniq := make(map[string]string, len(containers))
	for _, ctr := range containers {
		if _, seen := uniq[ctr.Image]; !seen {
			uniq[ctr.Image] = ""
		}
	}
	for ref := range uniq {
		uniq[ref] = defaultImageStatusCache.resolveTagImageID(ctx, c.cli, ref)
	}

	for i := range containers {
		containers[i].ImageStatus = compareImageStatus(
			containers[i].Image,
			containers[i].ImageID,
			uniq[containers[i].Image],
		)
	}
}

// invalidateImageStatusCache is called after an ImagePull / ImageRemove so
// the next list request reflects the new local state immediately.
func (c *Client) invalidateImageStatusCache() {
	defaultImageStatusCache.Invalidate()
}

// compareImageStatus returns "updated" / "outdated" / "unknown" for a single
// container. containerImageID is the SHA the container actually runs;
// tagImageID is the current local SHA for the same reference.
//
// Heuristic:
//   - Empty tag SHA (lookup failed / tag not present) -> unknown.
//   - Container image reference is a digest ("sha256:..." or "image@sha256:...")
//     -> unknown: the container is pinned, "update" is not meaningful.
//   - Same SHA -> updated.
//   - Different SHA -> outdated (newer local pull exists for this tag).
func compareImageStatus(imageRef, containerImageID, tagImageID string) string {
	if tagImageID == "" {
		return ImageStatusUnknown
	}
	if imageRef == "" {
		return ImageStatusUnknown
	}
	// Digest-pinned references never drift.
	if strings.Contains(imageRef, "@sha256:") || strings.HasPrefix(imageRef, "sha256:") {
		return ImageStatusUnknown
	}
	if containerImageID == "" {
		return ImageStatusUnknown
	}
	if containerImageID == tagImageID {
		return ImageStatusUpdated
	}
	return ImageStatusOutdated
}
