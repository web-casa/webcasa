package deploy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/web-casa/webcasa/internal/crypto"
	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
	"gorm.io/gorm"
)

// GitHubOAuthService handles GitHub App Installation OAuth flow.
type GitHubOAuthService struct {
	configStore *pluginpkg.ConfigStore
	db          *gorm.DB
	jwtSecret   string
	logger      *slog.Logger
	ghApp       *GitHubAppAuth // reuse JWT generation logic

	// In-memory CSRF state nonces (state → expiry time).
	stateMu sync.Mutex
	states  map[string]time.Time
}

// NewGitHubOAuthService creates a new GitHubOAuthService.
func NewGitHubOAuthService(cs *pluginpkg.ConfigStore, db *gorm.DB, jwtSecret string, logger *slog.Logger) *GitHubOAuthService {
	return &GitHubOAuthService{
		configStore: cs,
		db:          db,
		jwtSecret:   jwtSecret,
		logger:      logger.With("module", "github-oauth"),
		ghApp:       &GitHubAppAuth{},
		states:      make(map[string]time.Time),
	}
}

// ── Configuration ──

// GitHubAppConfig holds the GitHub App credentials for OAuth.
type GitHubAppConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"` // masked in response
	AppID        string `json:"app_id"`
	AppSlug      string `json:"app_slug"`
	PrivateKey   string `json:"private_key"` // masked in response
	Configured   bool   `json:"configured"`
}

// GetConfig returns the stored GitHub App configuration with secrets masked.
func (s *GitHubOAuthService) GetConfig() GitHubAppConfig {
	cfg := GitHubAppConfig{
		ClientID: s.configStore.Get("github_client_id"),
		AppID:    s.configStore.Get("github_app_id"),
		AppSlug:  s.configStore.Get("github_app_slug"),
	}

	// Mask secrets.
	if enc := s.configStore.Get("github_client_secret"); enc != "" {
		cfg.ClientSecret = "****"
	}
	if enc := s.configStore.Get("github_private_key"); enc != "" {
		cfg.PrivateKey = "****"
	}

	cfg.Configured = cfg.ClientID != "" && cfg.ClientSecret != "" && cfg.AppID != "" && cfg.PrivateKey != ""
	return cfg
}

// SaveConfig persists the GitHub App configuration. Empty secret fields are skipped
// (preserving existing values); the string "****" is also treated as "no change".
func (s *GitHubOAuthService) SaveConfig(cfg GitHubAppConfig) error {
	if cfg.ClientID != "" {
		s.configStore.Set("github_client_id", cfg.ClientID)
	}
	if cfg.AppID != "" {
		s.configStore.Set("github_app_id", cfg.AppID)
	}
	if cfg.AppSlug != "" {
		s.configStore.Set("github_app_slug", cfg.AppSlug)
	}

	// Encrypt secrets before saving.
	if cfg.ClientSecret != "" && cfg.ClientSecret != "****" {
		enc, err := crypto.Encrypt(cfg.ClientSecret, s.jwtSecret)
		if err != nil {
			return fmt.Errorf("encrypt client_secret: %w", err)
		}
		s.configStore.Set("github_client_secret", enc)
	}
	if cfg.PrivateKey != "" && cfg.PrivateKey != "****" {
		enc, err := crypto.Encrypt(cfg.PrivateKey, s.jwtSecret)
		if err != nil {
			return fmt.Errorf("encrypt private_key: %w", err)
		}
		s.configStore.Set("github_private_key", enc)
	}
	return nil
}

// decryptSecret reads and decrypts a config key.
func (s *GitHubOAuthService) decryptSecret(key string) (string, error) {
	enc := s.configStore.Get(key)
	if enc == "" {
		return "", fmt.Errorf("%s not configured", key)
	}
	return crypto.Decrypt(enc, s.jwtSecret)
}

// ── OAuth Authorize ──

// GetAuthorizeURL builds the GitHub OAuth authorization URL.
// If the App slug is set, directs to the App installation page so users can select repos.
func (s *GitHubOAuthService) GetAuthorizeURL(callbackURL string) (string, error) {
	clientID := s.configStore.Get("github_client_id")
	appSlug := s.configStore.Get("github_app_slug")
	if clientID == "" {
		return "", fmt.Errorf("GitHub App not configured: client_id is empty")
	}

	state := s.generateState()

	// Use the GitHub App installation flow — this lets users select repos.
	if appSlug != "" {
		u := fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s", appSlug, url.QueryEscape(state))
		return u, nil
	}

	// Fallback: standard OAuth authorize (less control over repo selection).
	u := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&state=%s&redirect_uri=%s",
		url.QueryEscape(clientID),
		url.QueryEscape(state),
		url.QueryEscape(callbackURL),
	)
	return u, nil
}

// generateState creates a random CSRF state token and stores it with a 5-minute TTL.
func (s *GitHubOAuthService) generateState() string {
	b := make([]byte, 20)
	rand.Read(b)
	state := hex.EncodeToString(b)

	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	// Purge expired states.
	now := time.Now()
	for k, exp := range s.states {
		if now.After(exp) {
			delete(s.states, k)
		}
	}

	s.states[state] = now.Add(5 * time.Minute)
	return state
}

// ValidateState checks and consumes a CSRF state token. Returns false if invalid/expired.
func (s *GitHubOAuthService) ValidateState(state string) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	exp, ok := s.states[state]
	if !ok {
		return false
	}
	delete(s.states, state)
	return time.Now().Before(exp)
}

// ── OAuth Callback ──

