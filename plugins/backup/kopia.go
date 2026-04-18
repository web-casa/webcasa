package backup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/web-casa/webcasa/internal/execx"
	"github.com/web-casa/webcasa/internal/versions"
)

// KopiaClient wraps the Kopia CLI for repository and snapshot operations.
type KopiaClient struct {
	configFile string // --config-file path for isolation
	logger     *slog.Logger
}

// NewKopiaClient creates a KopiaClient with a config file in the given data directory.
func NewKopiaClient(dataDir string, logger *slog.Logger) *KopiaClient {
	return &KopiaClient{
		configFile: filepath.Join(dataDir, "kopia.config"),
		logger:     logger,
	}
}

// InstallKopia runs the Kopia installation commands and streams output via
// the provided write functions. It detects the OS family and picks the
// appropriate install commands. Returns true on success.
//
// ctx should be the inbound HTTP request context so an SSE client
// disconnect kills the install subprocess tree (kopia install can pull
// packages, which is slow and wasteful to keep running after the admin
// closed the tab).
func (k *KopiaClient) InstallKopia(ctx context.Context, writeSSE func(string), writeEvent func(string, string)) bool {
	// Check if already installed.
	if status := k.CheckKopia(); status.Available {
		writeSSE("Kopia is already installed: " + status.Version)
		writeEvent("done", "ok")
		return true
	}

	// Detect OS family.
	osFamily := detectOSFamily()
	writeSSE("Detected OS family: " + osFamily)

	var installCmd string
	switch osFamily {
	case "debian":
		installCmd = kopiaInstallInstructions["debian"]
	case "rhel":
		installCmd = buildRHELInstallCmd()
	default:
		writeSSE("ERROR: Unsupported OS family: " + osFamily)
		writeSSE("Please install Kopia manually: https://kopia.io/docs/installation/")
		writeEvent("error", "Unsupported OS family")
		return false
	}

	writeSSE("Installing Kopia...")

	cmd := execx.BashContext(ctx, installCmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeSSE("ERROR: " + err.Error())
		writeEvent("error", err.Error())
		return false
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		writeSSE("ERROR: " + err.Error())
		writeEvent("error", err.Error())
		return false
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			writeSSE(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		writeSSE("ERROR: Installation failed: " + err.Error())
		writeEvent("error", "Installation failed: "+err.Error())
		return false
	}

	// Verify installation.
	status := k.CheckKopia()
	if !status.Available {
		writeSSE("ERROR: Kopia binary not found after installation")
		writeEvent("error", "Kopia not found after install")
		return false
	}

	writeSSE("Kopia installed successfully: " + status.Version)
	writeEvent("done", "ok")
	return true
}

// detectOSFamily reads /etc/os-release to determine the OS family.
// Returns "debian", "rhel", or "unknown".
func detectOSFamily() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	content := strings.ToLower(string(data))

	// Check ID_LIKE first, then ID.
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id_like=") {
			val := strings.Trim(strings.TrimPrefix(line, "id_like="), "\"")
			if strings.Contains(val, "debian") || strings.Contains(val, "ubuntu") {
				return "debian"
			}
			if strings.Contains(val, "rhel") || strings.Contains(val, "fedora") || strings.Contains(val, "centos") {
				return "rhel"
			}
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id=") {
			val := strings.Trim(strings.TrimPrefix(line, "id="), "\"")
			switch val {
			case "debian", "ubuntu", "linuxmint", "pop", "kali", "deepin":
				return "debian"
			case "rhel", "centos", "fedora", "rocky", "almalinux", "ol", "amzn":
				return "rhel"
			}
		}
	}
	return "unknown"
}

// ── Repository Operations ──

// KopiaStatus represents the availability of the Kopia CLI.
type KopiaStatus struct {
	Available           bool              `json:"available"`
	Version             string            `json:"version,omitempty"`
	InstallInstructions map[string]string `json:"install_instructions,omitempty"`
}

// kopiaVersion references the centrally pinned Kopia version.
var kopiaVersion = versions.Kopia

// kopiaInstallInstructions provides install commands per OS family.
// The "rhel" entry is built dynamically — see buildRHELInstallCmd().
var kopiaInstallInstructions = map[string]string{
	"debian":  "curl -s https://kopia.io/signing-key | sudo gpg --dearmor -o /etc/apt/keyrings/kopia-keyring.gpg && echo 'deb [signed-by=/etc/apt/keyrings/kopia-keyring.gpg] http://packages.kopia.io/apt/ stable main' | sudo tee /etc/apt/sources.list.d/kopia.list && sudo apt update && sudo apt install -y kopia",
	"generic": "Visit https://kopia.io/docs/installation/ for installation instructions",
}

