package database

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Service implements the business logic for database instance management.
type Service struct {
	db      *gorm.DB
	dataDir string
	logger  *slog.Logger
}

// NewService creates a database Service.
func NewService(db *gorm.DB, dataDir string, logger *slog.Logger) *Service {
	return &Service{db: db, dataDir: dataDir, logger: logger}
}

// ── Instance CRUD ──

// ListInstances returns all instances with live status.
func (s *Service) ListInstances() ([]Instance, error) {
	var instances []Instance
	if err := s.db.Order("id ASC").Find(&instances).Error; err != nil {
		return nil, err
	}
	for i := range instances {
		instances[i].Status = s.resolveInstanceStatus(&instances[i])
	}
	return instances, nil
}

// GetInstance returns a single instance with live status.
func (s *Service) GetInstance(id uint) (*Instance, error) {
	var inst Instance
	if err := s.db.First(&inst, id).Error; err != nil {
		return nil, err
	}
	inst.Status = s.resolveInstanceStatus(&inst)
	return &inst, nil
}

// CreateInstance creates a new database instance and optionally starts it.
func (s *Service) CreateInstance(req *CreateInstanceRequest) (*Instance, error) {
	// Validate engine.
	engineInfo := findEngine(req.Engine)
	if engineInfo == nil {
		return nil, fmt.Errorf("unsupported engine: %s", req.Engine)
	}

	// Require password for non-Redis engines.
	if req.Engine != EngineRedis && req.RootPassword == "" {
		return nil, fmt.Errorf("root_password is required for %s", req.Engine)
	}

	// Default version.
	version := req.Version
	if version == "" {
		version = engineInfo.Default
	}

	// Check name uniqueness.
	var count int64
	s.db.Model(&Instance{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("instance name %q already exists", req.Name)
	}

	// Validate that sanitized name is meaningful (not all special chars).
	safeName := sanitizeName(req.Name)
	if safeName == "unnamed" {
		return nil, fmt.Errorf("instance name must contain at least one letter or digit")
	}

	// Check slug collision: different names like "Foo!" and "Foo?" map to the same
	// container name and data directory, causing cross-interference.
	var existingInstances []Instance
	s.db.Select("name").Find(&existingInstances)
	for _, ex := range existingInstances {
		if sanitizeName(ex.Name) == safeName {
			return nil, fmt.Errorf("instance name %q conflicts with existing instance %q (same container name)", req.Name, ex.Name)
		}
	}

	// Allocate port — default to engine's standard port.
	port := req.Port
	if port == 0 {
		port = engineInfo.Port
	}
	// Validate port range.
	if port < 1024 || port > 65535 {
		return nil, fmt.Errorf("port must be between 1024 and 65535, got %d", port)
	}
	// Check for port conflicts with existing instances.
	var portConflict int64
	s.db.Model(&Instance{}).Where("port = ?", port).Count(&portConflict)
	if portConflict > 0 {
		return nil, fmt.Errorf("port %d is already in use by another instance", port)
	}

	// Memory limit — default 0.5g.
	memLimit := req.MemoryLimit
	if memLimit == "" {
		memLimit = "0.5g"
	}

	containerName := "webcasa-db-" + safeName

	// Serialize engine config to JSON.
	var configJSON string
	if req.Config != nil {
		if data, err := json.Marshal(req.Config); err == nil {
			configJSON = string(data)
		}
	}

	inst := &Instance{
		Name:          req.Name,
		Engine:        req.Engine,
		Version:       version,
		Status:        "stopped",
		Port:          port,
		RootPassword:  req.RootPassword,
		DataDir:       filepath.Join(s.dataDir, "instances", sanitizeName(req.Name)),
		ContainerName: containerName,
		MemoryLimit:   memLimit,
		Config:        configJSON,
	}

	// Create data directory.
	if err := os.MkdirAll(inst.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("create instance dir: %w", err)
	}

	// Generate and write compose file.
	composeContent := GenerateComposeFile(inst)
	composePath := filepath.Join(inst.DataDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0600); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}

	// Write .env with root password.
	envContent := fmt.Sprintf("ROOT_PASSWORD=%s\n", inst.RootPassword)
	envPath := filepath.Join(inst.DataDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return nil, fmt.Errorf("write env file: %w", err)
	}

	// Save to DB.
	if err := s.db.Create(inst).Error; err != nil {
		return nil, fmt.Errorf("create instance record: %w", err)
	}

	// Auto-start if requested.
	if req.AutoStart {
		if err := s.startInstance(inst); err != nil {
			// Rollback: remove DB record and data dir.
			s.db.Where("instance_id = ?", inst.ID).Delete(&Database{})
			s.db.Where("instance_id = ?", inst.ID).Delete(&DatabaseUser{})
			s.db.Delete(&Instance{}, inst.ID)
			os.RemoveAll(inst.DataDir)
			return nil, fmt.Errorf("auto-start failed: %w", err)
		}
	}

	return s.GetInstance(inst.ID)
}

