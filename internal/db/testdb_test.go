package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// newTestDB gives each test a fresh database with the project's migrations.
func newTestDB(test *testing.T) *sql.DB {
	test.Helper()

	conn, err := sql.Open("sqlite", filepath.Join(test.TempDir(), "test.db"))
	if err != nil {
		test.Fatalf("open test database: %v", err)
	}
	test.Cleanup(func() {
		if err := conn.Close(); err != nil {
			test.Errorf("close test database: %v", err)
		}
	})

	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(test.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		test.Fatalf("enable foreign keys: %v", err)
	}

	if err := goose.SetDialect("sqlite3"); err != nil {
		test.Fatalf("set migration dialect: %v", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		test.Fatalf("apply migrations: %v", err)
	}

	return conn
}
