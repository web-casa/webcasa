package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// previewJob tracks an in-flight runPreview goroutine so a second webhook
// (`synchronize` on top of an in-progress build) can cancel the first
// instead of racing it, and `DeletePreview` + plugin `Stop` can wait for
// the goroutine to actually drain before declaring the row safe to remove.
type previewJob struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// PreviewService manages ephemeral preview deployments from GitHub PRs.
//
// Concurrency model (Codex Round-5 C1/C2/H6 fixes):
//   - `jobs` tracks in-flight runPreview goroutines by preview ID.
//     CreatePreview cancels any existing job for the same ID before
//     spawning a new one (webhook deduplication).
//   - `wg` tracks all runPreview goroutines so plugin Stop can wait
//     for drain.
//   - `rootCtx` is the service-level context; cancelling it signals
//     every runPreview goroutine to abort at its next ctx check. Set
//     up in NewPreviewService and cancelled in Stop().
//   - The (project_id, pr_number) unique index on PreviewDeployment
//     prevents duplicate rows at the DB layer even if two webhooks
//     race past the lookup-then-create path in application code.
type PreviewService struct {
	db      *gorm.DB
	svc     *Service
	coreAPI pluginpkg.CoreAPI
	logger  *slog.Logger

	rootCtx    context.Context
	rootCancel context.CancelFunc
	jobsMu     sync.Mutex
	jobs       map[uint]*previewJob
	wg         sync.WaitGroup
}

// NewPreviewService creates a new PreviewService.
func NewPreviewService(db *gorm.DB, svc *Service, coreAPI pluginpkg.CoreAPI, logger *slog.Logger) *PreviewService {
	ctx, cancel := context.WithCancel(context.Background())
	return &PreviewService{
		db:         db,
		svc:        svc,
		coreAPI:    coreAPI,
		logger:     logger,
		rootCtx:    ctx,
		rootCancel: cancel,
		jobs:       make(map[uint]*previewJob),
	}
}

// Stop cancels every in-flight runPreview goroutine and waits for them to
// drain. Called from plugin Stop() so a panel shutdown doesn't leave zombie
// `git clone` or `podman build` children running past SIGTERM.
func (ps *PreviewService) Stop(drainTimeout time.Duration) {
	ps.rootCancel()
	// Wait up to drainTimeout for all goroutines. If they outrun the
	// timeout, exec.CommandContext still kills their subprocesses via
	// the cancel signal — we just return before they finish logging.
	done := make(chan struct{})
	go func() { ps.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(drainTimeout):
		ps.logger.Warn("preview jobs did not drain within timeout", "timeout", drainTimeout)
	}
}

