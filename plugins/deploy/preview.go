package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// PreviewService manages ephemeral preview deployments from GitHub PRs.
type PreviewService struct {
	db      *gorm.DB
	svc     *Service
	coreAPI pluginpkg.CoreAPI
	logger  *slog.Logger
}

// NewPreviewService creates a new PreviewService.
func NewPreviewService(db *gorm.DB, svc *Service, coreAPI pluginpkg.CoreAPI, logger *slog.Logger) *PreviewService {
	return &PreviewService{db: db, svc: svc, coreAPI: coreAPI, logger: logger}
}

// CreatePreview creates (or re-triggers) a preview deployment for a GitHub PR.
//
// The full pipeline is:
//  1. Preflight — preview feature enabled + wildcard domain configured
//  2. Record — upsert a PreviewDeployment row (keyed on project_id + pr_number)
//  3. Build — clone the PR branch into a dedicated source dir and build an
//     image tag per preview; only implemented for projects in Docker deploy
//     mode (bare-metal previews are intentionally out of scope for v0.14 — a
//     single machine can't practically run many copies of a systemd service)
//  4. Run — start the container on an allocated non-privileged port
//  5. Expose — create a Caddy reverse-proxy host at pr-N-<app>.<wildcard>
//     pointing at the container's port
//
// Runs asynchronously: the DB record is returned with status="pending" and
// a goroutine performs steps 3–5. Webhook handler can ack the PR quickly
// while the build runs for minutes. Status transitions: pending → building
// → running | failed.
func (ps *PreviewService) CreatePreview(projectID uint, prNumber int, branch string) (*PreviewDeployment, error) {
	project, err := ps.svc.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	if !project.PreviewEnabled {
		return nil, fmt.Errorf("preview deployments are not enabled for this project")
	}

	// Preview build mode is currently Docker-only. Bare-metal previews
	// would require running multiple concurrent copies of the project's
	// systemd service on the same host, which is complex port + unit-name
	// orchestration we don't need until someone asks for it.
	if project.DeployMode != "docker" {
		return nil, fmt.Errorf("preview deployments require Docker deploy mode; project is in %q mode", project.DeployMode)
	}

	domain, err := ps.coreAPI.GetSetting("wildcard_domain")
	if err != nil || domain == "" {
		return nil, fmt.Errorf("wildcard_domain not configured — set it in Settings → Deploy → Preview before enabling preview deploys")
	}

	previewDomain := fmt.Sprintf("pr-%d-%s.%s", prNumber, sanitizeForDomain(project.Name), domain)
	containerName := fmt.Sprintf("preview-%s-pr-%d", sanitizeForDomain(project.Name), prNumber)

	expiry := project.PreviewExpiry
	if expiry <= 0 {
		expiry = 7
	}
	newExpiry := time.Now().AddDate(0, 0, expiry)

	// Upsert record. If a preview for this PR already exists, reuse its
	// row + container name (we rebuild in place) and bump the expiry.
	var preview PreviewDeployment
	err = ps.db.Where("project_id = ? AND pr_number = ?", projectID, prNumber).First(&preview).Error
	switch {
	case err == nil:
		ps.logger.Info("rebuilding existing preview", "project", project.Name, "pr", prNumber, "branch", branch)
		ps.db.Model(&preview).Updates(map[string]interface{}{
			"branch":     branch,
			"status":     "pending",
			"expires_at": newExpiry,
		})
	case err == gorm.ErrRecordNotFound:
		preview = PreviewDeployment{
			ProjectID:     projectID,
			PRNumber:      prNumber,
			Branch:        branch,
			Domain:        previewDomain,
			ContainerName: containerName,
			Status:        "pending",
			ExpiresAt:     newExpiry,
		}
		if err := ps.db.Create(&preview).Error; err != nil {
			return nil, fmt.Errorf("create preview record: %w", err)
		}
	default:
		return nil, fmt.Errorf("lookup existing preview: %w", err)
	}

	// Fire the build in the background. Webhook handler returns fast so
	// GitHub doesn't retry; the run_preview goroutine owns status
	// transitions until the container is up (or the build fails).
	go ps.runPreview(preview.ID, project.ID, branch)

	return &preview, nil
}