// CreateInstanceStream creates a new database instance with progress callback for streaming output.
func (s *Service) CreateInstanceStream(req *CreateInstanceRequest, progressCb func(string)) (*Instance, error) {
	// Validate engine.
	engineInfo := findEngine(req.Engine)
	if engineInfo == nil {
		return nil, fmt.Errorf("unsupported engine: %s", req.Engine)
	}

	if req.Engine != EngineRedis && req.RootPassword == "" {
		return nil, fmt.Errorf("root_password is required for %s", req.Engine)
	}

	version := req.Version
	if version == "" {
		version = engineInfo.Default
	}

	var count int64
	s.db.Model(&Instance{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("instance name %q already exists", req.Name)
	}

	safeName := sanitizeName(req.Name)
	if safeName == "unnamed" {
		return nil, fmt.Errorf("instance name must contain at least one letter or digit")
	}

	// Check slug collision: different names mapping to the same container/directory.
	var existingInstances []Instance
	s.db.Select("name").Find(&existingInstances)
	for _, ex := range existingInstances {
		if sanitizeName(ex.Name) == safeName {
			return nil, fmt.Errorf("instance name %q conflicts with existing instance %q (same container name)", req.Name, ex.Name)
		}
	}

	port := req.Port
	if port == 0 {
		port = engineInfo.Port
	}
	// Validate port range.
	if port < 1024 || port > 65535 {
		return nil, fmt.Errorf("port must be between 1024 and 65535, got %d", port)
	}
	// Check for port conflicts with existing instances.
	var portConflict int64
	s.db.Model(&Instance{}).Where("port = ?", port).Count(&portConflict)
	if portConflict > 0 {
		return nil, fmt.Errorf("port %d is already in use by another instance", port)
	}

	memLimit := req.MemoryLimit
	if memLimit == "" {
		memLimit = "0.5g"
	}

	containerName := "webcasa-db-" + safeName

	var configJSON string
	if req.Config != nil {
		if data, err := json.Marshal(req.Config); err == nil {
			configJSON = string(data)
		}
	}

	inst := &Instance{
		Name:          req.Name,
		Engine:        req.Engine,
		Version:       version,
		Status:        "stopped",
		Port:          port,
		RootPassword:  req.RootPassword,
		DataDir:       filepath.Join(s.dataDir, "instances", sanitizeName(req.Name)),
		ContainerName: containerName,
		MemoryLimit:   memLimit,
		Config:        configJSON,
	}

	progressCb("Preparing instance directory...")
	if err := os.MkdirAll(inst.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("create instance dir: %w", err)
	}

	composeContent := GenerateComposeFile(inst)
	composePath := filepath.Join(inst.DataDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0600); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}

	envContent := fmt.Sprintf("ROOT_PASSWORD=%s\n", inst.RootPassword)
	envPath := filepath.Join(inst.DataDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return nil, fmt.Errorf("write env file: %w", err)
	}

	progressCb("Saving instance record...")
	if err := s.db.Create(inst).Error; err != nil {
		return nil, fmt.Errorf("create instance record: %w", err)
	}

	if req.AutoStart {
		progressCb("Starting instance (pulling image if needed)...")
		if err := s.runComposeStream(inst.DataDir, progressCb, "up", "-d", "--remove-orphans"); err != nil {
			// Rollback: remove DB record and data dir.
			s.db.Where("instance_id = ?", inst.ID).Delete(&Database{})
			s.db.Where("instance_id = ?", inst.ID).Delete(&DatabaseUser{})
			s.db.Delete(&Instance{}, inst.ID)
			os.RemoveAll(inst.DataDir)
			return nil, fmt.Errorf("auto-start failed: %w", err)
		}
	}

	return s.GetInstance(inst.ID)
}

