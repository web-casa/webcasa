package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
)

// SQLiteBrowser provides read-only access to SQLite database files.
type SQLiteBrowser struct {
	logger *slog.Logger
}

// NewSQLiteBrowser creates a new SQLiteBrowser.
func NewSQLiteBrowser(logger *slog.Logger) *SQLiteBrowser {
	return &SQLiteBrowser{logger: logger}
}

// validExtensions lists allowed SQLite file extensions.
var validExtensions = map[string]bool{
	".db":      true,
	".sqlite":  true,
	".sqlite3": true,
}

// unsafePattern matches SQL keywords that could modify data.
var unsafePattern = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|ATTACH|DETACH|REPLACE|PRAGMA\s+\w+\s*=)\b`)

// validatePath checks that the file path is safe.
func (b *SQLiteBrowser) validatePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	ext := filepath.Ext(path)
	if !validExtensions[ext] {
		return fmt.Errorf("invalid file extension: %s (allowed: .db, .sqlite, .sqlite3)", ext)
	}
	return nil
}

// open opens a SQLite file in read-only mode.
func (b *SQLiteBrowser) open(path string) (*sql.DB, error) {
	if err := b.validatePath(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	return db, nil
}

// ListTables returns all table names in the file.
func (b *SQLiteBrowser) ListTables(path string) ([]string, error) {
	db, err := b.open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, nil
}

// TableSchema represents column info for a table.
type TableSchema struct {
	CID       int    `json:"cid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	NotNull   bool   `json:"not_null"`
	Default   string `json:"default_value"`
	PK        bool   `json:"pk"`
}

// GetSchema returns column definitions for a table.
func (b *SQLiteBrowser) GetSchema(path, table string) ([]TableSchema, error) {
	db, err := b.open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Validate table name (alphanumeric + underscore only).
	if !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(table) {
		return nil, fmt.Errorf("invalid table name")
	}

	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schema []TableSchema
	for rows.Next() {
		var s TableSchema
		var dflt sql.NullString
		if err := rows.Scan(&s.CID, &s.Name, &s.Type, &s.NotNull, &dflt, &s.PK); err != nil {
			return nil, err
		}
		if dflt.Valid {
			s.Default = dflt.String
		}
		schema = append(schema, s)
	}
	return schema, nil
}

// QueryResult holds the result of a SELECT query.
type QueryResult struct {
	Columns []string                 `json:"columns"`
	Rows    []map[string]interface{} `json:"rows"`
	Count   int                      `json:"count"`
}

// Query executes a read-only SELECT query.
func (b *SQLiteBrowser) Query(path, query string, limit int) (*QueryResult, error) {
	// Validate query is SELECT only.
	trimmed := strings.TrimSpace(query)
	if !strings.HasPrefix(strings.ToUpper(trimmed), "SELECT") {
		return nil, fmt.Errorf("only SELECT queries are allowed")
	}
	if unsafePattern.MatchString(trimmed) {
		return nil, fmt.Errorf("query contains disallowed keywords")
	}

	// Enforce limit.
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	// Append LIMIT if not present.
	upper := strings.ToUpper(trimmed)
	if !strings.Contains(upper, "LIMIT") {
		query = fmt.Sprintf("%s LIMIT %d", strings.TrimSuffix(trimmed, ";"), limit)
	}

	db, err := b.open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
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
			// Convert []byte to string for JSON serialization.
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
