package database

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql" // MySQL driver
	_ "github.com/lib/pq"              // PostgreSQL driver
	"github.com/redis/go-redis/v9"
)

// DBClient wraps database driver connections for executing DDL on running instances.
type DBClient struct{}

// NewDBClient creates a new DBClient.
func NewDBClient() *DBClient {
	return &DBClient{}
}

// connectMySQL connects to a MySQL or MariaDB instance.
func (c *DBClient) connectMySQL(inst *Instance) (*sql.DB, error) {
	// Use mysql.Config to safely build DSN with any special characters in password.
	cfg := mysqldriver.NewConfig()
	cfg.User = "root"
	cfg.Passwd = inst.RootPassword
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("127.0.0.1:%d", inst.Port)
	dsn := cfg.FormatDSN()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping failed: %w", err)
	}
	return db, nil
}

// connectPostgres connects to a PostgreSQL instance.
func (c *DBClient) connectPostgres(inst *Instance) (*sql.DB, error) {
	// Quote password with single quotes; escape backslashes first, then quotes (libpq convention).
	escapedPwd := strings.ReplaceAll(inst.RootPassword, `\`, `\\`)
	escapedPwd = strings.ReplaceAll(escapedPwd, "'", `\'`)
	dsn := fmt.Sprintf("host=127.0.0.1 port=%d user=postgres password='%s' sslmode=disable",
		inst.Port, escapedPwd)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping failed: %w", err)
	}
	return db, nil
}

// connectRedis connects to a Redis instance.
func (c *DBClient) connectRedis(inst *Instance) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("127.0.0.1:%d", inst.Port),
		Password: inst.RootPassword,
	})
}

// ── Database operations ──

// validCharsets whitelist of safe charset values.
var validCharsets = map[string]bool{
	"utf8": true, "utf8mb4": true, "latin1": true, "ascii": true,
	"binary": true, "utf16": true, "utf32": true, "big5": true,
	"gb2312": true, "gbk": true, "euckr": true, "sjis": true,
	"UTF8": true, // PostgreSQL
}

// CreateDatabase creates a database in the instance.
func (c *DBClient) CreateDatabase(inst *Instance, name, charset string) error {
	if !validCharsets[charset] {
		return fmt.Errorf("invalid charset: %s", charset)
	}
	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		return c.mysqlExec(inst, fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET %s", escapeName(name), charset))
	case EnginePostgres:
		return c.pgCreateDatabase(inst, name, charset)
	default:
		return fmt.Errorf("engine %s does not support database creation", inst.Engine)
	}
}

// DropDatabase drops a database in the instance.
func (c *DBClient) DropDatabase(inst *Instance, name string) error {
	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		return c.mysqlExec(inst, fmt.Sprintf("DROP DATABASE `%s`", escapeName(name)))
	case EnginePostgres:
		return c.pgExec(inst, fmt.Sprintf(`DROP DATABASE "%s"`, escapeName(name)))
	default:
		return fmt.Errorf("engine %s does not support database deletion", inst.Engine)
	}
}

// ── User operations ──

// CreateUser creates a user in the instance.
func (c *DBClient) CreateUser(inst *Instance, username, password string) error {
	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		return c.mysqlExec(inst, fmt.Sprintf(
			"CREATE USER '%s'@'%%' IDENTIFIED BY '%s'",
			escapeString(username), escapeString(password),
		))
	case EnginePostgres:
		return c.pgExec(inst, fmt.Sprintf(
			`CREATE USER "%s" WITH PASSWORD '%s'`,
			escapeName(username), escapeString(password),
		))
	default:
		return fmt.Errorf("engine %s does not support user creation", inst.Engine)
	}
}

// GrantAll grants all privileges on a database to a user.
func (c *DBClient) GrantAll(inst *Instance, username, database string) error {
	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		err := c.mysqlExec(inst, fmt.Sprintf(
			"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'",
			escapeName(database), escapeString(username),
		))
		if err != nil {
			return err
		}
		return c.mysqlExec(inst, "FLUSH PRIVILEGES")
	case EnginePostgres:
		return c.pgExec(inst, fmt.Sprintf(
			`GRANT ALL PRIVILEGES ON DATABASE "%s" TO "%s"`,
			escapeName(database), escapeName(username),
		))
	default:
		return fmt.Errorf("engine %s does not support grants", inst.Engine)
	}
}

// DropUser drops a user from the instance.
func (c *DBClient) DropUser(inst *Instance, username string) error {
	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		return c.mysqlExec(inst, fmt.Sprintf("DROP USER '%s'@'%%'", escapeString(username)))
	case EnginePostgres:
		return c.pgExec(inst, fmt.Sprintf(`DROP USER "%s"`, escapeName(username)))
	default:
		return fmt.Errorf("engine %s does not support user deletion", inst.Engine)
	}
}

// ── Internal helpers ──

func (c *DBClient) mysqlExec(inst *Instance, query string) error {
	db, err := c.connectMySQL(inst)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(query)
	return err
}

func (c *DBClient) pgExec(inst *Instance, query string) error {
	db, err := c.connectPostgres(inst)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(query)
	return err
}

// pgCreateDatabase uses a separate connection to create a database (can't run inside a tx).
func (c *DBClient) pgCreateDatabase(inst *Instance, name, charset string) error {
	db, err := c.connectPostgres(inst)
	if err != nil {
		return err
	}
	defer db.Close()
	// CREATE DATABASE cannot run in a transaction in PG.
	_, err = db.Exec(fmt.Sprintf(`CREATE DATABASE "%s" ENCODING '%s'`, escapeName(name), charset))
	return err
}