// runComposeStream executes a docker compose command and streams output line-by-line via callback.
func (s *Service) runComposeStream(dir string, cb func(string), args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("docker compose %s: %w", args[0], err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		cb(scanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("docker compose %s failed: %w", args[0], err)
	}
	return nil
}

// DeleteInstance stops and removes an instance.
func (s *Service) DeleteInstance(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}

	// Stop and remove containers + volumes — if this fails, keep the instance visible.
	if err := s.runCompose(inst.DataDir, "down", "--volumes", "--remove-orphans"); err != nil {
		s.logger.Error("compose down failed", "instance", inst.Name, "err", err)
		return fmt.Errorf("failed to stop instance: %w (instance kept for manual cleanup)", err)
	}

	// Remove data directory.
	os.RemoveAll(inst.DataDir)

	// Delete related records.
	s.db.Where("instance_id = ?", id).Delete(&Database{})
	s.db.Where("instance_id = ?", id).Delete(&DatabaseUser{})

	return s.db.Delete(&Instance{}, id).Error
}

// ── Instance Lifecycle ──

// StartInstance starts the instance.
func (s *Service) StartInstance(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	return s.startInstance(inst)
}

func (s *Service) startInstance(inst *Instance) error {
	return s.runCompose(inst.DataDir, "up", "-d", "--remove-orphans")
}

// StopInstance stops the instance.
func (s *Service) StopInstance(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	return s.runCompose(inst.DataDir, "down")
}

// RestartInstance restarts the instance.
func (s *Service) RestartInstance(id uint) error {
	inst, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	return s.runCompose(inst.DataDir, "restart")
}

// InstanceLogs returns recent logs.
func (s *Service) InstanceLogs(id uint, tail string) (string, error) {
	inst, err := s.GetInstance(id)
	if err != nil {
		return "", err
	}
	if tail == "" {
		tail = "200"
	}
	return s.runComposeOutput(inst.DataDir, "logs", "--tail", tail, "--no-color")
}

// InstanceLogsFollow starts a streaming log process.
func (s *Service) InstanceLogsFollow(ctx context.Context, id uint, tail string) (io.ReadCloser, error) {
	inst, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}
	if tail == "" {
		tail = "100"
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--follow", "--tail", tail, "--no-color")
	cmd.Dir = inst.DataDir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(inst.DataDir))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() { cmd.Wait() }()
	return stdout, nil
}

// ── Connection Info ──

// GetConnectionInfo generates connection strings for an instance.
func (s *Service) GetConnectionInfo(id uint) (*ConnectionInfo, error) {
	inst, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}

	info := &ConnectionInfo{
		Host:           "localhost",
		Port:           inst.Port,
		DockerInternal: fmt.Sprintf("%s:%d", inst.ContainerName, defaultInternalPort(inst.Engine)),
	}

	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		info.Username = "root"
		info.ConnectionURI = fmt.Sprintf("mysql://root@localhost:%d/", inst.Port)
		info.CLICommand = fmt.Sprintf("mysql -h 127.0.0.1 -P %d -u root -p", inst.Port)
		info.EnvVar = fmt.Sprintf("DATABASE_URL=mysql://root:PASSWORD@localhost:%d/dbname", inst.Port)
	case EnginePostgres:
		info.Username = "postgres"
		info.ConnectionURI = fmt.Sprintf("postgresql://postgres@localhost:%d/", inst.Port)
		info.CLICommand = fmt.Sprintf("psql -h 127.0.0.1 -p %d -U postgres", inst.Port)
		info.EnvVar = fmt.Sprintf("DATABASE_URL=postgresql://postgres:PASSWORD@localhost:%d/dbname", inst.Port)
	case EngineRedis:
		info.Username = ""
		info.ConnectionURI = fmt.Sprintf("redis://:%s@localhost:%d/0", "PASSWORD", inst.Port)
		info.CLICommand = fmt.Sprintf("redis-cli -h 127.0.0.1 -p %d -a PASSWORD", inst.Port)
		info.EnvVar = fmt.Sprintf("REDIS_URL=redis://:PASSWORD@localhost:%d/0", inst.Port)
	}

	return info, nil
}