// CreatePreview creates (or re-triggers) a preview deployment for a GitHub PR.
//
// The full pipeline is:
//  1. Preflight — preview feature enabled + wildcard domain configured
//  2. Record — upsert a PreviewDeployment row (unique by project_id + pr_number)
//  3. Build — clone the PR branch into a dedicated source dir + build an image
//  4. Run — start the container on an allocated port (persisted on the row)
//  5. Expose — create/update a Caddy reverse-proxy host
//
// Concurrency: if a runPreview goroutine is already in-flight for this PR
// (fast double `synchronize` webhook is common on force-push), its context
// is cancelled before we spawn the new one. Only one runPreview for a
// given preview ID is ever running.
//
// Runs asynchronously: DB row is returned with status="pending" and a
// goroutine performs steps 3–5.
func (ps *PreviewService) CreatePreview(projectID uint, prNumber int, branch string) (*PreviewDeployment, error) {
	if prNumber <= 0 {
		return nil, fmt.Errorf("pr_number must be positive (got %d)", prNumber)
	}

	project, err := ps.svc.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	if !project.PreviewEnabled {
		return nil, fmt.Errorf("preview deployments are not enabled for this project")
	}
	if project.DeployMode != "docker" {
		return nil, fmt.Errorf("preview deployments require Docker deploy mode; project is in %q mode", project.DeployMode)
	}
	domain, err := ps.coreAPI.GetSetting("wildcard_domain")
	if err != nil || domain == "" {
		return nil, fmt.Errorf("wildcard_domain not configured — set it before enabling preview deploys")
	}

	expiry := project.PreviewExpiry
	if expiry <= 0 {
		expiry = 7
	}
	newExpiry := time.Now().AddDate(0, 0, expiry)

	// Upsert on (project_id, pr_number). The unique index enforces
	// single-row-per-PR; gorm handles the race naturally because Create
	// will fail on conflict and we fall through to the update path.
	var preview PreviewDeployment
	err = ps.db.Where("project_id = ? AND pr_number = ?", projectID, prNumber).First(&preview).Error
	switch {
	case err == nil:
		ps.logger.Info("rebuilding existing preview", "project", project.Name, "pr", prNumber, "branch", branch)
		ps.db.Model(&preview).Updates(map[string]interface{}{
			"branch":         branch,
			"status":         "pending",
			"expires_at":     newExpiry,
			"failure_reason": "",
		})
		preview.Branch = branch
		preview.Status = "pending"
	case err == gorm.ErrRecordNotFound:
		// Phase 1: create row with placeholder domain/names so we have an
		// ID to work with. Phase 2: backfill ID-derived fields. Two-phase
		// keeps names unique even across projects with identical slugs
		// (Codex H1) without needing a pre-ID hash.
		preview = PreviewDeployment{
			ProjectID: projectID,
			PRNumber:  prNumber,
			Branch:    branch,
			Status:    "pending",
			ExpiresAt: newExpiry,
		}
		if err := ps.db.Create(&preview).Error; err != nil {
			return nil, fmt.Errorf("create preview record: %w", err)
		}
		// Derive stable IDs. Domain + container + image tag all include
		// the preview ID so two projects with identical sanitized names
		// don't collide (H1). Port is persisted so `synchronize` reuses
		// the allocation (H5).
		port, err := ps.allocatePort(preview.ID)
		if err != nil {
			ps.db.Delete(&preview)
			return nil, fmt.Errorf("allocate port: %w", err)
		}
		slug := sanitizeForDomain(project.Name)
		previewDomain := fmt.Sprintf("pr-%d-%s-%d.%s", prNumber, slug, preview.ID, domain)
		ps.db.Model(&preview).Updates(map[string]interface{}{
			"domain":         previewDomain,
			"container_name": fmt.Sprintf("webcasa-preview-%d", preview.ID),
			"image_tag":      fmt.Sprintf("webcasa-preview-%d", preview.ID),
			"port":           port,
		})
		preview.Domain = previewDomain
		preview.ContainerName = fmt.Sprintf("webcasa-preview-%d", preview.ID)
		preview.ImageTag = fmt.Sprintf("webcasa-preview-%d", preview.ID)
		preview.Port = port
	default:
		return nil, fmt.Errorf("lookup existing preview: %w", err)
	}

	// Cancel any in-flight runPreview for this ID before spawning a new
	// one. C2 race fix: force-push sends two synchronize webhooks within
	// ms; without this both would fight over the same containerName +
	// srcDir.
	ps.cancelJob(preview.ID)

	jobCtx, jobCancel := context.WithCancel(ps.rootCtx)
	job := &previewJob{cancel: jobCancel, done: make(chan struct{})}
	ps.jobsMu.Lock()
	ps.jobs[preview.ID] = job
	ps.jobsMu.Unlock()

	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer close(job.done)
		defer ps.clearJob(preview.ID, job)
		ps.runPreview(jobCtx, preview.ID, project.ID, branch)
	}()

	return &preview, nil
}

