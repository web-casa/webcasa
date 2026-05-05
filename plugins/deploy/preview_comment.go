package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// postOrUpdatePRComment posts (or edits an existing) "preview ready"
// comment on the GitHub PR. Best-effort: any failure is logged and
// swallowed — a flaky GitHub or invalid token must NOT fail the
// otherwise-successful deploy.
//
// First successful deploy: POST a new comment, store its ID on the
// PreviewDeployment row. Subsequent rebuilds: PATCH the same comment
// so the PR thread doesn't fill with one bot post per `synchronize`.
//
// Token comes from the project's `github_token` field (encrypted at
// rest, decrypted via decryptField). Either a GitHub PAT or a GitHub
// App installation token works — we use `Bearer` form which both
// accept.
//
// Skipped silently when:
//   - project.GitHubToken is empty (admin opted out)
//   - project.GitURL isn't a GitHub URL (no comment surface)
//   - HTTP call fails (logged at debug, comment will retry next deploy)
func (ps *PreviewService) postOrUpdatePRComment(preview *PreviewDeployment, project *Project) {
	if project.GitHubToken == "" {
		return
	}
	owner, repo, ok := parseGitHubOwnerRepo(project.GitURL)
	if !ok {
		ps.logger.Debug("preview PR comment skipped: not a GitHub URL",
			"preview_id", preview.ID, "url", project.GitURL)
		return
	}
	token, err := ps.svc.decryptField(project.GitHubToken)
	if err != nil || token == "" {
		ps.logger.Warn("preview PR comment skipped: token decrypt failed",
			"preview_id", preview.ID, "err", err)
		return
	}

	body := fmt.Sprintf(
		"🚀 **Preview deployment is live**\n\n%s\n\n"+
			"Built from `%s` · slot `%d` · port `%d`\n\n"+
			"_This comment is updated on every push. Comment auto-removes when the PR closes._",
		"https://"+preview.Domain,
		preview.Branch, preview.Slot, preview.Port,
	)
	payload, _ := json.Marshal(map[string]string{"body": body})

	ctx, cancel := context.WithTimeout(ps.rootCtx, 10*time.Second)
	defer cancel()

	if preview.PRCommentID > 0 {
		// PATCH existing comment.
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/comments/%d",
			owner, repo, preview.PRCommentID)
		if ok := ps.doGitHubRequest(ctx, http.MethodPatch, url, token, payload, nil); ok {
			return
		}
		// PATCH failed (likely 404 — comment was deleted manually).
		// Fall through to POST a fresh one.
		ps.logger.Info("preview PR comment PATCH failed; posting new one",
			"preview_id", preview.ID, "old_comment_id", preview.PRCommentID)
	}

	// POST new comment.
	postURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments",
		owner, repo, preview.PRNumber)
	var resp struct {
		ID int64 `json:"id"`
	}
	if !ps.doGitHubRequest(ctx, http.MethodPost, postURL, token, payload, &resp) {
		return
	}
	if resp.ID == 0 {
		ps.logger.Warn("preview PR comment POST returned no ID", "preview_id", preview.ID)
		return
	}
	// Persist the comment ID for next-rebuild PATCH.
	if err := ps.db.Model(&PreviewDeployment{}).
		Where("id = ?", preview.ID).
		Update("pr_comment_id", resp.ID).Error; err != nil {
		ps.logger.Warn("preview PR comment ID save failed", "preview_id", preview.ID, "err", err)
		return
	}
	preview.PRCommentID = resp.ID
}

// doGitHubRequest issues a single API call and JSON-decodes the
// response into `out` if non-nil. Returns true on 2xx.
func (ps *PreviewService) doGitHubRequest(ctx context.Context, method, url, token string, payload []byte, out interface{}) bool {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		ps.logger.Warn("github request build failed", "method", method, "url", url, "err", err)
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		ps.logger.Warn("github request failed", "method", method, "url", url, "err", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		ps.logger.Warn("github request non-2xx",
			"method", method, "url", url, "status", resp.StatusCode, "body", string(body))
		return false
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			ps.logger.Warn("github response decode failed", "err", err)
			return false
		}
	}
	return true
}

// parseGitHubOwnerRepo extracts (owner, repo) from common GitHub URL
// forms. Returns (_, _, false) for non-GitHub or unparseable URLs.
//
// Handles:
//
//	https://github.com/owner/repo[.git]
//	https://github.com/owner/repo[.git]/...
//	git@github.com:owner/repo[.git]
//	ssh://git@github.com[:port]/owner/repo[.git]
func parseGitHubOwnerRepo(gitURL string) (string, string, bool) {
	const host = "github.com"
	var rest string
	switch {
	case strings.HasPrefix(gitURL, "https://"+host+"/"):
		rest = strings.TrimPrefix(gitURL, "https://"+host+"/")
	case strings.HasPrefix(gitURL, "http://"+host+"/"):
		rest = strings.TrimPrefix(gitURL, "http://"+host+"/")
	case strings.HasPrefix(gitURL, "git@"+host+":"):
		rest = strings.TrimPrefix(gitURL, "git@"+host+":")
	case strings.HasPrefix(gitURL, "ssh://git@"+host):
		// ssh://git@github.com[:port]/path
		s := strings.TrimPrefix(gitURL, "ssh://git@"+host)
		if strings.HasPrefix(s, ":") {
			if slash := strings.Index(s, "/"); slash != -1 {
				s = s[slash+1:]
			} else {
				return "", "", false
			}
		} else {
			s = strings.TrimPrefix(s, "/")
		}
		rest = s
	default:
		return "", "", false
	}
	rest = strings.TrimSuffix(rest, ".git")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