// rpmArch maps Go's GOARCH to Kopia RPM architecture suffixes.
// Actual filenames: kopia-0.22.3.x86_64.rpm, kopia-0.22.3.aarch64.rpm, kopia-0.22.3.armhfp.rpm
var rpmArch = map[string]string{
	"amd64": "x86_64",
	"arm64": "aarch64",
	"arm":   "armhfp",
}

// buildRHELInstallCmd returns the dnf install command with the exact RPM URL
// for the current architecture.
func buildRHELInstallCmd() string {
	arch := rpmArch[runtime.GOARCH]
	if arch == "" {
		arch = runtime.GOARCH
	}
	rpmURL := fmt.Sprintf(
		"https://github.com/kopia/kopia/releases/download/v%s/kopia-%s.%s.rpm",
		kopiaVersion, kopiaVersion, arch,
	)
	return fmt.Sprintf("sudo rpm --import https://kopia.io/signing-key && sudo dnf install -y %s", rpmURL)
}

// CheckKopia checks if the Kopia CLI is available in PATH.
func (k *KopiaClient) CheckKopia() KopiaStatus {
	cmd := exec.Command("kopia", "--version")
	output, err := cmd.Output()
	if err != nil {
		return KopiaStatus{
			Available:           false,
			InstallInstructions: kopiaInstallInstructions,
		}
	}
	return KopiaStatus{
		Available: true,
		Version:   strings.TrimSpace(string(output)),
	}
}

// InitRepository initialises a new Kopia repository at the specified target.
func (k *KopiaClient) InitRepository(ctx context.Context, cfg *BackupConfig) error {
	args := []string{"repository", "create"}
	args = append(args, k.targetArgs(cfg)...)

	_, err := k.runWithEnv(ctx, k.secretEnv(cfg), args...)
	return err
}

// ConnectRepository connects to an existing Kopia repository.
func (k *KopiaClient) ConnectRepository(ctx context.Context, cfg *BackupConfig) error {
	args := []string{"repository", "connect"}
	args = append(args, k.targetArgs(cfg)...)

	_, err := k.runWithEnv(ctx, k.secretEnv(cfg), args...)
	return err
}

// DisconnectRepository disconnects from the current repository.
func (k *KopiaClient) DisconnectRepository(ctx context.Context) error {
	_, err := k.run(ctx, "repository", "disconnect")
	return err
}

// TestConnection tests whether a connection to the repository can be established.
func (k *KopiaClient) TestConnection(ctx context.Context, cfg *BackupConfig) error {
	args := []string{"repository", "status"}
	_, err := k.run(ctx, args...)
	if err != nil {
		// Try connecting first.
		if connErr := k.ConnectRepository(ctx, cfg); connErr != nil {
			return fmt.Errorf("connect failed: %w", connErr)
		}
		_, err = k.run(ctx, args...)
	}
	return err
}

// ── Snapshot Operations ──

// kopiaSnapshotOutput represents the JSON output of `kopia snapshot create`.
type kopiaSnapshotOutput struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	TotalSize int64  `json:"totalSize"`
}

// CreateSnapshot creates a new snapshot for the given source path.
func (k *KopiaClient) CreateSnapshot(ctx context.Context, sourcePath string) (string, int64, error) {
	output, err := k.run(ctx, "snapshot", "create", sourcePath, "--json")
	if err != nil {
		return "", 0, err
	}

	// Try parsing JSON output for snapshot ID.
	var snap kopiaSnapshotOutput
	if jsonErr := json.Unmarshal([]byte(output), &snap); jsonErr == nil && snap.ID != "" {
		return snap.ID, snap.TotalSize, nil
	}

	// Fallback: extract ID from text output.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Created snapshot with root") || strings.Contains(line, "kopia") {
			parts := strings.Fields(line)
			for _, p := range parts {
				if len(p) > 10 && !strings.Contains(p, "/") {
					return p, 0, nil
				}
			}
		}
	}

	return "", 0, fmt.Errorf("failed to parse snapshot ID from kopia output")
}

// ListSnapshots returns a list of snapshot IDs.
func (k *KopiaClient) ListSnapshots(ctx context.Context) (string, error) {
	return k.run(ctx, "snapshot", "list", "--all")
}

// RestoreSnapshot restores a snapshot to the given target directory.
func (k *KopiaClient) RestoreSnapshot(ctx context.Context, snapshotID, targetDir string) error {
	if err := validateSnapshotID(snapshotID); err != nil {
		return err
	}
	_, err := k.run(ctx, "snapshot", "restore", snapshotID, targetDir)
	return err
}

