package ledger

import (
	"strings"
	"testing"

	"summa/internal/db"
)

func TestPeriodLockRejectsBackdatedPosting(t *testing.T) {
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
	sales := createTestAccount(t, q, org.ID, "Sales", "revenue")
	otherCash := createTestAccount(t, q, other.ID, "Cash", "asset")
	otherSales := createTestAccount(t, q, other.ID, "Sales", "revenue")
	post := func(orgID, debit, credit int64, date string) error {
		_, err := Post(ctx, conn, db.CreateTransactionParams{OrganizationID: orgID, Date: date, Description: "Sale"}, []db.CreateEntryParams{{AccountID: debit, Direction: "debit", Amount: 100}, {AccountID: credit, Direction: "credit", Amount: 100}})
		return err
	}
	if err := post(org.ID, cash.ID, sales.ID, "2026-01-15"); err != nil {
		t.Fatal(err)
	}
	if err := LockThrough(ctx, conn, org.ID, "2026-01-31"); err != nil {
		t.Fatal(err)
	}
	if err := LockThrough(ctx, conn, org.ID, "2026-01-31"); err != nil {
		t.Fatal(err)
	}
	if err := post(org.ID, cash.ID, sales.ID, "2026-01-31"); err == nil || !strings.Contains(err.Error(), "period is locked") {
		t.Fatalf("posting in closed period: %v", err)
	}
	transactions, err := q.ListTransactions(ctx, org.ID)
	if err != nil || len(transactions) != 1 {
		t.Fatalf("blocked posting left transactions: %v, %v", transactions, err)
	}
	if err := post(org.ID, cash.ID, sales.ID, "2026-02-01"); err != nil {
		t.Fatal(err)
	}
	if err := post(other.ID, otherCash.ID, otherSales.ID, "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	if err := LockThrough(ctx, conn, org.ID, "2026-01-01"); err == nil {
		t.Fatal("lock moved backward")
	}
	if err := LockThrough(ctx, conn, org.ID, "2026-02-30"); err == nil {
		t.Fatal("accepted invalid date")
	}
	if err := LockThrough(ctx, conn, org.ID, "2026-02-28"); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM accounting_period_lock_events WHERE organization_id = ?`, org.ID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("lock events = %d, %v", events, err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM accounting_period_locks WHERE organization_id = ?`, org.ID); err == nil {
		t.Fatal("deleted period lock")
	}
}
