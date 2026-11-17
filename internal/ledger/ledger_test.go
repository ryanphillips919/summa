package ledger

import (
	"math"
	"testing"

	"summa/internal/db"
)

func TestPostRejectsOverflowingEntryTotals(t *testing.T) {
	conn := newLedgerTestDB(t)
	queries := db.New(conn)
	org, err := queries.CreateOrganization(t.Context(), "Overflow test")
	if err != nil {
		t.Fatal(err)
	}
	asset := createTestAccount(t, queries, org.ID, "Cash", "asset")
	revenue := createTestAccount(t, queries, org.ID, "Revenue", "revenue")
	_, err = Post(t.Context(), conn, db.CreateTransactionParams{
		OrganizationID: org.ID, Date: "2026-10-31", Description: "Overflow",
	}, []db.CreateEntryParams{
		{AccountID: asset.ID, Direction: "debit", Amount: math.MaxInt64},
		{AccountID: asset.ID, Direction: "debit", Amount: math.MaxInt64},
		{AccountID: asset.ID, Direction: "debit", Amount: 3},
		{AccountID: revenue.ID, Direction: "credit", Amount: 1},
	})
	if err == nil {
		t.Fatal("accepted an overflowing, unbalanced transaction")
	}
	transactions, err := queries.ListTransactions(t.Context(), org.ID)
	if err != nil || len(transactions) != 0 {
		t.Fatalf("transactions after rejected post = %d, error = %v", len(transactions), err)
	}
}

func TestLedgerOrganizationAndImmutability(t *testing.T) {
	conn := newLedgerTestDB(t)
	ctx := t.Context()
	q := db.New(conn)
	org, err := q.CreateOrganization(ctx, "First")
	if err != nil {
		t.Fatal(err)
	}
	other, err := q.CreateOrganization(ctx, "Second")
	if err != nil {
		t.Fatal(err)
	}
	cash := createTestAccount(t, q, org.ID, "Cash", "asset")
	revenue := createTestAccount(t, q, org.ID, "Revenue", "revenue")
	foreign := createTestAccount(t, q, other.ID, "Foreign", "asset")
	params := db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-10-31", Description: "Sale"}
	_, err = Post(ctx, conn, params, []db.CreateEntryParams{
		{AccountID: foreign.ID, Direction: "debit", Amount: 100},
		{AccountID: revenue.ID, Direction: "credit", Amount: 100},
	})
	if err == nil {
		t.Fatal("accepted cross-organization posting")
	}
	transactions, err := q.ListTransactions(ctx, org.ID)
	if err != nil || len(transactions) != 0 {
		t.Fatalf("rejected posting left transactions: %v, %v", transactions, err)
	}

	posted, err := Post(ctx, conn, params, []db.CreateEntryParams{
		{AccountID: cash.ID, Direction: "debit", Amount: 100},
		{AccountID: revenue.ID, Direction: "credit", Amount: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := q.ListEntries(ctx, posted.ID)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %v, error = %v", entries, err)
	}
	for _, tc := range []struct {
		name, query string
		args        []any
	}{
		{"cross-organization insert", "INSERT INTO entries (transaction_id, account_id, direction, amount) VALUES (?, ?, 'debit', 1)", []any{posted.ID, foreign.ID}},
		{"insert after finalization", "INSERT INTO entries (transaction_id, account_id, direction, amount) VALUES (?, ?, 'debit', 1)", []any{posted.ID, cash.ID}},
		{"entry update", "UPDATE entries SET amount = 1 WHERE id = ?", []any{entries[0].ID}},
		{"entry delete", "DELETE FROM entries WHERE id = ?", []any{entries[0].ID}},
		{"transaction update", "UPDATE transactions SET description = 'changed' WHERE id = ?", []any{posted.ID}},
		{"transaction delete", "DELETE FROM transactions WHERE id = ?", []any{posted.ID}},
		{"account transfer", "UPDATE accounts SET organization_id = ? WHERE id = ?", []any{other.ID, cash.ID}},
		{"posted account type", "UPDATE accounts SET type = 'expense' WHERE id = ?", []any{cash.ID}},
		{"finalization update", "UPDATE ledger_finalizations SET finalized_at = 'changed' WHERE transaction_id = ?", []any{posted.ID}},
		{"finalization delete", "DELETE FROM ledger_finalizations WHERE transaction_id = ?", []any{posted.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := conn.ExecContext(ctx, tc.query, tc.args...); err == nil {
				t.Fatal("database accepted forbidden change")
			}
		})
	}
}

func TestDatabaseRejectsUnbalancedFinalization(t *testing.T) {
	conn := newLedgerTestDB(t)
	ctx := t.Context()
	q := db.New(conn)
	org, err := q.CreateOrganization(ctx, "Company")
	if err != nil {
		t.Fatal(err)
	}
	cash := createTestAccount(t, q, org.ID, "Cash", "asset")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	queries := db.New(tx)
	transaction, err := queries.CreateTransaction(ctx, db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-10-31", Description: "Incomplete"})
	if err != nil {
		t.Fatal(err)
	}
	if err := queries.CreateEntry(ctx, db.CreateEntryParams{TransactionID: transaction.ID, AccountID: cash.ID, Direction: "debit", Amount: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO ledger_finalizations (transaction_id) VALUES (?)", transaction.ID); err == nil {
		t.Fatal("finalized an unbalanced transaction")
	}
}

func TestPostRejectsInvalidHeader(t *testing.T) {
	conn := newLedgerTestDB(t)
	q := db.New(conn)
	org, err := q.CreateOrganization(t.Context(), "Company")
	if err != nil {
		t.Fatal(err)
	}
	cash := createTestAccount(t, q, org.ID, "Cash", "asset")
	sales := createTestAccount(t, q, org.ID, "Sales", "revenue")
	entries := []db.CreateEntryParams{{AccountID: cash.ID, Direction: "debit", Amount: 1}, {AccountID: sales.ID, Direction: "credit", Amount: 1}}
	for _, params := range []db.CreateTransactionParams{
		{OrganizationID: org.ID, Date: "2026-02-30", Description: "Invalid date"},
		{OrganizationID: org.ID, Date: "2026-01-01", Description: "  "},
	} {
		if _, err := Post(t.Context(), conn, params, entries); err == nil {
			t.Fatalf("accepted invalid transaction %+v", params)
		}
	}
}