// DeleteSnapshot removes a snapshot by ID.
func (k *KopiaClient) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	if err := validateSnapshotID(snapshotID); err != nil {
		return err
	}
	_, err := k.run(ctx, "snapshot", "delete", snapshotID, "--delete")
	return err
}

// validateSnapshotID ensures the snapshot ID looks like a hex hash
// and cannot be mistaken for a CLI flag (defense-in-depth).
func validateSnapshotID(id string) error {
	if id == "" {
		return fmt.Errorf("empty snapshot ID")
	}
	if strings.HasPrefix(id, "-") {
		return fmt.Errorf("invalid snapshot ID: %s", id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("invalid snapshot ID (non-hex character): %s", id)
		}
	}
	return nil
}

// ── Retention Policy ──

// SetRetention configures the retention policy.
func (k *KopiaClient) SetRetention(ctx context.Context, keepLatest, keepDays int) error {
	args := []string{
		"policy", "set", "--global",
		fmt.Sprintf("--keep-latest=%d", keepLatest),
		fmt.Sprintf("--keep-daily=%d", keepDays),
	}
	_, err := k.run(ctx, args...)
	return err
}

// ── Helpers ──

// targetArgs returns the storage subcommand and its flags for the repository target type.
// Kopia uses subcommands (e.g. `kopia repository create filesystem --path=...`),
// not a `--storage-type` flag.
// Secrets are passed via environment variables (see secretEnv) to avoid /proc exposure.
func (k *KopiaClient) targetArgs(cfg *BackupConfig) []string {
	switch cfg.TargetType {
	case "s3":
		args := []string{
			"s3",
			"--bucket=" + cfg.S3Bucket,
		}
		if cfg.S3Endpoint != "" {
			args = append(args, "--endpoint="+cfg.S3Endpoint)
		}
		if cfg.S3Region != "" {
			args = append(args, "--region="+cfg.S3Region)
		}
		return args

	case "webdav":
		args := []string{
			"webdav",
			"--url=" + cfg.WebdavURL,
		}
		if cfg.WebdavUser != "" {
			args = append(args, "--webdav-username="+cfg.WebdavUser)
		}
		if cfg.WebdavPassword != "" {
			args = append(args, "--webdav-password="+cfg.WebdavPassword)
		}
		return args

	case "sftp":
		args := []string{
			"sftp",
			"--host=" + cfg.SftpHost,
			"--port=" + fmt.Sprintf("%d", cfg.SftpPort),
			"--username=" + cfg.SftpUser,
			"--path=" + cfg.SftpPath,
		}
		if cfg.SftpKeyPath != "" {
			args = append(args, "--keyfile="+cfg.SftpKeyPath)
		}
		if cfg.SftpPassword != "" && cfg.SftpKeyPath == "" {
			args = append(args, "--sftp-password="+cfg.SftpPassword)
		}
		return args

	default: // "local"
		path := cfg.LocalPath
		if path == "" {
			path = "/var/backups/webcasa"
		}
		os.MkdirAll(path, 0755)
		return []string{
			"filesystem",
			"--path=" + path,
		}
	}
}

// secretEnv returns environment variables for passing secrets to Kopia CLI.
// This avoids exposing passwords/keys in /proc/PID/cmdline.
func (k *KopiaClient) secretEnv(cfg *BackupConfig) []string {
	env := []string{
		"KOPIA_PASSWORD=" + cfg.RepoPassword,
	}

	switch cfg.TargetType {
	case "s3":
		env = append(env,
			"AWS_ACCESS_KEY_ID="+cfg.S3AccessKey,
			"AWS_SECRET_ACCESS_KEY="+cfg.S3SecretKey,
		)
	case "webdav":
		// Kopia doesn't support env vars for webdav credentials natively,
		// so we still pass them as args but only for webdav.
	case "sftp":
		// SFTP password is passed via --sftp-password in targetArgs, not env var.
	}

	return env
}

// run executes a Kopia CLI command and returns stdout.
func (k *KopiaClient) run(ctx context.Context, args ...string) (string, error) {
	return k.runWithEnv(ctx, nil, args...)
}

// runWithEnv executes a Kopia CLI command with extra environment variables.
func (k *KopiaClient) runWithEnv(ctx context.Context, extraEnv []string, args ...string) (string, error) {
	fullArgs := append([]string{"--config-file=" + k.configFile}, args...)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kopia", fullArgs...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	k.logger.Debug("kopia command", "args", strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = stdout.String()
		}
		return "", fmt.Errorf("kopia %s: %s (%w)", args[0], strings.TrimSpace(errMsg), err)
	}

	return stdout.String(), nil
}
