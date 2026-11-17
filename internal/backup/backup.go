// Package backup creates a consistent SQLite snapshot without changing the source.
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Create writes a validated snapshot to a new destination. It never replaces
// an existing file; a temporary file is atomically linked only after checks.
func Create(ctx context.Context, source, destination string) error {
	if source == "" || destination == "" {
		return errors.New("source and destination are required")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("source database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("source must be a regular database file")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".summa-backup-*.db")
	if err != nil {
		return fmt.Errorf("create temporary backup: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Close(); err != nil {
		return err
	}

	sourceURL := (&url.URL{Scheme: "file", Path: source}).String() + "?mode=ro"
	conn, err := sql.Open("sqlite", sourceURL)
	if err != nil {
		return err
	}
	var tables int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('organizations', 'accounts', 'transactions', 'entries', 'goose_db_version')`).Scan(&tables); err != nil {
		conn.Close()
		return fmt.Errorf("inspect source database: %w", err)
	}
	if tables != 5 {
		conn.Close()
		return errors.New("source does not appear to be a Summa database")
	}
	if _, err = conn.ExecContext(ctx, `VACUUM INTO ?`, temporary); err != nil {
		conn.Close()
		return fmt.Errorf("create snapshot: %w", err)
	}
	if err := conn.Close(); err != nil {
		return err
	}

	backupURL := (&url.URL{Scheme: "file", Path: temporary}).String() + "?mode=ro"
	check, err := sql.Open("sqlite", backupURL)
	if err != nil {
		return err
	}
	defer check.Close()
	var result string
	if err := check.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("check snapshot: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("snapshot integrity check failed: %s", result)
	}
	rows, err := check.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("check snapshot foreign keys: %w", err)
	}
	if rows.Next() {
		rows.Close()
		return errors.New("snapshot has foreign key violations")
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := check.Close(); err != nil {
		return err
	}
	if err := os.Link(temporary, destination); err != nil {
		return fmt.Errorf("publish backup without replacing an existing file: %w", err)
	}
	return nil
}
