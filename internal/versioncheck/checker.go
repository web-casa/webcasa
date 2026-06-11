package versioncheck

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// pubKeyEnv is the env var holding a base64-encoded ed25519 public key used to
// verify a detached signature over the fetched manifest bytes. When set,
// fetchManifest requires a valid signature served alongside the manifest
// (manifestURL + ".sig", a base64-encoded ed25519 signature). When unset,
// manifest fetching keeps its prior behavior but is logged as unverified.
const pubKeyEnv = "WEBCASA_VERSIONCHECK_PUBKEY"

// RemoteVersions is the top-level structure of the remote versions.json manifest.
type RemoteVersions struct {
	SchemaVersion int                        `json:"schema_version"`
	UpdatedAt     string                     `json:"updated_at"`
	Dependencies  map[string]json.RawMessage `json:"dependencies"`
}

// ToolVersion represents version info for a single tool (kopia, docker, etc.).
type ToolVersion struct {
	Recommended    string            `json:"recommended"`
	Minimum        string            `json:"minimum"`
	InstallScripts map[string]string `json:"install_scripts,omitempty"`
}

// CheckResult holds the comparison between local and remote versions for a tool.
type CheckResult struct {
	Tool               string `json:"tool"`
	LocalVersion       string `json:"local_version"`
	RecommendedVersion string `json:"recommended_version"`
	UpdateAvailable    bool   `json:"update_available"`
	Installed          bool   `json:"installed"`
}

// Checker periodically fetches the remote version manifest and compares
// with locally installed tool versions.
type Checker struct {
	manifestURL string
	cache       *RemoteVersions
	cacheTime   time.Time
	results     []CheckResult
	mu          sync.RWMutex
	stopCh      chan struct{}
	logger      *slog.Logger
}

// NewChecker creates a Checker that polls the given manifest URL.
func NewChecker(manifestURL string, logger *slog.Logger) *Checker {
	return &Checker{
		manifestURL: manifestURL,
		stopCh:      make(chan struct{}),
		logger:      logger.With("module", "versioncheck"),
	}
}

// Start begins the background polling goroutine.
// It waits 30 seconds before the first check, then repeats every 72 hours.
func (c *Checker) Start() {
	go func() {
		select {
		case <-time.After(30 * time.Second):
		case <-c.stopCh:
			return
		}

		c.refresh()

		ticker := time.NewTicker(72 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.refresh()
			case <-c.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the background polling goroutine.
func (c *Checker) Stop() {
	select {
	case <-c.stopCh:
		// already closed
	default:
		close(c.stopCh)
	}
}

// GetResults returns the cached check results.
func (c *Checker) GetResults() []CheckResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.results == nil {
		return []CheckResult{}
	}
	out := make([]CheckResult, len(c.results))
	copy(out, c.results)
	return out
}

// GetManifest returns the cached remote manifest (nil if not yet fetched).
func (c *Checker) GetManifest() *RemoteVersions {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache
}

// GetToolVersion parses and returns the ToolVersion for a specific dependency key.
func (c *Checker) GetToolVersion(tool string) *ToolVersion {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return nil
	}

	raw, ok := c.cache.Dependencies[tool]
	if !ok {
		return nil
	}

	var tv ToolVersion
	if err := json.Unmarshal(raw, &tv); err != nil {
		return nil
	}
	return &tv
}

// refresh fetches the remote manifest and checks local versions.
func (c *Checker) refresh() {
	c.logger.Info("checking remote version manifest")

	manifest, err := c.fetchManifest()
	if err != nil {
		c.logger.Error("failed to fetch version manifest", "err", err)
		return
	}

	c.mu.Lock()
	c.cache = manifest
	c.cacheTime = time.Now()
	c.mu.Unlock()

	results := c.checkLocal(manifest)

	c.mu.Lock()
	c.results = results
	c.mu.Unlock()

	for _, r := range results {
		if r.UpdateAvailable {
			c.logger.Info("update available", "tool", r.Tool, "local", r.LocalVersion, "recommended", r.RecommendedVersion)
		}
	}
}

// fetchManifest downloads and parses the remote versions.json.
func (c *Checker) fetchManifest() (*RemoteVersions, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(c.manifestURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Defense in depth: verify a detached ed25519 signature over the raw
	// manifest bytes when a public key is configured. Reject on mismatch so a
	// MITM'd or compromised manifest host cannot feed us a forged manifest.
	// When unconfigured this is a no-op (current behavior) but is logged so
	// operators know the manifest is unverified.
	if err := c.verifyManifest(body); err != nil {
		return nil, fmt.Errorf("verify manifest: %w", err)
	}

	var manifest RemoteVersions
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return &manifest, nil
}

// verifyManifest validates a detached ed25519 signature over body when a public
// key is configured via WEBCASA_VERSIONCHECK_PUBKEY (base64). The signature is
// fetched from manifestURL + ".sig" (base64-encoded). Returns nil (allowing the
// fetch) when no key is configured, logging that the manifest is unverified.
func (c *Checker) verifyManifest(body []byte) error {
	keyB64 := strings.TrimSpace(os.Getenv(pubKeyEnv))
	if keyB64 == "" {
		c.logger.Warn("manifest signature verification disabled (no public key configured); manifest is unverified", "env", pubKeyEnv)
		return nil
	}

	pubKey, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return fmt.Errorf("decode public key: %w", err)
	}
	if len(pubKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size %d (want %d)", len(pubKey), ed25519.PublicKeySize)
	}

	sig, err := c.fetchSignature()
	if err != nil {
		return fmt.Errorf("fetch signature: %w", err)
	}

	if !ed25519.Verify(ed25519.PublicKey(pubKey), body, sig) {
		return fmt.Errorf("signature does not match manifest")
	}
	c.logger.Info("manifest signature verified")
	return nil
}

// fetchSignature downloads the detached signature (base64-encoded ed25519) from
// manifestURL + ".sig". The signature blob is capped to a small size.
func (c *Checker) fetchSignature() ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(c.manifestURL + ".sig")
	if err != nil {
		return nil, fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096)) // signatures are tiny
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("invalid signature size %d (want %d)", len(sig), ed25519.SignatureSize)
	}
	return sig, nil
}