// escapeName sanitizes identifiers to prevent SQL injection.
func escapeName(name string) string {
	// Remove any backticks, double quotes, and semicolons.
	name = strings.ReplaceAll(name, "`", "")
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, ";", "")
	name = strings.ReplaceAll(name, "'", "")
	return name
}

// escapeString escapes a string for use in SQL single-quoted literals.
// Escapes backslashes first (MySQL treats \ as escape char by default),
// then single quotes.
func escapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

// ── Query Execution ──

// ExecuteQuery runs an arbitrary SQL query (or Redis command) against a running instance.
func (c *DBClient) ExecuteQuery(ctx context.Context, inst *Instance, database, query string, limit int) (*QueryResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}

	switch inst.Engine {
	case EngineMySQL, EngineMariaDB:
		return c.executeSQLQuery(ctx, "mysql", inst, database, query, limit)
	case EnginePostgres:
		return c.executeSQLQuery(ctx, "postgres", inst, database, query, limit)
	case EngineRedis:
		return c.executeRedisCommand(ctx, inst, query)
	default:
		return nil, fmt.Errorf("unsupported engine: %s", inst.Engine)
	}
}

// executeSQLQuery executes a SQL query on a MySQL, MariaDB, or PostgreSQL instance.
// validDatabaseName checks that a database name is safe for DSN/SQL use.
var validDBNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

func (c *DBClient) executeSQLQuery(ctx context.Context, driver string, inst *Instance, database, query string, limit int) (*QueryResult, error) {
	if database != "" && !validDBNameRe.MatchString(database) {
		return nil, fmt.Errorf("invalid database name: %s", database)
	}

	var db *sql.DB
	var err error

	switch driver {
	case "mysql":
		if database != "" {
			cfg := mysqldriver.NewConfig()
			cfg.User = "root"
			cfg.Passwd = inst.RootPassword
			cfg.Net = "tcp"
			cfg.Addr = fmt.Sprintf("127.0.0.1:%d", inst.Port)
			cfg.DBName = database
			db, err = sql.Open("mysql", cfg.FormatDSN())
		} else {
			db, err = c.connectMySQL(inst)
		}
	case "postgres":
		if database != "" {
			escapedPwd := strings.ReplaceAll(inst.RootPassword, `\`, `\\`)
			escapedPwd = strings.ReplaceAll(escapedPwd, "'", `\'`)
			dsn := fmt.Sprintf("host=127.0.0.1 port=%d user=postgres password='%s' sslmode=disable dbname=%s",
				inst.Port, escapedPwd, database)
			db, err = sql.Open("postgres", dsn)
		} else {
			db, err = c.connectPostgres(inst)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	trimmed := strings.TrimSpace(query)
	upper := strings.ToUpper(trimmed)

	// Determine if query returns rows.
	isSelectLike := strings.HasPrefix(upper, "SELECT") ||
		strings.HasPrefix(upper, "SHOW") ||
		strings.HasPrefix(upper, "DESCRIBE") ||
		strings.HasPrefix(upper, "DESC ") ||
		strings.HasPrefix(upper, "EXPLAIN")

	if isSelectLike {
		if !strings.Contains(upper, "LIMIT") && strings.HasPrefix(upper, "SELECT") {
			query = fmt.Sprintf("%s LIMIT %d", strings.TrimSuffix(trimmed, ";"), limit)
		}
		return c.queryRows(ctx, db, query)
	}

	// DML/DDL: execute and return affected rows.
	result, err := db.ExecContext(ctx, trimmed)
	if err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}
	affected, _ := result.RowsAffected()
	return &QueryResult{
		Columns: []string{"affected_rows"},
		Rows:    []map[string]interface{}{{"affected_rows": affected}},
		Count:   1,
	}, nil
}

// queryRows executes a query that returns rows and scans them into QueryResult.
func (c *DBClient) queryRows(ctx context.Context, db *sql.DB, query string) (*QueryResult, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := &QueryResult{Columns: columns}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		result.Rows = append(result.Rows, row)
	}
	result.Count = len(result.Rows)
	return result, nil
}

// executeRedisCommand executes a Redis command and returns the result.
func (c *DBClient) executeRedisCommand(ctx context.Context, inst *Instance, command string) (*QueryResult, error) {
	rdb := c.connectRedis(inst)
	defer rdb.Close()

	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	args := make([]interface{}, len(parts))
	for i, p := range parts {
		args[i] = p
	}

	val, err := rdb.Do(ctx, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}

	return formatRedisResult(val), nil
}

// formatRedisResult converts a Redis response to QueryResult format.
func formatRedisResult(val interface{}) *QueryResult {
	switch v := val.(type) {
	case []interface{}:
		result := &QueryResult{Columns: []string{"index", "value"}}
		for i, item := range v {
			row := map[string]interface{}{"index": i, "value": fmt.Sprintf("%v", item)}
			result.Rows = append(result.Rows, row)
		}
		result.Count = len(result.Rows)
		return result
	default:
		return &QueryResult{
			Columns: []string{"result"},
			Rows:    []map[string]interface{}{{"result": fmt.Sprintf("%v", v)}},
			Count:   1,
		}
	}
}
