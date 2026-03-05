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

	// Allocate port — default to engine's standard port.
	port := req.Port
	if port == 0 {
		port = engineInfo.Port
	}

	// Memory limit — default 0.5g.
	memLimit := req.MemoryLimit
	if memLimit == "" {
		memLimit = "0.5g"
	}

	containerName := "webcasa-db-" + sanitizeName(req.Name)

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
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
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
			s.logger.Error("auto-start failed", "instance", req.Name, "err", err)
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

	port := req.Port
	if port == 0 {
		port = engineInfo.Port
	}

	memLimit := req.MemoryLimit
	if memLimit == "" {
		memLimit = "0.5g"
	}

	containerName := "webcasa-db-" + sanitizeName(req.Name)

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
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
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
			s.logger.Error("auto-start failed", "instance", req.Name, "err", err)
			progressCb("Warning: auto-start failed: " + err.Error())
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

	// Stop and remove containers + volumes.
	_ = s.runCompose(inst.DataDir, "down", "--volumes", "--remove-orphans")

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
// Only SELECT, SHOW, DESCRIBE, and EXPLAIN statements are allowed.
func (s *Service) ExecuteQuery(instanceID uint, database, query string, limit int) (*QueryResult, error) {
	inst, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}

	// Validate query is read-only
	if !isReadOnlyQuery(query) {
		return nil, fmt.Errorf("only SELECT, SHOW, DESCRIBE, and EXPLAIN statements are allowed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewDBClient()
	return client.ExecuteQuery(ctx, inst, database, query, limit)
}

// isReadOnlyQuery checks if a SQL query is a read-only statement.
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

	upper := strings.ToUpper(normalized)
	allowedPrefixes := []string{"SELECT ", "SELECT\t", "SELECT\n",
		"SHOW ", "SHOW\t", "SHOW\n",
		"DESCRIBE ", "DESCRIBE\t", "DESCRIBE\n",
		"DESC ", "DESC\t", "DESC\n",
		"EXPLAIN ", "EXPLAIN\t", "EXPLAIN\n"}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
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
	for _, dbName := range req.Databases {
		if err := client.GrantAll(inst, req.Username, dbName); err != nil {
			s.logger.Error("grant failed", "user", req.Username, "db", dbName, "err", err)
		}
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
	return strings.Trim(name, "-")
}
