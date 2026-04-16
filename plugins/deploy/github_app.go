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
