package database

import "testing"

// TestIsReadOnlyQuery verifies the SQL read-only guard used to protect the
// browser-query endpoint from write/DDL/DCL operations.
//
// Known limitation: the function only inspects the first statement keyword
// after stripping comments.  Multi-statement queries such as
// "SELECT 1; DROP TABLE users" will pass the check because the leading
// keyword is SELECT.  The actual defence against multi-statement injection
// must be handled at the database driver level (e.g. disabling
// multi-statements on the connection).
func TestIsReadOnlyQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		// ── Allowed: basic SELECT ──────────────────────────────────
		{"basic SELECT", "SELECT * FROM users", true},
		{"lowercase select", "select * from users", true},
		{"SELECT with WHERE", "SELECT id, name FROM users WHERE id = 1", true},
		{"leading spaces", "  SELECT * FROM users", true},
		{"leading tab", "\tSELECT * FROM users", true},
		{"leading newline", "\nSELECT * FROM users", true},

		// ── Allowed: SHOW / DESCRIBE / DESC / EXPLAIN ─────────────
		{"SHOW TABLES", "SHOW TABLES", true},
		{"SHOW DATABASES", "SHOW DATABASES", true},
		{"lowercase show", "show tables", true},
		{"DESCRIBE", "DESCRIBE users", true},
		{"DESC", "DESC users", true},
		{"EXPLAIN", "EXPLAIN SELECT * FROM users", true},
		// EXPLAIN ANALYZE actually executes the statement (dangerous for DML).
		{"explain analyze select", "explain analyze select * from users", false},
		{"EXPLAIN ANALYZE DELETE", "EXPLAIN ANALYZE DELETE FROM users", false},

		// ── Allowed: comments followed by SELECT ──────────────────
		{"single-line comment then SELECT", "-- comment\nSELECT * FROM users", true},
		{"block comment then SELECT", "/* block comment */SELECT * FROM users", true},
		{"multi-line block comment then SELECT", "/* multi\nline\ncomment */\nSELECT * FROM users", true},
		{"multiple single-line comments then SELECT", "-- comment1\n-- comment2\nSELECT * FROM users", true},

		// ── Denied: DDL / DML / DCL ───────────────────────────────
		{"DROP TABLE", "DROP TABLE users", false},
		{"DROP DATABASE", "DROP DATABASE production", false},
		{"DELETE", "DELETE FROM users", false},
		{"INSERT", "INSERT INTO users (name) VALUES ('evil')", false},
		{"UPDATE", "UPDATE users SET role = 'admin'", false},
		{"ALTER TABLE", "ALTER TABLE users ADD COLUMN backdoor TEXT", false},
		{"CREATE TABLE", "CREATE TABLE evil (id INT)", false},
		{"TRUNCATE", "TRUNCATE TABLE users", false},
		{"GRANT", "GRANT ALL ON *.* TO 'hacker'", false},
		{"REVOKE", "REVOKE ALL ON *.* FROM 'admin'", false},

		// ── Denied: empty / whitespace / comment-only ─────────────
		{"empty string", "", false},
		{"only spaces", "   ", false},
		{"comment only (single-line)", "-- just a comment", false},
		{"comment only (block)", "/* only a block comment */", false},

		// ── SQL injection bypass attempts ─────────────────────────
		// Stacked queries are now rejected (semicolons in body are blocked).
		{"multi-statement SELECT then DROP", "SELECT 1; DROP TABLE users", false},
		{"semicolon inside string literal is allowed", "SELECT ';' AS s", true},
		{"semicolon outside string is still blocked", "SELECT 'ok'; DROP TABLE users", false},
		{"comment hiding DROP then real DROP", "-- DROP TABLE\nDROP TABLE users", false},
		{"empty block comment before DROP", "/**/DROP TABLE users", false},
		{"leading spaces before DROP", "   DROP TABLE users", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isReadOnlyQuery(tc.query)
			if got != tc.want {
				t.Errorf("isReadOnlyQuery(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}
