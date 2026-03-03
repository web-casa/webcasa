package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Service orchestrates backup operations, configuration, and scheduling.
type Service struct {
	db        *gorm.DB
	dataDir   string
	logger    *slog.Logger
	kopia     *KopiaClient
	scheduler *Scheduler
	mu        sync.Mutex
	running   bool
}

// NewService creates a new backup Service.
func NewService(db *gorm.DB, dataDir string, logger *slog.Logger) *Service {
	svc := &Service{
		db:        db,
		dataDir:   dataDir,
		logger:    logger,
		kopia:     NewKopiaClient(dataDir, logger),
		scheduler: NewScheduler(logger),
	}

	// Wire scheduler callback.
	svc.scheduler.SetCallback(func() {
		svc.RunBackup("scheduled")
	})

	return svc
}

// CheckDependency returns the Kopia CLI availability status.
func (s *Service) CheckDependency() KopiaStatus {
	return s.kopia.CheckKopia()
}

// Start starts the scheduler and loads the current schedule from config.
func (s *Service) Start() error {
	s.scheduler.Start()

	cfg, err := s.GetConfig()
	if err != nil {
		return nil // no config yet, fine
	}

	if cfg.ScheduleEnabled && cfg.CronExpr != "" {
		if err := s.scheduler.UpdateSchedule(cfg.CronExpr, true); err != nil {
			s.logger.Error("load schedule", "err", err)
		}
	}

	return nil
}

// Stop shuts down the scheduler.
func (s *Service) Stop() {
	s.scheduler.Stop()
}

// ── Config ──

