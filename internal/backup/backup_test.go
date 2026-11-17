package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"summa/internal/db"
	"summa/internal/ledger"
)

func TestSnapshotRestoreAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	source, output := filepath.Join(dir, "source.db"), filepath.Join(dir, "backup.db")
	conn, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(conn, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	org, err := q.CreateOrganization(t.Context(), "Business")
	if err != nil {
		t.Fatal(err)
	}
	cash, err := q.CreateAccount(t.Context(), db.CreateAccountParams{OrganizationID: org.ID, Name: "Cash", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	sales, err := q.CreateAccount(t.Context(), db.CreateAccountParams{OrganizationID: org.ID, Name: "Sales", Type: "revenue"})
	if err != nil {
		t.Fatal(err)
	}
	post := func(amount int64) {
		t.Helper()
		_, err := ledger.Post(t.Context(), conn, db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-01-01", Description: "Sale"}, []db.CreateEntryParams{{AccountID: cash.ID, Direction: "debit", Amount: amount}, {AccountID: sales.ID, Direction: "credit", Amount: amount}})
		if err != nil {
			t.Fatal(err)
		}
	}
	post(1250)
	if err := Create(context.Background(), source, output); err != nil {
		t.Fatal(err)
	}
	post(500)
	// Restore to a separate file and confirm the books remain usable for posting.
	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	restorePath := filepath.Join(dir, "restored.db")
	if err := os.WriteFile(restorePath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite", restorePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	trial, err := ledger.BuildTrialBalance(t.Context(), restored, org.ID, "2026-12-31")
	if err != nil || trial.TotalDebit != 1250 || trial.TotalCredit != 1250 {
		t.Fatalf("restored trial balance = %+v, %v", trial, err)
	}
	_, err = ledger.Post(t.Context(), restored, db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-01-02", Description: "After restore"}, []db.CreateEntryParams{{AccountID: cash.ID, Direction: "debit", Amount: 250}, {AccountID: sales.ID, Direction: "credit", Amount: 250}})
	if err != nil {
		t.Fatalf("post to restored database: %v", err)
	}
	if err := Create(context.Background(), source, output); err == nil {
		t.Fatal("overwrote existing backup")
	}
	backup, err := sql.Open("sqlite", output)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var count int
	if err := backup.QueryRow(`SELECT count(*) FROM transactions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("existing backup changed: %d, %v", count, err)
	}
}

func TestRejectMissingOrWrongSource(t *testing.T) {
	dir := t.TempDir()
	if err := Create(context.Background(), filepath.Join(dir, "missing.db"), filepath.Join(dir, "out.db")); err == nil {
		t.Fatal("accepted missing source")
	}
	wrong := filepath.Join(dir, "wrong.db")
	if err := os.WriteFile(wrong, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Create(context.Background(), wrong, filepath.Join(dir, "out.db")); err == nil {
		t.Fatal("accepted empty source")
	}
}