// checkLocal detects locally installed tools and compares with the manifest.
func (c *Checker) checkLocal(manifest *RemoteVersions) []CheckResult {
	var results []CheckResult

	// Check kopia
	if raw, ok := manifest.Dependencies["kopia"]; ok {
		var tv ToolVersion
		if json.Unmarshal(raw, &tv) == nil {
			r := CheckResult{
				Tool:               "kopia",
				RecommendedVersion: tv.Recommended,
			}
			ver, err := getCommandVersion("kopia", "--version")
			if err == nil && ver != "" {
				r.Installed = true
				r.LocalVersion = ver
				r.UpdateAvailable = semverLessThan(ver, tv.Recommended)
			}
			results = append(results, r)
		}
	}

	// Check docker
	if raw, ok := manifest.Dependencies["docker"]; ok {
		var tv ToolVersion
		if json.Unmarshal(raw, &tv) == nil {
			r := CheckResult{
				Tool:               "docker",
				RecommendedVersion: tv.Recommended,
			}
			ver, err := getCommandVersion("docker", "version", "--format", "{{.Server.Version}}")
			if err == nil && ver != "" {
				r.Installed = true
				r.LocalVersion = ver
				r.UpdateAvailable = semverLessThan(ver, tv.Recommended)
			}
			results = append(results, r)
		}
	}

	return results
}

// semverLessThan returns true if version a is strictly less than version b.
// Handles versions like "27.5.1", "0.18.2", "1.2.3-beta". Pre-release suffixes are stripped.
func semverLessThan(a, b string) bool {
	parseVer := func(v string) (int, int, int) {
		// Strip leading "v" and any pre-release suffix (e.g. "-beta")
		v = strings.TrimPrefix(v, "v")
		if idx := strings.IndexByte(v, '-'); idx >= 0 {
			v = v[:idx]
		}
		parts := strings.SplitN(v, ".", 3)
		major, _ := strconv.Atoi(safeIndex(parts, 0))
		minor, _ := strconv.Atoi(safeIndex(parts, 1))
		patch, _ := strconv.Atoi(safeIndex(parts, 2))
		return major, minor, patch
	}
	aMaj, aMin, aPat := parseVer(a)
	bMaj, bMin, bPat := parseVer(b)
	if aMaj != bMaj {
		return aMaj < bMaj
	}
	if aMin != bMin {
		return aMin < bMin
	}
	return aPat < bPat
}

func safeIndex(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "0"
}

// getCommandVersion runs a command and returns the first line of output, trimmed.
func getCommandVersion(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Take the first line and trim whitespace.
	ver := strings.TrimSpace(string(out))
	if idx := strings.IndexByte(ver, '\n'); idx >= 0 {
		ver = ver[:idx]
	}
	return ver, nil
}
