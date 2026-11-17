package ledger

import (
	"testing"

	"summa/internal/db"
)

func TestTrialBalanceAsOfAndOrganization(t *testing.T) {
	conn := newLedgerTestDB(t)
	ctx := t.Context()
	q := db.New(conn)
	org, err := q.CreateOrganization(ctx, "First")
	if err != nil {
		t.Fatal(err)
	}
	other, err := q.CreateOrganization(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	cash := createTestAccount(t, q, org.ID, "Cash", "asset")
	sales := createTestAccount(t, q, org.ID, "Sales", "revenue")
	otherCash := createTestAccount(t, q, other.ID, "Other cash", "asset")
	otherSales := createTestAccount(t, q, other.ID, "Other sales", "revenue")
	post := func(orgID, debit, credit, amount int64, date string) {
		t.Helper()
		_, err := Post(ctx, conn, db.CreateTransactionParams{OrganizationID: orgID, Date: date, Description: "Sale"}, []db.CreateEntryParams{{AccountID: debit, Direction: "debit", Amount: amount}, {AccountID: credit, Direction: "credit", Amount: amount}})
		if err != nil {
			t.Fatal(err)
		}
	}
	post(org.ID, cash.ID, sales.ID, 500, "2026-01-01")
	post(org.ID, sales.ID, cash.ID, 200, "2026-01-02")
	post(other.ID, otherCash.ID, otherSales.ID, 900, "2026-01-01")
	// A committed incomplete transaction is ignored by this report.
	draft, err := q.CreateTransaction(ctx, db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-01-01", Description: "Incomplete"})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateEntry(ctx, db.CreateEntryParams{TransactionID: draft.ID, AccountID: cash.ID, Direction: "debit", Amount: 100}); err != nil {
		t.Fatal(err)
	}
	transactions, err := q.ListTransactions(ctx, org.ID)
	if err != nil || len(transactions) != 2 {
		t.Fatalf("posted transaction list = %v, %v", transactions, err)
	}
	entries, err := q.ListEntries(ctx, draft.ID)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unfinalized entries exposed: %v, %v", entries, err)
	}
	totals, err := q.AccountTotals(ctx, org.ID)
	if err != nil || len(totals) != 2 || totals[0].Debits != 500 {
		t.Fatalf("account totals include unfinalized entry: %v, %v", totals, err)
	}
	for _, tc := range []struct {
		date   string
		amount int64
	}{{"2025-12-31", 0}, {"2026-01-01", 500}, {"2026-01-02", 300}} {
		report, err := BuildTrialBalance(ctx, conn, org.ID, tc.date)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Rows) != 2 || report.TotalDebit != tc.amount || report.TotalCredit != tc.amount {
			t.Fatalf("%s: got %+v", tc.date, report)
		}
		if report.Rows[0].Name != "Cash" || report.Rows[0].Debit != tc.amount || report.Rows[1].Credit != tc.amount {
			t.Fatalf("%s: rows = %+v", tc.date, report.Rows)
		}
	}
	if _, err := BuildTrialBalance(ctx, conn, org.ID, "2026-02-30"); err == nil {
		t.Fatal("accepted invalid date")
	}
	income, err := BuildIncomeStatement(ctx, conn, org.ID, "2026-01-02", "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if income.Revenue != -200 || income.NetIncome != -200 {
		t.Fatalf("contra-revenue period = %+v", income)
	}
	balance, err := BuildBalanceSheet(ctx, conn, org.ID, "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if balance.Assets != 300 || balance.AccumulatedIncome != 300 || balance.TotalLiabilitiesAndEquity != 300 {
		t.Fatalf("balance sheet = %+v", balance)
	}
}