// GetRootPassword returns the root password for an instance.
func (s *Service) GetRootPassword(id uint) (string, error) {
	var inst Instance
	if err := s.db.First(&inst, id).Error; err != nil {
		return "", err
	}
	return inst.RootPassword, nil
}

// ExecuteQuery executes a read-only query against a running database instance.
// SQL: only SELECT, SHOW, DESCRIBE, and EXPLAIN are allowed.
// Redis: only read-only commands (GET, KEYS, INFO, etc.) are allowed.
func (s *Service) ExecuteQuery(instanceID uint, database, query string, limit int) (*QueryResult, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}

	if inst.Engine == EngineRedis {
		// Redis: validate command is read-only.
		if !isReadOnlyRedisCommand(query) {
			return nil, fmt.Errorf("only read-only Redis commands are allowed (GET, MGET, KEYS, SCAN, INFO, TTL, TYPE, EXISTS, DBSIZE, LRANGE, SCARD, SMEMBERS, HGETALL, HGET, LLEN, ZRANGE, ZCARD)")
		}
	} else {
		// SQL: validate query is read-only.
		if !isReadOnlyQuery(query) {
			return nil, fmt.Errorf("only SELECT, SHOW, DESCRIBE, and EXPLAIN statements are allowed")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewDBClient()
	return client.ExecuteQuery(ctx, inst, database, query, limit)
}

// isReadOnlyRedisCommand checks if a Redis command is read-only.
func isReadOnlyRedisCommand(cmd string) bool {
	parts := strings.Fields(strings.TrimSpace(cmd))
	if len(parts) == 0 {
		return false
	}
	readOnly := map[string]bool{
		"GET": true, "MGET": true, "KEYS": true, "SCAN": true,
		"INFO": true, "TTL": true, "PTTL": true, "TYPE": true,
		"EXISTS": true, "DBSIZE": true, "RANDOMKEY": true,
		"LRANGE": true, "LLEN": true, "LINDEX": true,
		"SCARD": true, "SMEMBERS": true, "SISMEMBER": true,
		"HGET": true, "HGETALL": true, "HLEN": true, "HKEYS": true, "HVALS": true,
		"ZRANGE": true, "ZCARD": true, "ZSCORE": true, "ZRANK": true,
		"STRLEN": true, "PING": true, "ECHO": true, "TIME": true,
		"SELECT": true,
	}
	return readOnly[strings.ToUpper(parts[0])]
}

// isReadOnlyQuery checks if a SQL query is a read-only, single statement.
func isReadOnlyQuery(query string) bool {
	// Normalize: trim whitespace and get the first keyword
	normalized := strings.TrimSpace(query)
	// Remove leading comments (single-line and multi-line)
	for {
		if strings.HasPrefix(normalized, "--") {
			if idx := strings.Index(normalized, "\n"); idx >= 0 {
				normalized = strings.TrimSpace(normalized[idx+1:])
				continue
			}
			return false // comment-only query
		}
		if strings.HasPrefix(normalized, "/*") {
			if idx := strings.Index(normalized, "*/"); idx >= 0 {
				normalized = strings.TrimSpace(normalized[idx+2:])
				continue
			}
			return false // unclosed comment
		}
		break
	}

	// Reject stacked queries: look for semicolons outside quoted strings.
	body := strings.TrimRight(normalized, "; \t\n\r")
	if containsUnquotedSemicolon(body) {
		return false
	}

	upper := strings.ToUpper(normalized)

	// Reject SELECT INTO (write side-effects via single statement).
	if strings.Contains(upper, " INTO ") && (strings.Contains(upper, "OUTFILE") || strings.Contains(upper, "DUMPFILE") || strings.Contains(upper, "TEMP ") || strings.Contains(upper, "TEMPORARY ")) {
		return false
	}
	allowedPrefixes := []string{"SELECT ", "SELECT\t", "SELECT\n",
		"SHOW ", "SHOW\t", "SHOW\n",
		"DESCRIBE ", "DESCRIBE\t", "DESCRIBE\n",
		"DESC ", "DESC\t", "DESC\n",
		"EXPLAIN ", "EXPLAIN\t", "EXPLAIN\n"}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			// EXPLAIN ANALYZE executes the underlying statement (DELETE/UPDATE/INSERT)
			// on PostgreSQL and MySQL 8+, so it is NOT read-only.
			if strings.HasPrefix(upper, "EXPLAIN") && strings.Contains(upper, "ANALYZE") {
				return false
			}
			return true
		}
	}
	return false
}

