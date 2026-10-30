package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens a SQLite database and applies the embedded schema migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}

	// SQLite foreign-key enforcement belongs to a connection, not the database file.
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("load embedded migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, migrationFS)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("prepare migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return conn, nil
}
