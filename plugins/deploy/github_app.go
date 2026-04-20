package deploy

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

// GitHubAppAuth handles GitHub App authentication for cloning private repos.
type GitHubAppAuth struct{}

// GenerateJWT creates a short-lived JWT (10 min) signed with the App's private key.
func (g *GitHubAppAuth) GenerateJWT(appID int64, privateKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block from private key")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 as fallback
		pkcs8Key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", fmt.Errorf("parse private key: %w (pkcs8: %w)", err, err2)
		}
		var ok bool
		key, ok = pkcs8Key.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("private key is not RSA")
		}
	}

	now := time.Now()
	claims := jwtv5.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(), // issued at (1 min in the past for clock drift)
		"exp": now.Add(10 * time.Minute).Unix(),  // expires in 10 minutes
		"iss": appID,
	}

	token := jwtv5.NewWithClaims(jwtv5.SigningMethodRS256, claims)
	return token.SignedString(key)
}

// GetInstallationToken exchanges a JWT for a short-lived installation access token.
func (g *GitHubAppAuth) GetInstallationToken(jwt string, installationID int64) (string, error) {
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.Token, nil
}

// GetCloneToken obtains a GitHub App installation token for cloning.
func (g *GitHubAppAuth) GetCloneToken(appID int64, privateKeyPEM string, installationID int64) (string, error) {
	jwt, err := g.GenerateJWT(appID, privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("generate JWT: %w", err)
	}
	return g.GetInstallationToken(jwt, installationID)
}

// ConvertSSHToCleanHTTPS converts an SSH git URL to its clean HTTPS form
// (no embedded credentials), or strips embedded credentials from an
// existing HTTPS URL. Used by callers that authenticate via separate
// channels (env vars, headers) rather than baking the token into the
// URL — preview deploys take this path so the token doesn't surface
// in argv (R6-H1) or in `git remote -v` output (R7-H3).
//
// Returns an error for URLs that cannot be safely converted (e.g.,
// non-GitHub SSH forms paired with a GitHub-only token). Callers MUST
// surface this error rather than silently falling back to the SSH URL
// without a deploy key (R8-M2 — failing loud beats clone-fails-with-
// auth-error in build logs).
//
// Forms handled:
//
//	git@github.com:owner/repo[.git]            → https://github.com/owner/repo[.git]
//	ssh://git@github.com[:port]/owner/repo     → https://github.com/owner/repo
//	https://[user:pass@]github.com/owner/repo  → https://github.com/owner/repo
func ConvertSSHToCleanHTTPS(gitURL string) (string, error) {
	// HTTPS form — strip any embedded credentials and return.
	if strings.HasPrefix(gitURL, "https://") || strings.HasPrefix(gitURL, "http://") {
		idx := strings.Index(gitURL, "://")
		rest := gitURL[idx+3:]
		if at := strings.Index(rest, "@"); at != -1 {
			return gitURL[:idx+3] + rest[at+1:], nil
		}
		return gitURL, nil
	}
	// scp-like SSH: git@host:owner/repo
	if strings.HasPrefix(gitURL, "git@") {
		colon := strings.Index(gitURL, ":")
		at := strings.Index(gitURL, "@")
		if colon > at {
			host := gitURL[at+1 : colon]
			path := gitURL[colon+1:]
			return "https://" + host + "/" + path, nil
		}
		return "", fmt.Errorf("unrecognized SSH URL form: %q", gitURL)
	}
	// ssh://[user@]host[:port]/path
	if strings.HasPrefix(gitURL, "ssh://") {
		rest := gitURL[len("ssh://"):]
		if at := strings.Index(rest, "@"); at != -1 {
			rest = rest[at+1:]
		}
		slash := strings.Index(rest, "/")
		if slash == -1 {
			return "", fmt.Errorf("ssh:// URL missing path: %q", gitURL)
		}
		host := rest[:slash]
		// Drop port if present — HTTPS will use 443.
		if colon := strings.Index(host, ":"); colon != -1 {
			host = host[:colon]
		}
		return "https://" + host + rest[slash:], nil
	}
	return "", fmt.Errorf("cannot convert git URL to HTTPS (not SSH or HTTPS): %q", gitURL)
}

// ConvertToHTTPS converts a git URL (SSH or HTTPS) to HTTPS format with token auth.
func ConvertToHTTPS(gitURL, token string) string {
	// Handle SSH URLs: git@github.com:user/repo.git
	if strings.HasPrefix(gitURL, "git@github.com:") {
		repo := strings.TrimPrefix(gitURL, "git@github.com:")
		return fmt.Sprintf("https://x-access-token:%s@github.com/%s", token, repo)
	}
	// Handle HTTPS URLs: https://github.com/user/repo.git
	if strings.Contains(gitURL, "github.com") {
		// Strip existing credentials if any
		cleaned := gitURL
		if idx := strings.Index(cleaned, "://"); idx != -1 {
			rest := cleaned[idx+3:]
			if atIdx := strings.Index(rest, "@"); atIdx != -1 {
				cleaned = cleaned[:idx+3] + rest[atIdx+1:]
			}
		}
		return strings.Replace(cleaned, "https://github.com", fmt.Sprintf("https://x-access-token:%s@github.com", token), 1)
	}
	return gitURL
}
