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
