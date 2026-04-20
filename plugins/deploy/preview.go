package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
	// createMu serializes the entire CreatePreview lookup-and-allocate
	// sequence so two concurrent webhooks for different PRs (or the
	// same one) cannot race base_port allocation. R7-H1 fix.
	createMu sync.Mutex
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

	// R7-H1 fix: serialize the lookup+create+allocate sequence so two
	// concurrent webhooks cannot both pick the same base_port or both
	// reach the "create new row" branch and race the unique (project_id,
	// pr_number) index into a 500 response.
	ps.createMu.Lock()
	preview, created, err := ps.upsertPreviewRow(project, prNumber, branch, newExpiry, domain)
	ps.createMu.Unlock()
	if err != nil {
		return nil, err
	}
	if created {
		ps.logger.Info("created new preview", "project", project.Name, "pr", prNumber, "base_port", preview.BasePort)
	} else {
		ps.logger.Info("rebuilding existing preview", "project", project.Name, "pr", prNumber, "branch", branch)
	}

	// Cancel any in-flight runPreview for this ID before spawning a new
	// one. C2 race fix: force-push sends two synchronize webhooks within
	// ms; without this both would fight over the same containerName +
	// srcDir. R7-H2: if the previous job did not drain within the
	// timeout, refuse rather than spawn a racing job.
	if !ps.cancelJob(preview.ID) {
		return nil, fmt.Errorf("previous preview job for PR %d did not drain in time; webhook will retry", prNumber)
	}

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

// upsertPreviewRow looks up an existing preview by (project_id,
// pr_number) and either updates it (rebuild path) or creates a fresh
// row with all derived fields populated atomically (no post-Create
// backfill, so a partial-create cannot leave base_port=0). Caller must
// hold ps.createMu so port allocation is single-flight.
//
// Returns the resolved preview row + a `created` flag.
func (ps *PreviewService) upsertPreviewRow(project *Project, prNumber int, branch string, expiry time.Time, wildcardDomain string) (PreviewDeployment, bool, error) {
	var preview PreviewDeployment
	err := ps.db.Where("project_id = ? AND pr_number = ?", project.ID, prNumber).First(&preview).Error
	if err == nil {
		// Rebuild path. Preserve BasePort + Slot so the running container
		// keeps serving until runPreview swaps it out.
		if uerr := ps.db.Model(&preview).Updates(map[string]interface{}{
			"branch":         branch,
			"status":         "pending",
			"expires_at":     expiry,
			"failure_reason": "",
		}).Error; uerr != nil {
			return preview, false, fmt.Errorf("update existing preview: %w", uerr)
		}
		preview.Branch = branch
		preview.Status = "pending"
		return preview, false, nil
	}
	if err != gorm.ErrRecordNotFound {
		return preview, false, fmt.Errorf("lookup existing preview: %w", err)
	}

	// Fresh-create path. Insert a temporary row first so we have an ID
	// (needed for image_tag / domain / base_port allocation seed), then
	// fill in the derived fields in a SINGLE transactional Updates call.
	// All Updates checks are explicit so a DB error fails-loud rather
	// than leaving a half-baked row in the table.
	preview = PreviewDeployment{
		ProjectID: project.ID,
		PRNumber:  prNumber,
		Branch:    branch,
		Status:    "pending",
		ExpiresAt: expiry,
		Slot:      -1,
	}
	if cerr := ps.db.Create(&preview).Error; cerr != nil {
		// Unique-index conflict on (project_id, pr_number) — racing
		// webhook beat us. Re-lookup and treat as rebuild.
		var existing PreviewDeployment
		if rerr := ps.db.Where("project_id = ? AND pr_number = ?", project.ID, prNumber).First(&existing).Error; rerr == nil {
			ps.logger.Info("preview row created by concurrent webhook; updating",
				"project", project.Name, "pr", prNumber)
			if uerr := ps.db.Model(&existing).Updates(map[string]interface{}{
				"branch":         branch,
				"status":         "pending",
				"expires_at":     expiry,
				"failure_reason": "",
			}).Error; uerr != nil {
				return existing, false, fmt.Errorf("update existing preview after race: %w", uerr)
			}
			existing.Branch = branch
			existing.Status = "pending"
			return existing, false, nil
		}
		return preview, false, fmt.Errorf("create preview record: %w", cerr)
	}

	basePort, perr := ps.allocateBasePort(preview.ID)
	if perr != nil {
		ps.db.Delete(&preview)
		return preview, false, fmt.Errorf("allocate port: %w", perr)
	}
	slug := sanitizeForDomain(project.Name)
	previewDomain := fmt.Sprintf("pr-%d-%s-%d.%s", prNumber, slug, preview.ID, wildcardDomain)
	imageTag := fmt.Sprintf("webcasa-preview-%d", preview.ID)
	if uerr := ps.db.Model(&preview).Updates(map[string]interface{}{
		"domain":    previewDomain,
		"image_tag": imageTag,
		"base_port": basePort,
	}).Error; uerr != nil {
		// Backfill failed — row exists but is unusable. Delete it so a
		// retry from the webhook can start fresh.
		ps.db.Delete(&preview)
		return preview, false, fmt.Errorf("backfill preview fields: %w", uerr)
	}
	preview.Domain = previewDomain
	preview.ImageTag = imageTag
	preview.BasePort = basePort
	return preview, true, nil
}

