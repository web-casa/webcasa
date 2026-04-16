package deploy

import (
	"fmt"
	"log/slog"
	"os"
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

// CreatePreview creates a preview deployment for a GitHub PR.
func (ps *PreviewService) CreatePreview(projectID uint, prNumber int, branch string) (*PreviewDeployment, error) {
	project, err := ps.svc.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	if !project.PreviewEnabled {
		return nil, fmt.Errorf("preview deployments are not enabled for this project")
	}

	// Check if a preview already exists for this PR.
	var existing PreviewDeployment
	if ps.db.Where("project_id = ? AND pr_number = ?", projectID, prNumber).First(&existing).Error == nil {
		// Update existing: mark as pending re-deploy.
		ps.logger.Info("updating existing preview", "project", project.Name, "pr", prNumber)
		ps.db.Model(&existing).Updates(map[string]interface{}{
			"branch": branch,
			"status": "pending",
		})
		// TODO: trigger actual rebuild of the preview container with latest code
		return &existing, nil
	}

	// Generate domain from wildcard setting.
	domain, err := ps.coreAPI.GetSetting("wildcard_domain")
	if err != nil || domain == "" {
		return nil, fmt.Errorf("wildcard_domain not configured — required for preview deployments")
	}
	previewDomain := fmt.Sprintf("pr-%d-%s.%s", prNumber, sanitizeForDomain(project.Name), domain)

	expiry := project.PreviewExpiry
	if expiry <= 0 {
		expiry = 7
	}

	preview := &PreviewDeployment{
		ProjectID:     projectID,
		PRNumber:      prNumber,
		Branch:        branch,
		Domain:        previewDomain,
		ContainerName: fmt.Sprintf("preview-%s-pr-%d", sanitizeForDomain(project.Name), prNumber),
		Status:        "pending", // pending until actual container is provisioned
		ExpiresAt:     time.Now().AddDate(0, 0, expiry),
	}

	if err := ps.db.Create(preview).Error; err != nil {
		return nil, fmt.Errorf("create preview record: %w", err)
	}

	ps.logger.Info("preview deployment created", "project", project.Name, "pr", prNumber, "domain", previewDomain)
	return preview, nil
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
