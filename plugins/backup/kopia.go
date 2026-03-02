package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

// ── Repository Operations ──

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

	return "unknown", 0, nil
}

// ListSnapshots returns a list of snapshot IDs.
func (k *KopiaClient) ListSnapshots(ctx context.Context) (string, error) {
	return k.run(ctx, "snapshot", "list", "--all")
}

// RestoreSnapshot restores a snapshot to the given target directory.
func (k *KopiaClient) RestoreSnapshot(ctx context.Context, snapshotID, targetDir string) error {
	_, err := k.run(ctx, "snapshot", "restore", snapshotID, targetDir)
	return err
}

// DeleteSnapshot removes a snapshot by ID.
func (k *KopiaClient) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	_, err := k.run(ctx, "snapshot", "delete", snapshotID, "--delete")
	return err
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

// targetArgs builds the CLI arguments for the repository target type.
// Secrets are passed via environment variables (see secretEnv) to avoid /proc exposure.
func (k *KopiaClient) targetArgs(cfg *BackupConfig) []string {
	switch cfg.TargetType {
	case "s3":
		args := []string{
			"--storage-type=s3",
			"--s3.bucket=" + cfg.S3Bucket,
		}
		if cfg.S3Endpoint != "" {
			args = append(args, "--s3.endpoint="+cfg.S3Endpoint)
		}
		if cfg.S3Region != "" {
			args = append(args, "--s3.region="+cfg.S3Region)
		}
		return args

	case "webdav":
		return []string{
			"--storage-type=webdav",
			"--webdav.url=" + cfg.WebdavURL,
		}

	case "sftp":
		args := []string{
			"--storage-type=sftp",
			"--sftp.host=" + cfg.SftpHost,
			"--sftp.port=" + fmt.Sprintf("%d", cfg.SftpPort),
			"--sftp.username=" + cfg.SftpUser,
			"--sftp.path=" + cfg.SftpPath,
		}
		if cfg.SftpKeyPath != "" {
			args = append(args, "--sftp.keyfile="+cfg.SftpKeyPath)
		}
		return args

	default: // "local"
		path := cfg.LocalPath
		if path == "" {
			path = "/var/backups/webcasa"
		}
		os.MkdirAll(path, 0755)
		return []string{
			"--storage-type=filesystem",
			"--file.path=" + path,
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
		// SFTP password handled via sshpass or key file
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
