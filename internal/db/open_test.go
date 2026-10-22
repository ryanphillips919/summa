package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func openTest(t *testing.T) (*sql.DB, *Queries) {
	t.Helper()
	conn, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, New(conn)
}

func TestForeignKeysEnabledOnEveryConnection(t *testing.T) {
	conn, _ := openTest(t)
	conn.SetMaxOpenConns(4)
	ctx := context.Background()
	var held []*sql.Conn
	for range 4 {
		c, err := conn.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
		var on int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&on); err != nil {
			t.Fatal(err)
		}
		if on != 1 {
			t.Fatal("foreign_keys is off on a pooled connection")
		}
	}
	for _, c := range held {
		c.Close()
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for range 2 {
		conn, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
}

func TestPostAndTrialBalance(t *testing.T) {
	ctx := context.Background()
	conn, q := openTest(t)

	org, err := q.CreateOrganization(ctx, "Modern Bagelry")
	if err != nil {
		t.Fatal(err)
	}
	user, err := q.CreateUser(ctx, CreateUserParams{Email: "sawyer@utah.edu", Name: "Sawyer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.AddMembership(ctx, AddMembershipParams{OrganizationID: org.ID, UserID: user.ID, Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	role, err := q.GetMembershipRole(ctx, GetMembershipRoleParams{OrganizationID: org.ID, UserID: user.ID})
	if err != nil || role != "owner" {
		t.Fatalf("role = %q, %v", role, err)
	}

	account := func(code, name, typ string) Account {
		t.Helper()
		a, err := q.CreateAccount(ctx, CreateAccountParams{OrganizationID: org.ID, Code: code, Name: name, Type: typ})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	cash := account("1000", "Cash", "asset")
	equity := account("3000", "Equity", "equity")

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	qtx := q.WithTx(tx)
	posted, err := qtx.InsertTransaction(ctx, InsertTransactionParams{
		OrganizationID: org.ID, OccurredOn: "2026-10-01", Description: "Owner investment", PostedBy: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []InsertEntryParams{
		{AccountID: cash.ID, Direction: "debit", Amount: 100_000_000},
		{AccountID: equity.ID, Direction: "credit", Amount: 100_000_000},
	} {
		e.OrganizationID, e.TransactionID = org.ID, posted.ID
		if err := qtx.InsertEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	totals, err := qtx.TransactionTotals(ctx, posted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if totals.EntryCount != 2 || totals.Debits != totals.Credits {
		t.Fatalf("unbalanced: %+v", totals)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	rows, err := q.TrialBalance(ctx, TrialBalanceParams{OrganizationID: org.ID, AsOf: "2026-10-31"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Code != "1000" || rows[0].Debits != 100_000_000 || rows[1].Credits != 100_000_000 {
		t.Fatalf("trial balance = %+v", rows)
	}
	if before, _ := q.TrialBalance(ctx, TrialBalanceParams{OrganizationID: org.ID, AsOf: "2026-09-30"}); len(before) != 0 {
		t.Errorf("trial balance before the posting = %+v, want empty", before)
	}

	// The append-only triggers survive the goose + driver path.
	if _, err := conn.ExecContext(ctx, "UPDATE entries SET amount = 1"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("updating an entry: err = %v, want append-only rejection", err)
	}
}