// GetConfig returns the backup configuration (creates default if not exists).
func (s *Service) GetConfig() (*BackupConfig, error) {
	var cfg BackupConfig
	err := s.db.First(&cfg, 1).Error
	if err == gorm.ErrRecordNotFound {
		cfg = BackupConfig{
			ID:         1,
			TargetType: "local",
			LocalPath:  "/var/backups/webcasa",
			CronExpr:   "0 2 * * *",
			RetainCount: 10,
			RetainDays:  30,
			Scopes:     JSONArray{"panel"},
		}
		if err := s.db.Create(&cfg).Error; err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	return &cfg, err
}

// UpdateConfig updates the backup configuration.
func (s *Service) UpdateConfig(req *UpdateConfigRequest) (*BackupConfig, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{}

	if req.TargetType != "" {
		updates["target_type"] = req.TargetType
	}
	if req.LocalPath != "" {
		updates["local_path"] = req.LocalPath
	}
	if req.S3Endpoint != "" {
		updates["s3_endpoint"] = req.S3Endpoint
	}
	if req.S3Bucket != "" {
		updates["s3_bucket"] = req.S3Bucket
	}
	if req.S3AccessKey != "" {
		updates["s3_access_key"] = req.S3AccessKey
	}
	if req.S3SecretKey != "" {
		updates["s3_secret_key"] = req.S3SecretKey
	}
	if req.S3Region != "" {
		updates["s3_region"] = req.S3Region
	}
	if req.WebdavURL != "" {
		updates["webdav_url"] = req.WebdavURL
	}
	if req.WebdavUser != "" {
		updates["webdav_user"] = req.WebdavUser
	}
	if req.WebdavPassword != "" {
		updates["webdav_password"] = req.WebdavPassword
	}
	if req.SftpHost != "" {
		updates["sftp_host"] = req.SftpHost
	}
	if req.SftpPort > 0 {
		updates["sftp_port"] = req.SftpPort
	}
	if req.SftpUser != "" {
		updates["sftp_user"] = req.SftpUser
	}
	if req.SftpPassword != "" {
		updates["sftp_password"] = req.SftpPassword
	}
	if req.SftpKeyPath != "" {
		updates["sftp_key_path"] = req.SftpKeyPath
	}
	if req.SftpPath != "" {
		updates["sftp_path"] = req.SftpPath
	}
	if req.ScheduleEnabled != nil {
		updates["schedule_enabled"] = *req.ScheduleEnabled
	}
	if req.CronExpr != "" {
		updates["cron_expr"] = req.CronExpr
	}
	if req.RetainCount > 0 {
		updates["retain_count"] = req.RetainCount
	}
	if req.RetainDays > 0 {
		updates["retain_days"] = req.RetainDays
	}
	if len(req.Scopes) > 0 {
		data, _ := JSONArray(req.Scopes).Value()
		updates["scopes"] = data
	}
	if req.RepoPassword != "" {
		updates["repo_password"] = req.RepoPassword
	}

	if len(updates) > 0 {
		if err := s.db.Model(cfg).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	// Reload and update scheduler.
	cfg, _ = s.GetConfig()
	schedEnabled := cfg.ScheduleEnabled
	if req.ScheduleEnabled != nil {
		schedEnabled = *req.ScheduleEnabled
	}
	if err := s.scheduler.UpdateSchedule(cfg.CronExpr, schedEnabled); err != nil {
		s.logger.Error("update schedule", "err", err)
	}

	return cfg, nil
}

// TestConnection tests the backup target connection.
func (s *Service) TestConnection() error {
	if status := s.kopia.CheckKopia(); !status.Available {
		return fmt.Errorf("Kopia is not installed. Please install it first: %s", kopiaInstallInstructions["generic"])
	}

	cfg, err := s.GetConfig()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// If repo not initialized, try to create it.
	if !cfg.RepoInitialized {
		if err := s.kopia.InitRepository(ctx, cfg); err != nil {
			// Maybe already exists, try connecting.
			if connErr := s.kopia.ConnectRepository(ctx, cfg); connErr != nil {
				return fmt.Errorf("init: %v, connect: %v", err, connErr)
			}
		}
		s.db.Model(cfg).Update("repo_initialized", true)
		return nil
	}

	return s.kopia.TestConnection(ctx, cfg)
}

// ── Backup Operations ──

// RunBackup executes a backup operation.
func (s *Service) RunBackup(trigger string) (*BackupSnapshot, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, fmt.Errorf("a backup is already running")
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	cfg, err := s.GetConfig()
	if err != nil {
		return nil, err
	}

	startTime := time.Now()
	snapshot := &BackupSnapshot{
		Status:  "running",
		Scopes:  cfg.Scopes,
		Trigger: trigger,
	}
	s.db.Create(snapshot)

	s.addLog(snapshot.ID, "info", "Backup started (trigger: "+trigger+")")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Ensure repository is connected.
	if !cfg.RepoInitialized {
		if err := s.kopia.InitRepository(ctx, cfg); err != nil {
			if connErr := s.kopia.ConnectRepository(ctx, cfg); connErr != nil {
				s.failSnapshot(snapshot, fmt.Sprintf("repository init/connect failed: %v / %v", err, connErr))
				return snapshot, nil
			}
		}
		s.db.Model(cfg).Update("repo_initialized", true)
	} else {
		if err := s.kopia.ConnectRepository(ctx, cfg); err != nil {
			s.failSnapshot(snapshot, "repository connect failed: "+err.Error())
			return snapshot, nil
		}
	}

	// Prepare backup source directory.
	sourceDir := filepath.Join(s.dataDir, "staging")
	os.RemoveAll(sourceDir)
	os.MkdirAll(sourceDir, 0755)
	defer os.RemoveAll(sourceDir) // always clean up staging, even on failure

	// Collect scopes.
	for _, scope := range cfg.Scopes {
		switch scope {
		case "panel":
			s.backupPanel(sourceDir, snapshot.ID)
		case "docker":
			s.backupDocker(sourceDir, snapshot.ID)
		case "database":
			s.backupDatabases(sourceDir, snapshot.ID)
		}
	}

	// Create Kopia snapshot.
	snapshotID, sizeBytes, err := s.kopia.CreateSnapshot(ctx, sourceDir)
	if err != nil {
		s.failSnapshot(snapshot, "kopia snapshot failed: "+err.Error())
		return snapshot, nil
	}

	// Update retention policy.
	_ = s.kopia.SetRetention(ctx, cfg.RetainCount, cfg.RetainDays)

	duration := time.Since(startTime).Seconds()
	s.db.Model(snapshot).Updates(map[string]interface{}{
		"status":      "completed",
		"snapshot_id": snapshotID,
		"size_bytes":  sizeBytes,
		"duration":    duration,
	})

	s.addLog(snapshot.ID, "info", fmt.Sprintf("Backup completed in %.1fs (snapshot: %s)", duration, snapshotID))
	s.logger.Info("backup completed", "snapshot", snapshotID, "duration", duration)

	return snapshot, nil
}

// RestoreSnapshot restores from a snapshot.
func (s *Service) RestoreSnapshot(snapshotDBID uint) error {
	var snap BackupSnapshot
	if err := s.db.First(&snap, snapshotDBID).Error; err != nil {
		return err
	}
	if snap.SnapshotID == "" {
		return fmt.Errorf("snapshot has no Kopia ID")
	}

	cfg, err := s.GetConfig()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Connect to repo.
	if err := s.kopia.ConnectRepository(ctx, cfg); err != nil {
		return fmt.Errorf("connect repository: %w", err)
	}

	// Restore to a temp directory.
	restoreDir := filepath.Join(s.dataDir, "restore-"+fmt.Sprintf("%d", snap.ID))
	os.MkdirAll(restoreDir, 0755)

	if err := s.kopia.RestoreSnapshot(ctx, snap.SnapshotID, restoreDir); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	s.addLog(snap.ID, "info", "Snapshot restored to: "+restoreDir)
	return nil
}

// DeleteSnapshot removes a snapshot.
func (s *Service) DeleteSnapshot(snapshotDBID uint) error {
	var snap BackupSnapshot
	if err := s.db.First(&snap, snapshotDBID).Error; err != nil {
		return err
	}

	if snap.SnapshotID != "" {
		cfg, err := s.GetConfig()
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_ = s.kopia.ConnectRepository(ctx, cfg)
			_ = s.kopia.DeleteSnapshot(ctx, snap.SnapshotID)
		}
	}

	// Delete logs.
	s.db.Where("snapshot_id = ?", snapshotDBID).Delete(&BackupLog{})
	return s.db.Delete(&BackupSnapshot{}, snapshotDBID).Error
}

// ── Query Methods ──

// ListSnapshots returns all backup snapshots.
func (s *Service) ListSnapshots() ([]BackupSnapshot, error) {
	var snapshots []BackupSnapshot
	if err := s.db.Order("created_at DESC").Find(&snapshots).Error; err != nil {
		return nil, err
	}
	return snapshots, nil
}

// GetStatus returns the current backup status.
func (s *Service) GetStatus() (*BackupStatus, error) {
	status := &BackupStatus{}

	s.mu.Lock()
	status.Running = s.running
	s.mu.Unlock()

	var last BackupSnapshot
	if err := s.db.Order("created_at DESC").First(&last).Error; err == nil {
		status.LastBackup = &last
	}

	entry := s.scheduler.NextRun()
	if entry != nil && !entry.Next.IsZero() {
		status.NextRunTime = &entry.Next
	}

	return status, nil
}

// ListLogs returns backup logs, optionally filtered by snapshot.
func (s *Service) ListLogs(snapshotID uint, limit int) ([]BackupLog, error) {
	if limit <= 0 {
		limit = 100
	}
	q := s.db.Order("created_at DESC").Limit(limit)
	if snapshotID > 0 {
		q = q.Where("snapshot_id = ?", snapshotID)
	}
	var logs []BackupLog
	if err := q.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// ── Backup Scope Helpers ──

// backupPanel backs up the panel database and Caddyfile.
func (s *Service) backupPanel(destDir string, snapID uint) {
	panelDir := filepath.Join(destDir, "panel")
	os.MkdirAll(panelDir, 0755)

	// Copy SQLite DB using VACUUM INTO for consistency.
	dbPath := os.Getenv("WEBCASA_DB_PATH")
	if dbPath == "" {
		dbPath = "/opt/webcasa/data/webcasa.db"
	}
	if _, err := os.Stat(dbPath); err == nil {
		vacuumDest := filepath.Join(panelDir, "webcasa.db")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sqlite3", dbPath, fmt.Sprintf("VACUUM INTO '%s';", vacuumDest))
		if output, err := cmd.CombinedOutput(); err != nil {
			s.addLog(snapID, "warn", "Panel DB backup failed: "+string(output))
		} else {
			s.addLog(snapID, "info", "Panel DB backed up")
		}
	}

	// Copy Caddyfile.
	caddyfilePath := os.Getenv("WEBCASA_CADDYFILE_PATH")
	if caddyfilePath == "" {
		caddyfilePath = "/etc/caddy/Caddyfile"
	}
	if data, err := os.ReadFile(caddyfilePath); err == nil {
		os.WriteFile(filepath.Join(panelDir, "Caddyfile"), data, 0644)
		s.addLog(snapID, "info", "Caddyfile backed up")
	}
}

// backupDocker backs up Docker volume data.
func (s *Service) backupDocker(destDir string, snapID uint) {
	dockerDir := filepath.Join(destDir, "docker")
	os.MkdirAll(dockerDir, 0755)

	// List Docker volumes.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "--format", "{{.Name}}")
	output, err := cmd.Output()
	if err != nil {
		s.addLog(snapID, "warn", "Failed to list Docker volumes: "+err.Error())
		return
	}

	volumes := 0
	for _, name := range splitLines(string(output)) {
		if name == "" {
			continue
		}
		volDir := filepath.Join(dockerDir, name)
		os.MkdirAll(volDir, 0755)

		// Use docker run to copy volume contents.
		cpCtx, cpCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		cpCmd := exec.CommandContext(cpCtx, "docker", "run", "--rm",
			"-v", name+":/source:ro",
			"-v", volDir+":/dest",
			"alpine", "cp", "-a", "/source/.", "/dest/")
		if out, err := cpCmd.CombinedOutput(); err != nil {
			s.addLog(snapID, "warn", fmt.Sprintf("Volume %s backup failed: %s", name, string(out)))
		} else {
			volumes++
		}
		cpCancel()
	}

	s.addLog(snapID, "info", fmt.Sprintf("Docker volumes backed up: %d", volumes))
}

// backupDatabases performs database dumps for running database instances.
func (s *Service) backupDatabases(destDir string, snapID uint) {
	dbDir := filepath.Join(destDir, "databases")
	os.MkdirAll(dbDir, 0755)

	// Get running database containers (matching webcasa-db-* naming).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "ps", "--filter", "name=webcasa-db-",
		"--format", "{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		s.addLog(snapID, "warn", "Failed to list database containers: "+err.Error())
		return
	}

	count := 0
	for _, name := range splitLines(string(output)) {
		if name == "" {
			continue
		}

		dumpFile := filepath.Join(dbDir, name+".sql")
		var dumpCmd *exec.Cmd

		dumpCtx, dumpCancel := context.WithTimeout(context.Background(), 10*time.Minute)

		// Detect engine by container name or environment.
		if containsAny(name, "mysql", "mariadb") {
			dumpCmd = exec.CommandContext(dumpCtx, "docker", "exec", name,
				"mysqldump", "--all-databases", "-u", "root",
				"--password="+os.Getenv("WEBCASA_DB_ROOT_PASS"))
		} else if containsAny(name, "postgres") {
			dumpCmd = exec.CommandContext(dumpCtx, "docker", "exec", name,
				"pg_dumpall", "-U", "postgres")
		} else {
			dumpCancel()
			continue
		}

		dumpOutput, err := dumpCmd.Output()
		if err != nil {
			s.addLog(snapID, "warn", fmt.Sprintf("Database dump %s failed: %v", name, err))
		} else {
			os.WriteFile(dumpFile, dumpOutput, 0600)
			count++
		}
		dumpCancel()
	}

	s.addLog(snapID, "info", fmt.Sprintf("Database dumps completed: %d", count))
}

// ── Internal Helpers ──

func (s *Service) failSnapshot(snap *BackupSnapshot, errMsg string) {
	s.db.Model(snap).Updates(map[string]interface{}{
		"status":    "failed",
		"error_msg": errMsg,
	})
	s.addLog(snap.ID, "error", errMsg)
	s.logger.Error("backup failed", "err", errMsg)
}

func (s *Service) addLog(snapshotID uint, level, message string) {
	s.db.Create(&BackupLog{
		SnapshotID: snapshotID,
		Level:      level,
		Message:    message,
	})
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range filepath.SplitList(s) {
		lines = append(lines, line)
	}
	// filepath.SplitList doesn't work well for newlines; use simple split.
	result := []string{}
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

func containsAny(s string, substrs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}
