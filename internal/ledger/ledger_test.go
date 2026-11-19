package ledger

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"summa/internal/db"
)

func TestPost(t *testing.T) {
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(t.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(conn, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	org, err := q.CreateOrganization(t.Context(), "Example")
	if err != nil {
		t.Fatal(err)
	}
	cash, err := q.CreateAccount(t.Context(), db.CreateAccountParams{OrganizationID: org.ID, Name: "Cash", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	rent, err := q.CreateAccount(t.Context(), db.CreateAccountParams{OrganizationID: org.ID, Name: "Rent", Type: "expense"})
	if err != nil {
		t.Fatal(err)
	}
	params := db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-10-15", Description: "Rent"}
	entries := []db.CreateEntryParams{
		{AccountID: rent.ID, Direction: "debit", Amount: 30000},
		{AccountID: cash.ID, Direction: "credit", Amount: 30000},
	}
	posted, err := Post(t.Context(), conn, params, entries)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := q.ListEntries(t.Context(), posted.ID)
	if err != nil || len(saved) != 2 {
		t.Fatalf("saved entries = %+v, error = %v; want two entries", saved, err)
	}
	if saved[0].AccountID != rent.ID || saved[0].Direction != "debit" || saved[0].Amount != 30000 ||
		saved[1].AccountID != cash.ID || saved[1].Direction != "credit" || saved[1].Amount != 30000 {
		t.Fatalf("unexpected saved entries: %+v", saved)
	}

	for _, tc := range []struct {
		name    string
		entries []db.CreateEntryParams
	}{
		{"one entry", entries[:1]},
		{"unbalanced", []db.CreateEntryParams{entries[0], {AccountID: cash.ID, Direction: "credit", Amount: 1}}},
		{"invalid second account", []db.CreateEntryParams{entries[0], {AccountID: -1, Direction: "credit", Amount: 30000}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Post(t.Context(), conn, params, tc.entries); err == nil {
				t.Fatal("expected posting to fail")
			}
			transactions, err := q.ListTransactions(t.Context(), org.ID)
			if err != nil || len(transactions) != 1 {
				t.Fatalf("transactions = %+v, error = %v; want only the successful post", transactions, err)
			}
		})
	}
}