// containsUnquotedSemicolon checks whether s has a semicolon that is NOT
// inside a single-quoted SQL string literal. This avoids false positives
// for queries like SELECT ';' AS s while still blocking stacked statements.
func containsUnquotedSemicolon(s string) bool {
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' {
			if inQuote && i+1 < len(s) && s[i+1] == '\'' {
				i++ // escaped quote '' — skip both
				continue
			}
			inQuote = !inQuote
			continue
		}
		if ch == '\\' && inQuote {
			i++ // skip escaped character inside string
			continue
		}
		if ch == ';' && !inQuote {
			return true
		}
	}
	return false
}

// ── Database CRUD ──

// ListDatabases returns databases for an instance.
func (s *Service) ListDatabases(instanceID uint) ([]Database, error) {
	var dbs []Database
	if err := s.db.Where("instance_id = ?", instanceID).Order("id ASC").Find(&dbs).Error; err != nil {
		return nil, err
	}
	return dbs, nil
}

// CreateDatabase creates a logical database in the instance.
func (s *Service) CreateDatabase(instanceID uint, req *CreateDatabaseRequest) (*Database, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}
	if inst.Engine == EngineRedis {
		return nil, fmt.Errorf("Redis does not support named databases")
	}

	charset := req.Charset
	if charset == "" {
		if inst.Engine == EnginePostgres {
			charset = "UTF8"
		} else {
			charset = "utf8mb4"
		}
	}

	// Validate database name matches the same pattern used by the SQL query executor,
	// so databases created here can always be queried later.
	if !validDBNameRe.MatchString(req.Name) {
		return nil, fmt.Errorf("invalid database name %q: must match [a-zA-Z_][a-zA-Z0-9_-]*", req.Name)
	}

	client := NewDBClient()
	if err := client.CreateDatabase(inst, req.Name, charset); err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}

	db := &Database{
		InstanceID: instanceID,
		Name:       req.Name,
		Charset:    charset,
	}
	if err := s.db.Create(db).Error; err != nil {
		return nil, err
	}
	return db, nil
}

// DeleteDatabase drops a logical database.
func (s *Service) DeleteDatabase(instanceID uint, dbName string) error {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return err
	}
	if inst.Status != "running" {
		return fmt.Errorf("instance is not running")
	}

	client := NewDBClient()
	if err := client.DropDatabase(inst, dbName); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}

	return s.db.Where("instance_id = ? AND name = ?", instanceID, dbName).Delete(&Database{}).Error
}

// ── User CRUD ──