// HandleCallback processes the GitHub OAuth callback.
// It exchanges the code for an access token (if provided) and stores the installation.
func (s *GitHubOAuthService) HandleCallback(code string, installationID int64) (*GitHubInstallation, error) {
	if installationID == 0 {
		return nil, fmt.Errorf("missing installation_id")
	}

	// Exchange code for user access token (optional — we mainly need installation_id).
	if code != "" {
		if _, err := s.exchangeCodeForToken(code); err != nil {
			s.logger.Warn("code exchange failed (non-fatal, installation_id is sufficient)", "err", err)
		}
	}

	// Fetch installation details from GitHub API.
	install, err := s.fetchInstallationInfo(installationID)
	if err != nil {
		// Save with minimal info if API call fails.
		install = &GitHubInstallation{
			InstallationID: installationID,
		}
	}

	// Upsert: update if installation_id already exists.
	var existing GitHubInstallation
	result := s.db.Where("installation_id = ?", installationID).First(&existing)
	if result.Error == nil {
		existing.AccountLogin = install.AccountLogin
		existing.AccountType = install.AccountType
		existing.AccountAvatarURL = install.AccountAvatarURL
		existing.UpdatedAt = time.Now()
		if err := s.db.Save(&existing).Error; err != nil {
			return nil, fmt.Errorf("update installation: %w", err)
		}
		return &existing, nil
	}

	// Only create if record was genuinely not found.
	if result.Error != nil && result.Error.Error() != "record not found" {
		return nil, fmt.Errorf("query installation: %w", result.Error)
	}

	if err := s.db.Create(install).Error; err != nil {
		return nil, fmt.Errorf("create installation: %w", err)
	}
	return install, nil
}

// exchangeCodeForToken exchanges an OAuth code for a user access token.
func (s *GitHubOAuthService) exchangeCodeForToken(code string) (string, error) {
	clientID := s.configStore.Get("github_client_id")
	clientSecret, err := s.decryptSecret("github_client_secret")
	if err != nil {
		return "", err
	}

	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
	}

	req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("token exchange: %s", result.Error)
	}
	return result.AccessToken, nil
}

// fetchInstallationInfo calls the GitHub API to get installation account details.
func (s *GitHubOAuthService) fetchInstallationInfo(installationID int64) (*GitHubInstallation, error) {
	appIDStr := s.configStore.Get("github_app_id")
	appID, _ := strconv.ParseInt(appIDStr, 10, 64)
	if appID == 0 {
		return nil, fmt.Errorf("github_app_id not configured")
	}

	privateKey, err := s.decryptSecret("github_private_key")
	if err != nil {
		return nil, err
	}

	jwt, err := s.ghApp.GenerateJWT(appID, privateKey)
	if err != nil {
		return nil, fmt.Errorf("generate JWT: %w", err)
	}

	req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/app/installations/%d", installationID), nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(body))
	}

	var data struct {
		ID      int64 `json:"id"`
		Account struct {
			Login     string `json:"login"`
			Type      string `json:"type"`
			AvatarURL string `json:"avatar_url"`
		} `json:"account"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse installation info: %w", err)
	}

	return &GitHubInstallation{
		InstallationID:   data.ID,
		AccountLogin:     data.Account.Login,
		AccountType:      data.Account.Type,
		AccountAvatarURL: data.Account.AvatarURL,
	}, nil
}

// ── Installation Management ──

// ListInstallations returns all stored GitHub App installations.
func (s *GitHubOAuthService) ListInstallations() ([]GitHubInstallation, error) {
	var installations []GitHubInstallation
	if err := s.db.Order("updated_at DESC").Find(&installations).Error; err != nil {
		return nil, err
	}
	return installations, nil
}

// DeleteInstallation removes a stored installation.
func (s *GitHubOAuthService) DeleteInstallation(id uint) error {
	return s.db.Delete(&GitHubInstallation{}, id).Error
}

// ── Repository Listing ──

// GitHubRepo represents a repository returned by the GitHub API.
type GitHubRepo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Name          string `json:"name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	Language      string `json:"language"`
	Description   string `json:"description"`
	UpdatedAt     string `json:"updated_at"`
}

// ListRepos returns the repositories accessible through a given installation.
func (s *GitHubOAuthService) ListRepos(installationID int64) ([]GitHubRepo, error) {
	token, err := s.GetInstallationToken(installationID)
	if err != nil {
		return nil, err
	}

	var allRepos []GitHubRepo
	page := 1

	for {
		u := fmt.Sprintf("https://api.github.com/installation/repositories?per_page=100&page=%d", page)
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if err != nil {
			return nil, fmt.Errorf("list repos request: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(body))
		}

		var result struct {
			Repositories []GitHubRepo `json:"repositories"`
			TotalCount   int          `json:"total_count"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("parse repos response: %w", err)
		}

		allRepos = append(allRepos, result.Repositories...)

		if len(allRepos) >= result.TotalCount || len(result.Repositories) == 0 {
			break
		}
		page++
		if page > 10 { // safety limit
			break
		}
	}

	return allRepos, nil
}

// GetInstallationToken obtains a GitHub App installation access token
// using the globally configured App credentials.
func (s *GitHubOAuthService) GetInstallationToken(installationID int64) (string, error) {
	appIDStr := s.configStore.Get("github_app_id")
	appID, _ := strconv.ParseInt(appIDStr, 10, 64)
	if appID == 0 {
		return "", fmt.Errorf("github_app_id not configured")
	}

	privateKey, err := s.decryptSecret("github_private_key")
	if err != nil {
		return "", err
	}

	return s.ghApp.GetCloneToken(appID, privateKey, installationID)
}
