package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens the SQLite database at path and applies any pending migrations.
// Every connection enforces foreign keys; without them the schema's
// organization and append-only protections silently stop working.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	params := url.Values{
		"_pragma": {
			"foreign_keys(1)",
			"journal_mode(WAL)",
			"busy_timeout(5000)",
		},
		// Take the write lock when a transaction begins. Postings read then
		// write, and a deferred transaction can't wait to upgrade its lock,
		// so concurrent postings would fail with SQLITE_BUSY instead of queuing.
		"_txlock": {"immediate"},
	}
	conn, err := sql.Open("sqlite", "file:"+path+"?"+params.Encode())
	if err != nil {
		return nil, fmt.Errorf("open database %q: %w", path, err)
	}
	if err := migrate(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func migrate(ctx context.Context, conn *sql.DB) error {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, files)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