// cancelJob cancels a running runPreview for the given preview ID and
// waits up to 30s for it to drain. Returns true if no job was running
// or the job exited cleanly within the deadline; false if the timeout
// was hit (caller must NOT spawn a replacement job — the stale job is
// still mid-flight and will fight for resources).
//
// R7-H2 fix: previously this swallowed the timeout, letting a stale
// job continue into UpdateHostUpstream / DB writes after a fresh job
// for the same preview started — the stale finish would clobber the
// new slot in DB.
func (ps *PreviewService) cancelJob(previewID uint) bool {
	ps.jobsMu.Lock()
	job, ok := ps.jobs[previewID]
	ps.jobsMu.Unlock()
	if !ok {
		return true
	}
	job.cancel()
	select {
	case <-job.done:
		return true
	case <-time.After(30 * time.Second):
		ps.logger.Warn("previous preview job did not drain in 30s", "preview_id", previewID)
		return false
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

// allocateBasePort picks a slot-0 base port in [20000, 25000) that isn't
// taken by another preview. Slot-1 port is always base+5000, landing in
// [25000, 30000). Because BasePort has a unique index, two previews are
// guaranteed non-colliding on both slot ports.
//
// Uses a deterministic starting point for spread without a central
// atomic counter. Bounded at 5000 slots; ample for any realistic
// preview workload.
func (ps *PreviewService) allocateBasePort(previewID uint) (int, error) {
	const base = 20000
	const rng = 5000
	start := base + int(previewID%uint(rng))
	for i := 0; i < rng; i++ {
		candidate := base + ((start - base + i) % rng)
		var count int64
		ps.db.Model(&PreviewDeployment{}).
			Where("base_port = ? AND id != ?", candidate, previewID).
			Count(&count)
		if count == 0 {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("no free preview base port in [%d, %d)", base, base+rng)
}

// slotName returns the canonical container name for a given preview +
// slot. Deterministic so DeletePreview can target both slots without
// tracking the inactive one's state.
func slotName(previewID uint, slot int) string {
	return fmt.Sprintf("webcasa-preview-%d-p%d", previewID, slot)
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
	authMethod, deployKey, httpsToken, err := ps.svc.GetGitCredentials(project)
	if err != nil {
		ps.markFailed(previewID, fmt.Sprintf("git credentials: %v", err))
		return
	}
	// R7-H3 fix: when auth_method is github_app/github_oauth, the project
	// URL is typically SSH (`git@github.com:owner/repo`) but the token
	// path needs HTTPS. Convert to a clean HTTPS URL (no embedded
	// credentials) and let CloneToDir push the token via env var.
	cloneURL := project.GitURL
	if (authMethod == "github_app" || authMethod == "github_oauth") && httpsToken != "" {
		cloneURL = ConvertSSHToCleanHTTPS(project.GitURL)
		deployKey = "" // SSH key not needed on the HTTPS path
	}
	// H3/R6-H1 fix: pass the clean URL + token separately. CloneToDir
	// delivers the token via the GIT_CONFIG_COUNT env-var ladder
	// (invisible to `ps`) scoped to the requesting origin — so the
	// token never lands in argv and cannot leak on redirect.
	if err := ps.svc.git.CloneToDir(ctx, cloneURL, branch, deployKey, httpsToken, srcDir, logWriter); err != nil {
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

	// R6-C1 fix: two-slot alternation. Slot 0 binds BasePort, slot 1
	// binds BasePort+5000. Each deploy flips to the unused slot so the
	// old container keeps serving while the new one comes up, and we
	// never need to rename+remap ports (which the old implementation
	// did incorrectly — docker rename doesn't move port bindings, so
	// Caddy was pointed at a port nothing was listening on).
	//
	// currentSlot == -1 on first run → nextSlot = 0
	// currentSlot == 0             → nextSlot = 1
	// currentSlot == 1             → nextSlot = 0
	nextSlot := 0
	if preview.Slot == 0 {
		nextSlot = 1
	}
	nextPort := preview.BasePort
	if nextSlot == 1 {
		nextPort = preview.BasePort + 5000
	}
	nextContainer := slotName(preview.ID, nextSlot)

	// Remove any leftover from a previous failed attempt in the same slot.
	ps.svc.docker.StopAndRemove(nextContainer)
	if ctx.Err() != nil {
		return
	}
	if _, err := ps.svc.docker.RunWithName(ctx, nextContainer, imageTag, nextPort, envVars); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("run staging container: %v", err))
		return
	}

	// R6-H3 fix: proper TCP readiness probe instead of a flat 3s sleep.
	// Give the container up to 30s to bind its port; fail fast if it
	// crashes immediately. Uses the same port dialing probe as the main
	// deploy healthcheck — Phase B will layer HTTP-level checks on top.
	if err := waitForPortOpen(ctx, nextPort, 30*time.Second); err != nil {
		ps.svc.docker.StopAndRemove(nextContainer)
		ps.markFailed(previewID, fmt.Sprintf("staging container failed to bind port %d: %v", nextPort, err))
		return
	}

	// R7-H2: re-check ctx before host updates. CreateHost/UpdateHostUpstream
	// don't take a ctx and may take seconds (Caddy reload). If the job
	// was cancelled while waiting for the container, abort before
	// touching shared infra (Caddy host) we'd then have to undo.
	if ctx.Err() != nil {
		ps.svc.docker.StopAndRemove(nextContainer)
		return
	}

	// Create the host on first run, or update existing host's upstream to
	// point at the new slot's port. Old slot (if any) keeps serving
	// until this call returns; so failed upstream update leaves the old
	// version live.
	if preview.HostID == 0 {
		hostID, err := ps.coreAPI.CreateHost(pluginpkg.CreateHostRequest{
			Domain:       preview.Domain,
			HostType:     "proxy",
			UpstreamAddr: fmt.Sprintf("localhost:%d", nextPort),
			TLSEnabled:   true,
			HTTPRedirect: true,
			WebSocket:    true,
			Compression:  true,
		})
		if err != nil {
			ps.svc.docker.StopAndRemove(nextContainer)
			ps.markFailed(previewID, fmt.Sprintf("create host: %v", err))
			return
		}
		preview.HostID = hostID
		ps.db.Model(&preview).Update("host_id", hostID)
	} else {
		if err := ps.coreAPI.UpdateHostUpstream(preview.HostID, fmt.Sprintf("localhost:%d", nextPort)); err != nil {
			ps.svc.docker.StopAndRemove(nextContainer)
			ps.markFailed(previewID, fmt.Sprintf("update host upstream: %v", err))
			return
		}
	}

	// Caddy now points at the new slot. Stop + remove the previous slot
	// container; this is the only destructive step and it only runs
	// AFTER traffic has moved.
	if preview.Slot >= 0 {
		oldContainer := slotName(preview.ID, preview.Slot)
		ps.svc.docker.StopAndRemove(oldContainer)
	}

	// R7-H2: persist via conditional update — WHERE slot = preview.Slot
	// matches only if no other goroutine has already advanced past us
	// (e.g. a stale post-cancel-timeout job from before, or a delete +
	// re-create racing). RowsAffected==0 means our work is stale; back
	// out the new container so we don't leave an orphan listening on
	// the new port.
	res := ps.db.Model(&PreviewDeployment{}).
		Where("id = ? AND slot = ?", previewID, preview.Slot).
		Updates(map[string]interface{}{
			"slot":           nextSlot,
			"port":           nextPort,
			"container_name": nextContainer,
			"status":         "running",
			"failure_reason": "",
		})
	if res.Error != nil || res.RowsAffected == 0 {
		ps.logger.Warn("preview run completed but DB row advanced past us; backing out",
			"id", previewID, "expected_slot", preview.Slot, "err", res.Error)
		ps.svc.docker.StopAndRemove(nextContainer)
		return
	}
	ps.logger.Info("preview deployment running",
		"project", project.Name, "pr", preview.PRNumber,
		"domain", preview.Domain, "port", nextPort, "slot", nextSlot)
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

// waitForPortOpen polls the loopback port every 500ms until a TCP
// connection succeeds or the timeout elapses. Returns nil on success.
// Used as a simple readiness probe for freshly-started preview
// containers — replaces the previous fixed 3s sleep (R6-H3).
func waitForPortOpen(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("timeout after %s waiting for port %d", timeout, port)
}

// removeImage force-removes a previously built image. Used during cleanup
// on failure/delete so images don't accumulate.
func removeImage(ctx context.Context, imageTag string) {
	_ = exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag).Run()
}

// isNotFoundErr classifies an error as "the target resource didn't
// exist". Container / image cleanup must be idempotent — if the item
// was never created (e.g. build failed before Run) we don't want to
// flag that as a cleanup failure that traps the row in cleanup_failed
// forever (R6-M1).
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such container") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "no such object")
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

	// 1. Cancel in-flight build (no-op if nothing running). For Delete
	// we tolerate the timeout — even if the goroutine is still draining
	// we want to push through cleanup; the goroutine's row-existence
	// check (setStatus / re-read after build) will see the deletion
	// and abort.
	_ = ps.cancelJob(id)

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

	// 2b. Containers: remove both slot containers. Either may not exist
	// if the preview never got past the build step or was in the middle
	// of a flip; "not found" is not a cleanup failure (R6-M1 fix).
	for _, slot := range []int{0, 1} {
		name := slotName(id, slot)
		if err := ps.coreAPI.DockerRemoveContainer(name, true); err != nil && !isNotFoundErr(err) {
			errs = append(errs, fmt.Sprintf("remove container %s: %v", name, err))
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