// cancelJob cancels a running runPreview for the given preview ID (no-op
// if no job is running) and waits briefly for it to drain. Used by
// CreatePreview on re-trigger and by DeletePreview before teardown.
func (ps *PreviewService) cancelJob(previewID uint) {
	ps.jobsMu.Lock()
	job, ok := ps.jobs[previewID]
	ps.jobsMu.Unlock()
	if !ok {
		return
	}
	job.cancel()
	select {
	case <-job.done:
	case <-time.After(30 * time.Second):
		ps.logger.Warn("previous preview job did not drain in 30s", "preview_id", previewID)
	}
}

// clearJob removes the job from the registry iff it still matches — so a
// cancel+immediate-restart sequence doesn't drop the NEW job entry.
func (ps *PreviewService) clearJob(previewID uint, job *previewJob) {
	ps.jobsMu.Lock()
	defer ps.jobsMu.Unlock()
	if cur, ok := ps.jobs[previewID]; ok && cur == job {
		delete(ps.jobs, previewID)
	}
}

// allocatePort picks a host port in the [20000, 30000) range that isn't
// already used by another preview. Stored on the row so subsequent
// rebuilds (synchronize) reuse the same port and keep the Caddy upstream
// stable.
//
// Uses a deterministic starting point (20000 + previewID mod 10000) and
// probes forward until a free slot is found. Gives reasonable spread
// without a central atomic counter.
func (ps *PreviewService) allocatePort(previewID uint) (int, error) {
	const base = 20000
	const rng = 10000
	start := base + int(previewID%uint(rng))
	for i := 0; i < rng; i++ {
		candidate := base + ((start - base + i) % rng)
		var count int64
		ps.db.Model(&PreviewDeployment{}).
			Where("port = ? AND id != ?", candidate, previewID).
			Count(&count)
		if count == 0 {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("no free preview port in [%d, %d)", base, base+rng)
}

// runPreview executes the full build-run-expose pipeline asynchronously.
// Called from a goroutine by CreatePreview so the webhook handler can ack
// GitHub quickly. All status transitions + cleanup on failure happen here.
//
// On failure: marks status="failed", tears down any partial resources
// (image / container / host) so a retry (PR `synchronize` event) can
// start fresh. The preview DB row itself is NOT deleted on failure —
// the UI uses it to surface the error to the admin.
func (ps *PreviewService) runPreview(jobCtx context.Context, previewID, projectID uint, branch string) {
	// Per-job deadline on top of the service-wide root context. 15 min is
	// generous for most projects but bounded so a stuck build doesn't hold
	// the job slot forever.
	ctx, cancel := context.WithTimeout(jobCtx, 15*time.Minute)
	defer cancel()

	var preview PreviewDeployment
	if err := ps.db.First(&preview, previewID).Error; err != nil {
		// Row was deleted while we were queued (DeletePreview cancelled
		// but the goroutine raced past the cancel check). Exit clean.
		ps.logger.Info("runPreview: preview row gone, exiting", "id", previewID)
		return
	}
	project, err := ps.svc.GetProject(projectID)
	if err != nil {
		ps.markFailed(previewID, fmt.Sprintf("project lookup: %v", err))
		return
	}
	if !ps.setStatus(previewID, "building", "") {
		return // row deleted between First() and Update()
	}

	// Per-preview log file. `cat $data/logs/preview_<id>/build.log` today;
	// streaming to the UI is a Phase B concern.
	logDir := filepath.Join(ps.svc.dataDir, "logs", fmt.Sprintf("preview_%d", previewID))
	_ = os.MkdirAll(logDir, 0755)
	logWriter, logErr := NewLogWriter(filepath.Join(logDir, "build.log"))
	if logErr != nil {
		ps.logger.Warn("preview log writer failed; proceeding without file log", "err", logErr)
	}

	srcDir := filepath.Join(ps.svc.dataDir, "preview-sources", fmt.Sprintf("preview_%d", previewID))
	if err := os.MkdirAll(filepath.Dir(srcDir), 0755); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("mkdir parent: %v", err))
		return
	}

	// Resolve credentials via the main build path (handles SSH / GitHub
	// App / GitHub OAuth / plain HTTPS).
	_, deployKey, httpsToken, err := ps.svc.GetGitCredentials(project)
	if err != nil {
		ps.markFailed(previewID, fmt.Sprintf("git credentials: %v", err))
		return
	}
	// H3 fix: pass the clean URL + token separately. CloneToDir injects
	// the token via `-c http.extraHeader` instead of baking it into argv,
	// so the token doesn't leak to `ps` / process listings.
	if err := ps.svc.git.CloneToDir(ctx, project.GitURL, branch, deployKey, httpsToken, srcDir, logWriter); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("git clone: %v", err))
		return
	}
	if ctx.Err() != nil {
		return // cancelled mid-pipeline
	}

	imageTag := preview.ImageTag
	if err := ps.svc.docker.BuildImageWithTag(ctx, srcDir, imageTag, logWriter, project.BuildType); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("build: %v", err))
		return
	}
	if ctx.Err() != nil {
		return
	}

	// Re-read the row in case a concurrent DeletePreview fired while we
	// were building. If the row is gone, abort before creating any
	// external resources (C1 fix).
	if err := ps.db.First(&preview, previewID).Error; err != nil {
		ps.logger.Info("runPreview: row deleted during build; aborting before host creation", "id", previewID)
		// Clean up whatever we built so it doesn't linger.
		ps.svc.docker.StopAndRemove(preview.ContainerName)
		removeImage(ctx, imageTag)
		return
	}

	envVars := project.EnvVarList
	envVars = append(envVars,
		EnvVar{Key: "WEBCASA_PREVIEW", Value: "1"},
		EnvVar{Key: "WEBCASA_PREVIEW_PR", Value: fmt.Sprintf("%d", preview.PRNumber)},
		EnvVar{Key: "WEBCASA_PREVIEW_BRANCH", Value: branch},
	)

	// H2 fix: staging-then-swap. Start the new container under a staging
	// name (new port) + update the Caddy upstream FIRST, then tear down
	// the old container. If the new container fails to start, the old
	// one keeps serving traffic — failed synchronize doesn't break a
	// previously-working preview.
	stagingName := preview.ContainerName + "-staging"
	stagingPort := preview.Port + 10000 // separate port during swap
	ps.svc.docker.StopAndRemove(stagingName)
	if _, err := ps.svc.docker.RunWithName(ctx, stagingName, imageTag, stagingPort, envVars); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("run staging container: %v", err))
		return
	}
	// Give the container a few seconds to bind its port before swapping
	// traffic. No healthcheck integration yet — Phase B concern.
	select {
	case <-ctx.Done():
		ps.svc.docker.StopAndRemove(stagingName)
		return
	case <-time.After(3 * time.Second):
	}

	// Create the host on first run, or update existing host's upstream to
	// point at the staging port.
	if preview.HostID == 0 {
		hostID, err := ps.coreAPI.CreateHost(pluginpkg.CreateHostRequest{
			Domain:       preview.Domain,
			HostType:     "proxy",
			UpstreamAddr: fmt.Sprintf("localhost:%d", stagingPort),
			TLSEnabled:   true,
			HTTPRedirect: true,
			WebSocket:    true,
			Compression:  true,
		})
		if err != nil {
			ps.svc.docker.StopAndRemove(stagingName)
			ps.markFailed(previewID, fmt.Sprintf("create host: %v", err))
			return
		}
		preview.HostID = hostID
		ps.db.Model(&preview).Update("host_id", hostID)
	} else {
		if err := ps.coreAPI.UpdateHostUpstream(preview.HostID, fmt.Sprintf("localhost:%d", stagingPort)); err != nil {
			ps.svc.docker.StopAndRemove(stagingName)
			ps.markFailed(previewID, fmt.Sprintf("update host upstream: %v", err))
			return
		}
	}

	// Traffic now points at staging. Tear down the old container + rename
	// staging → canonical.
	ps.svc.docker.StopAndRemove(preview.ContainerName)
	if err := ps.renameContainer(ctx, stagingName, preview.ContainerName); err != nil {
		// Rename failed — point Caddy back to the staging name via its
		// port (it's still running). Record the state as running but
		// with a diagnostic.
		ps.logger.Warn("rename staging → canonical failed; keeping staging port in Caddy",
			"preview_id", previewID, "err", err)
	} else {
		// Swap Caddy back to the canonical port now that the container
		// has its canonical name.
		_ = ps.coreAPI.UpdateHostUpstream(preview.HostID, fmt.Sprintf("localhost:%d", preview.Port))
	}

	ps.db.Model(&preview).Updates(map[string]interface{}{
		"status":         "running",
		"failure_reason": "",
	})
	ps.logger.Info("preview deployment running",
		"project", project.Name, "pr", preview.PRNumber,
		"domain", preview.Domain, "port", preview.Port)
}