// runPreview executes the full build-run-expose pipeline asynchronously.
// Called from a goroutine by CreatePreview so the webhook handler can ack
// GitHub quickly. All status transitions + cleanup on failure happen here.
//
// On failure: marks status="failed", tears down any partial resources
// (image / container / host) so a retry (PR `synchronize` event) can
// start fresh. The preview DB row itself is NOT deleted on failure —
// the UI uses it to surface the error to the admin.
func (ps *PreviewService) runPreview(previewID, projectID uint, branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var preview PreviewDeployment
	if err := ps.db.First(&preview, previewID).Error; err != nil {
		ps.logger.Error("runPreview: preview vanished", "id", previewID, "err", err)
		return
	}
	project, err := ps.svc.GetProject(projectID)
	if err != nil {
		ps.markFailed(previewID, fmt.Sprintf("project lookup: %v", err))
		return
	}
	ps.db.Model(&preview).Update("status", "building")

	// Per-preview log file under the standard build-log directory; streaming
	// it to the UI is a Phase B concern, but persisting means admins can
	// `cat` it from disk today.
	logDir := filepath.Join(ps.svc.dataDir, "logs", fmt.Sprintf("preview_%d", previewID))
	_ = os.MkdirAll(logDir, 0755)
	logWriter, logErr := NewLogWriter(filepath.Join(logDir, "build.log"))
	if logErr != nil {
		ps.logger.Warn("preview log writer failed; proceeding without file log", "err", logErr)
	}

	// Per-preview source directory — keeps the project's main source tree
	// untouched so concurrent main-project builds don't stomp the PR
	// checkout.
	srcDir := filepath.Join(ps.svc.dataDir, "preview-sources", fmt.Sprintf("preview_%d", previewID))
	if err := os.MkdirAll(filepath.Dir(srcDir), 0755); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("mkdir parent: %v", err))
		return
	}

	// Resolve credentials via the same path the main build uses so SSH /
	// GitHub App / GitHub OAuth / plain HTTPS all work identically.
	authMethod, deployKey, httpsToken, err := ps.svc.GetGitCredentials(project)
	if err != nil {
		ps.markFailed(previewID, fmt.Sprintf("git credentials: %v", err))
		return
	}
	cloneURL := project.GitURL
	if authMethod == "github_app" || authMethod == "github_oauth" {
		cloneURL = injectHTTPSToken(project.GitURL, httpsToken)
	}
	if err := ps.svc.git.CloneToDir(cloneURL, branch, deployKey, srcDir, logWriter); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("git clone: %v", err))
		return
	}

	// Build a dedicated image tag so old preview images can be GC'd
	// without touching the main project image.
	imageTag := fmt.Sprintf("webcasa-preview-%d", previewID)
	if err := ps.svc.docker.BuildImageWithTag(ctx, srcDir, imageTag, logWriter, project.BuildType); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("build: %v", err))
		return
	}

	// Port allocation: previews use 20000+previewID, safely above the
	// main project range (10000+projectID).
	port := 20000 + int(previewID)

	// Copy project env vars + inject PR context so the app can render a
	// "preview of PR #N" banner.
	envVars := project.EnvVarList
	envVars = append(envVars,
		EnvVar{Key: "WEBCASA_PREVIEW", Value: "1"},
		EnvVar{Key: "WEBCASA_PREVIEW_PR", Value: fmt.Sprintf("%d", preview.PRNumber)},
		EnvVar{Key: "WEBCASA_PREVIEW_BRANCH", Value: branch},
	)

	ps.svc.docker.StopAndRemove(preview.ContainerName)
	if _, err := ps.svc.docker.RunWithName(ctx, preview.ContainerName, imageTag, port, envVars); err != nil {
		ps.markFailed(previewID, fmt.Sprintf("run container: %v", err))
		return
	}

	// Create/update the Caddy host. Rebuild path (PR synchronize) may
	// have an existing host_id; update its upstream instead of creating
	// a duplicate.
	if preview.HostID > 0 {
		if err := ps.coreAPI.UpdateHostUpstream(preview.HostID, fmt.Sprintf("localhost:%d", port)); err != nil {
			ps.logger.Warn("update preview host upstream failed; recreating", "err", err)
			_ = ps.coreAPI.DeleteHost(preview.HostID)
			preview.HostID = 0
		}
	}
	if preview.HostID == 0 {
		hostID, err := ps.coreAPI.CreateHost(pluginpkg.CreateHostRequest{
			Domain:       preview.Domain,
			HostType:     "proxy",
			UpstreamAddr: fmt.Sprintf("localhost:%d", port),
			TLSEnabled:   true,
			HTTPRedirect: true,
			WebSocket:    true,
			Compression:  true,
		})
		if err != nil {
			ps.markFailed(previewID, fmt.Sprintf("create host: %v", err))
			ps.svc.docker.StopAndRemove(preview.ContainerName)
			return
		}
		preview.HostID = hostID
	}

	ps.db.Model(&preview).Updates(map[string]interface{}{
		"host_id": preview.HostID,
		"status":  "running",
	})
	ps.logger.Info("preview deployment running",
		"project", project.Name, "pr", preview.PRNumber,
		"domain", preview.Domain, "port", port)
}

