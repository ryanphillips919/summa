package ledger

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestLedgerMigrationPreservesLegacyTriggers(t *testing.T) {
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy-triggers.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, "../db/migrations", 3); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"CREATE TRIGGER entries_no_update BEFORE UPDATE ON entries BEGIN SELECT RAISE(ABORT, 'legacy'); END",
		"CREATE TRIGGER entries_no_delete BEFORE DELETE ON entries BEGIN SELECT RAISE(ABORT, 'legacy'); END",
		"CREATE TRIGGER transactions_no_update BEFORE UPDATE ON transactions BEGIN SELECT RAISE(ABORT, 'legacy'); END",
		"CREATE TRIGGER transactions_no_delete BEFORE DELETE ON transactions BEGIN SELECT RAISE(ABORT, 'legacy'); END",
	} {
		if _, err := conn.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := goose.UpTo(conn, "../db/migrations", 4); err != nil {
		t.Fatalf("upgrade with legacy triggers: %v", err)
	}
	if err := goose.DownTo(conn, "../db/migrations", 3); err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name IN ('entries_no_update', 'entries_no_delete', 'transactions_no_update', 'transactions_no_delete')").Scan(&count); err != nil || count != 4 {
		t.Fatalf("legacy triggers after downgrade = %d, %v", count, err)
	}
}

func TestFinalizationMigration(t *testing.T) {
	for _, balanced := range []bool{true, false} {
		name := "unbalanced"
		if balanced {
			name = "balanced"
		}
		t.Run(name, func(t *testing.T) {
			conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetMaxOpenConns(1)
			if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
				t.Fatal(err)
			}
			if err := goose.SetDialect("sqlite3"); err != nil {
				t.Fatal(err)
			}
			if err := goose.UpTo(conn, "../db/migrations", 4); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{
				"INSERT INTO organizations (id, name) VALUES (1, 'Legacy')",
				"INSERT INTO accounts (id, organization_id, name, type) VALUES (1, 1, 'Cash', 'asset'), (2, 1, 'Sales', 'revenue')",
				"INSERT INTO transactions (id, organization_id, date, description) VALUES (1, 1, '2026-01-01', 'Legacy')",
				"INSERT INTO entries (transaction_id, account_id, direction, amount) VALUES (1, 1, 'debit', 100)",
			} {
				if _, err := conn.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			if balanced {
				if _, err := conn.Exec("INSERT INTO entries (transaction_id, account_id, direction, amount) VALUES (1, 2, 'credit', 100)"); err != nil {
					t.Fatal(err)
				}
			}
			err = goose.Up(conn, "../db/migrations")
			if !balanced {
				if err == nil {
					t.Fatal("migration accepted unbalanced historical transaction")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var recordedAt sql.NullString
			if err := conn.QueryRow("SELECT finalized_at FROM ledger_finalizations WHERE transaction_id = 1").Scan(&recordedAt); err != nil {
				t.Fatal(err)
			}
			if recordedAt.Valid {
				t.Fatalf("legacy transaction given fabricated posting time: %s", recordedAt.String)
			}
			if _, err := conn.Exec("INSERT INTO entries (transaction_id, account_id, direction, amount) VALUES (1, 1, 'debit', 1)"); err == nil {
				t.Fatal("legacy transaction was not protected")
			}
		})
	}
}