// ListUsers returns database users for an instance.
func (s *Service) ListUsers(instanceID uint) ([]DatabaseUser, error) {
	var users []DatabaseUser
	if err := s.db.Where("instance_id = ?", instanceID).Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// CreateUser creates a database user and grants access.
func (s *Service) CreateUser(instanceID uint, req *CreateUserRequest) (*DatabaseUser, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}
	if inst.Engine == EngineRedis {
		return nil, fmt.Errorf("Redis does not support user management via this interface")
	}

	client := NewDBClient()
	if err := client.CreateUser(inst, req.Username, req.Password); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	// Grant access to specified databases.
	var grantErrors []string
	for _, dbName := range req.Databases {
		if err := client.GrantAll(inst, req.Username, dbName); err != nil {
			grantErrors = append(grantErrors, fmt.Sprintf("%s: %v", dbName, err))
		}
	}
	if len(grantErrors) > 0 {
		// Rollback: drop the user we just created since grants failed.
		_ = client.DropUser(inst, req.Username)
		return nil, fmt.Errorf("grant failed for databases: %s", strings.Join(grantErrors, "; "))
	}

	user := &DatabaseUser{
		InstanceID: instanceID,
		Username:   req.Username,
		Host:       "%",
	}
	if err := s.db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// DeleteUser drops a database user.
func (s *Service) DeleteUser(instanceID uint, username string) error {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return err
	}
	if inst.Status != "running" {
		return fmt.Errorf("instance is not running")
	}

	client := NewDBClient()
	if err := client.DropUser(inst, username); err != nil {
		return fmt.Errorf("drop user: %w", err)
	}

	return s.db.Where("instance_id = ? AND username = ?", instanceID, username).Delete(&DatabaseUser{}).Error
}

// ── Helpers ──

// allocatePort finds the next available port for the engine.
func (s *Service) allocatePort(engine EngineType) (int, error) {
	portRange := enginePortRange(engine)
	if portRange[0] == 0 {
		return 0, fmt.Errorf("unknown engine port range for %s", engine)
	}

	// Get all used ports.
	var usedPorts []int
	s.db.Model(&Instance{}).Pluck("port", &usedPorts)
	usedSet := make(map[int]bool, len(usedPorts))
	for _, p := range usedPorts {
		usedSet[p] = true
	}

	for port := portRange[0]; port <= portRange[1]; port++ {
		if !usedSet[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports for %s (range %d-%d)", engine, portRange[0], portRange[1])
}

func enginePortRange(engine EngineType) [2]int {
	switch engine {
	case EngineMySQL:
		return [2]int{13306, 13399}
	case EnginePostgres:
		return [2]int{15432, 15499}
	case EngineMariaDB:
		return [2]int{13400, 13499}
	case EngineRedis:
		return [2]int{16379, 16399}
	default:
		return [2]int{0, 0}
	}
}

func defaultInternalPort(engine EngineType) int {
	switch engine {
	case EngineMySQL, EngineMariaDB:
		return 3306
	case EnginePostgres:
		return 5432
	case EngineRedis:
		return 6379
	default:
		return 0
	}
}

func findEngine(engine EngineType) *EngineInfo {
	for _, e := range SupportedEngines {
		if e.Engine == engine {
			return &e
		}
	}
	return nil
}

// resolveInstanceStatus checks Docker for actual container state.
func (s *Service) resolveInstanceStatus(inst *Instance) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Status}}", inst.ContainerName)
	output, err := cmd.Output()
	if err != nil {
		return "stopped"
	}
	status := strings.TrimSpace(string(output))
	if status == "running" {
		return "running"
	}
	return "stopped"
}

// runCompose executes a docker compose command in the given directory.
func (s *Service) runCompose(dir string, args ...string) error {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("docker compose failed",
			"dir", dir,
			"args", strings.Join(args, " "),
			"output", string(output),
			"err", err,
		)
		return fmt.Errorf("docker compose %s: %s", args[0], strings.TrimSpace(string(output)))
	}
	return nil
}

// runComposeOutput executes a compose command and returns stdout.
func (s *Service) runComposeOutput(dir string, args ...string) (string, error) {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+filepath.Base(dir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose %s: %s", args[0], strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// sanitizeName converts a name to a filesystem-safe string.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "unnamed"
	}
	return name
}