// setStatus updates the preview row's status atomically and returns false
// when the row has been deleted (RowsAffected == 0), so callers can abort
// without creating external resources. C1 fix.
func (ps *PreviewService) setStatus(previewID uint, status, failureReason string) bool {
	updates := map[string]interface{}{"status": status}
	if failureReason != "" {
		updates["failure_reason"] = failureReason
	}
	res := ps.db.Model(&PreviewDeployment{}).Where("id = ?", previewID).Updates(updates)
	return res.RowsAffected > 0
}

// renameContainer renames a container via podman. Uses the same CLI the
// runner already relies on. Returns nil if the target already has the
// canonical name (idempotent).
func (ps *PreviewService) renameContainer(ctx context.Context, oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	return exec.CommandContext(ctx, "docker", "rename", oldName, newName).Run()
}

// removeImage force-removes a previously built image. Used during cleanup
// on failure/delete so images don't accumulate.
func removeImage(ctx context.Context, imageTag string) {
	_ = exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag).Run()
}

// markFailed records the reason on the row and flips status to "failed".
// Persists `failure_reason` alongside `status` so the UI can show what
// went wrong without grepping logs.
func (ps *PreviewService) markFailed(previewID uint, reason string) {
	ps.logger.Error("preview deployment failed", "id", previewID, "reason", reason)
	ps.db.Model(&PreviewDeployment{}).Where("id = ?", previewID).Updates(map[string]interface{}{
		"status":         "failed",
		"failure_reason": truncate(reason, 500),
	})
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// DeletePreview removes a preview deployment and cleans up all resources:
// Caddy host, container (+ staging), image, source dir, log dir, and row.
//
// Order matters:
//  1. Cancel any in-flight runPreview goroutine (so it doesn't race us on
//     container create / host create after we've torn it all down — C1)
//  2. Remove external resources (host, containers, image, dirs)
//  3. Delete DB row IFF all external cleanups succeeded. Partial failures
//     leave the row in status=cleanup_failed so admins can see + retry
//     manually (Codex M1)
func (ps *PreviewService) DeletePreview(id uint) error {
	var preview PreviewDeployment
	if err := ps.db.First(&preview, id).Error; err != nil {
		return fmt.Errorf("preview not found: %w", err)
	}

	// 1. Cancel in-flight build (no-op if nothing running).
	ps.cancelJob(id)

	ctx, cancel := context.WithTimeout(ps.rootCtx, 60*time.Second)
	defer cancel()

	var errs []string

	// 2a. Caddy host — remove first so the subdomain stops pointing at a
	// soon-to-be-dead container.
	if preview.HostID > 0 {
		if err := ps.coreAPI.DeleteHost(preview.HostID); err != nil {
			ps.logger.Warn("delete preview host failed", "host_id", preview.HostID, "err", err)
			errs = append(errs, fmt.Sprintf("delete host: %v", err))
		}
	}

	// 2b. Containers: both canonical + any leftover staging from a half-
	// done swap.
	if preview.ContainerName != "" {
		if err := ps.coreAPI.DockerRemoveContainer(preview.ContainerName, true); err != nil {
			errs = append(errs, fmt.Sprintf("remove container: %v", err))
		}
		if err := ps.coreAPI.DockerRemoveContainer(preview.ContainerName+"-staging", true); err != nil {
			// Staging missing is the common case — only log verbose failure.
			ps.logger.Debug("staging container already gone", "err", err)
		}
	}

	// 2c. Image (preview images are never shared across PRs).
	if preview.ImageTag != "" {
		removeImage(ctx, preview.ImageTag)
	}

	// 2d. On-disk dirs — H4 fix. Both trees are under $data/, known
	// locations, safe to RemoveAll.
	srcDir := filepath.Join(ps.svc.dataDir, "preview-sources", fmt.Sprintf("preview_%d", id))
	logDir := filepath.Join(ps.svc.dataDir, "logs", fmt.Sprintf("preview_%d", id))
	if err := os.RemoveAll(srcDir); err != nil {
		errs = append(errs, fmt.Sprintf("remove src dir: %v", err))
	}
	if err := os.RemoveAll(logDir); err != nil {
		errs = append(errs, fmt.Sprintf("remove log dir: %v", err))
	}

	// 3. DB row. If external cleanup had failures, keep the row in
	// cleanup_failed so the UI surfaces the problem — otherwise we'd
	// silently orphan the external resources.
	if len(errs) > 0 {
		ps.db.Model(&preview).Updates(map[string]interface{}{
			"status":         "cleanup_failed",
			"failure_reason": strings.Join(errs, "; "),
		})
		ps.logger.Warn("preview cleanup partial failure; row retained as cleanup_failed",
			"id", id, "errs", errs)
		return fmt.Errorf("preview cleanup partial failure: %s", strings.Join(errs, "; "))
	}
	if err := ps.db.Delete(&PreviewDeployment{}, id).Error; err != nil {
		return fmt.Errorf("delete record: %w", err)
	}
	ps.logger.Info("preview deployment deleted", "id", id, "domain", preview.Domain)
	return nil
}

// CleanupExpired removes all expired preview deployments regardless of
// their current status. Codex M2 fix: previously only `status=running`
// was swept, so rows stuck in `pending` / `building` / `failed` leaked
// past their expires_at and kept disk + images + hosts around forever.
func (ps *PreviewService) CleanupExpired() int {
	var expired []PreviewDeployment
	ps.db.Where("expires_at < ?", time.Now()).Find(&expired)

	count := 0
	for _, p := range expired {
		if err := ps.DeletePreview(p.ID); err != nil {
			ps.logger.Error("cleanup expired preview failed", "id", p.ID, "err", err)
		} else {
			count++
		}
	}
	return count
}

// ListByProject returns all preview deployments for a project.
func (ps *PreviewService) ListByProject(projectID uint) ([]PreviewDeployment, error) {
	var previews []PreviewDeployment
	err := ps.db.Where("project_id = ?", projectID).Order("created_at DESC").Find(&previews).Error
	return previews, err
}

// sanitizeForDomain converts a string to a DNS-safe label.
//
// RFC 1035 labels: 1–63 chars, [a-z0-9-], can't start or end with `-`.
// We cap at 20 chars here to leave room for `pr-<N>-` prefix + `-<id>`
// suffix + the wildcard domain. Truncation happens BEFORE final trim so
// a long-name truncation can't leave a trailing hyphen (Codex M3 fix).
func sanitizeForDomain(s string) string {
	var result []byte
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			result = append(result, c)
		case c >= 'A' && c <= 'Z':
			result = append(result, c+32)
		default:
			result = append(result, '-')
		}
	}
	s = string(result)
	// Truncate FIRST — trimming after truncation is what guarantees no
	// trailing hyphen regardless of where the boundary lands.
	if len(s) > 20 {
		s = s[:20]
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "app"
	}
	return s
}

// init registers the PreviewDeployment table for auto-migration.
func init() {
	// Table migration happens in plugin.go Init().
	_ = PreviewDeployment{}
	_ = os.Getenv // suppress unused import
}
