package main

import (
	"path/filepath"
	"testing"

	"summa/internal/db"
)

func TestDatabaseBootstrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "summa.db")
	conn, err := openDatabase(t.Context(), path)
	if err != nil {
		t.Fatalf("open new database: %v", err)
	}
	queries := db.New(conn)
	orgID, err := seedDevData(t.Context(), conn, queries)
	if err != nil {
		conn.Close()
		t.Fatalf("seed new database: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close new database: %v", err)
	}

	// Reopening must reuse the migrated schema and leave the seed unchanged.
	reopened, err := openDatabase(t.Context(), path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	queries = db.New(reopened)
	gotOrgID, err := seedDevData(t.Context(), reopened, queries)
	if err != nil {
		t.Fatalf("seed existing database: %v", err)
	}
	if gotOrgID != orgID {
		t.Errorf("organization ID after restart = %d, want %d", gotOrgID, orgID)
	}
	accounts, err := queries.ListAccounts(t.Context(), orgID)
	if err != nil {
		t.Fatalf("list seeded accounts: %v", err)
	}
	if len(accounts) != 8 {
		t.Errorf("seeded accounts = %d, want 8", len(accounts))
	}
	transactions, err := queries.ListTransactions(t.Context(), orgID)
	if err != nil {
		t.Fatalf("list seeded transactions: %v", err)
	}
	if len(transactions) != 4 {
		t.Errorf("seeded transactions = %d, want 4", len(transactions))
	}
	var foreignKeys int
	if err := reopened.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("check foreign keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Error("foreign-key enforcement is disabled")
	}
}