// injectHTTPSToken rewrites an https:// URL to embed a token for clone auth.
// Used for GitHub App / OAuth flows where the "deploy key" is actually a
// short-lived HTTPS token.
func injectHTTPSToken(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "https://") {
		return "https://x-access-token:" + token + "@" + strings.TrimPrefix(rawURL, "https://")
	}
	return rawURL
}

// markFailed records a failure reason and flips status to "failed" so the
// UI and next webhook `synchronize` event can surface it.
func (ps *PreviewService) markFailed(previewID uint, reason string) {
	ps.logger.Error("preview deployment failed", "id", previewID, "reason", reason)
	ps.db.Model(&PreviewDeployment{}).Where("id = ?", previewID).Update("status", "failed")
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// DeletePreview removes a preview deployment and cleans up all resources.
func (ps *PreviewService) DeletePreview(id uint) error {
	var preview PreviewDeployment
	if err := ps.db.First(&preview, id).Error; err != nil {
		return fmt.Errorf("preview not found: %w", err)
	}

	var errs []string

	// Delete Caddy host.
	if preview.HostID > 0 {
		if err := ps.coreAPI.DeleteHost(preview.HostID); err != nil {
			ps.logger.Warn("failed to delete preview host", "host_id", preview.HostID, "err", err)
			errs = append(errs, fmt.Sprintf("delete host: %v", err))
		}
	}

	// Delete container.
	if preview.ContainerName != "" {
		if err := ps.coreAPI.DockerRemoveContainer(preview.ContainerName, true); err != nil {
			ps.logger.Warn("failed to remove preview container", "container", preview.ContainerName, "err", err)
			errs = append(errs, fmt.Sprintf("remove container: %v", err))
		}
	}

	// Delete preview record.
	if err := ps.db.Delete(&PreviewDeployment{}, id).Error; err != nil {
		ps.logger.Error("failed to delete preview record", "id", id, "err", err)
		errs = append(errs, fmt.Sprintf("delete record: %v", err))
	}

	ps.logger.Info("preview deployment deleted", "id", id, "domain", preview.Domain)
	if len(errs) > 0 {
		return fmt.Errorf("preview cleanup partial failure: %s", strings.Join(errs, "; "))
	}
	return nil
}

// CleanupExpired removes all expired preview deployments.
func (ps *PreviewService) CleanupExpired() int {
	var expired []PreviewDeployment
	ps.db.Where("expires_at < ? AND status = ?", time.Now(), "running").Find(&expired)

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
func sanitizeForDomain(s string) string {
	var result []byte
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32)
		} else {
			result = append(result, '-')
		}
	}
	// Trim hyphens and limit length.
	s = string(result)
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == '-' {
		s = s[:len(s)-1]
	}
	if len(s) > 20 {
		s = s[:20]
	}
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
