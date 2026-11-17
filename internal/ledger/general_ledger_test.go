package ledger

import (
	"database/sql"
	"errors"
	"testing"

	"summa/internal/db"
)

func TestGeneralLedgerOpeningAndRunningBalances(t *testing.T) {
	conn := newLedgerTestDB(t)
	ctx := t.Context()
	q := db.New(conn)
	org, err := q.CreateOrganization(ctx, "Business")
	if err != nil {
		t.Fatal(err)
	}
	other, err := q.CreateOrganization(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	cash := createTestAccount(t, q, org.ID, "Cash", "asset")
	sales := createTestAccount(t, q, org.ID, "Sales", "revenue")
	post := func(date string, amount int64, debit, credit int64) {
		t.Helper()
		_, err := Post(ctx, conn, db.CreateTransactionParams{OrganizationID: org.ID, Date: date, Description: "Example"}, []db.CreateEntryParams{{AccountID: debit, Direction: "debit", Amount: amount}, {AccountID: credit, Direction: "credit", Amount: amount}})
		if err != nil {
			t.Fatal(err)
		}
	}
	post("2026-01-01", 1000, cash.ID, sales.ID)
	post("2026-02-01", 300, sales.ID, cash.ID)
	post("2026-03-01", 500, cash.ID, sales.ID)
	incomplete, err := q.CreateTransaction(ctx, db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-02-10", Description: "Incomplete"})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateEntry(ctx, db.CreateEntryParams{TransactionID: incomplete.ID, AccountID: cash.ID, Direction: "debit", Amount: 999}); err != nil {
		t.Fatal(err)
	}
	report, err := BuildGeneralLedger(ctx, conn, org.ID, cash.ID, "2026-02-01", "2026-02-28")
	if err != nil {
		t.Fatal(err)
	}
	if report.Opening != 1000 || report.Closing != 700 || len(report.Lines) != 1 || report.Lines[0].Credit != 300 || report.Lines[0].Balance != 700 {
		t.Fatalf("cash report = %+v", report)
	}
	report, err = BuildGeneralLedger(ctx, conn, org.ID, sales.ID, "2026-02-01", "2026-02-28")
	if err != nil || report.Opening != 1000 || report.Closing != 700 || report.Lines[0].Debit != 300 {
		t.Fatalf("sales report = %+v, %v", report, err)
	}
	if _, err := BuildGeneralLedger(ctx, conn, other.ID, cash.ID, "2026-01-01", "2026-12-31"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-organization account: %v", err)
	}
	if _, err := BuildGeneralLedger(ctx, conn, org.ID, cash.ID, "2026-02-01", "2026-01-31"); err == nil {
		t.Fatal("accepted invalid range")
	}
}
