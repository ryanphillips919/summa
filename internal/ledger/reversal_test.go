package ledger

import (
	"database/sql"
	"errors"
	"testing"

	"summa/internal/db"
)

func TestReverseManual(t *testing.T) {
	conn := newLedgerTestDB(t)
	ctx := t.Context()
	q := db.New(conn)
	org, err := q.CreateOrganization(ctx, "Company")
	if err != nil {
		t.Fatal(err)
	}
	other, err := q.CreateOrganization(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	cash := createTestAccount(t, q, org.ID, "Cash", "asset")
	sales := createTestAccount(t, q, org.ID, "Sales", "revenue")
	original, err := Post(ctx, conn, db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-01-03", Description: "Correction"}, []db.CreateEntryParams{{AccountID: cash.ID, Direction: "debit", Amount: 350}, {AccountID: sales.ID, Direction: "credit", Amount: 350}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReverseManual(ctx, conn, org.ID, original.ID, "2026-01-03"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("reversed unmarked transaction: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO manual_journal_transactions (transaction_id) VALUES (?)", original.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ReverseManual(ctx, conn, other.ID, original.ID, "2026-01-03"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-organization reversal: %v", err)
	}
	if _, err := ReverseManual(ctx, conn, org.ID, original.ID, "2026-01-02"); err == nil {
		t.Fatal("accepted reversal before original")
	}
	reversal, err := ReverseManual(ctx, conn, org.ID, original.ID, "2026-01-04")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := q.ListEntries(ctx, reversal.ID)
	if err != nil || len(lines) != 2 || lines[0].AccountID != cash.ID || lines[0].Direction != "credit" || lines[0].Amount != 350 || lines[1].Direction != "debit" {
		t.Fatalf("reversal lines: %v, %v", lines, err)
	}
	if _, err := ReverseManual(ctx, conn, org.ID, original.ID, "2026-01-04"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("duplicate reversal: %v", err)
	}
	if _, err := ReverseManual(ctx, conn, org.ID, reversal.ID, "2026-01-04"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("reversed reversal: %v", err)
	}
	report, err := BuildTrialBalance(ctx, conn, org.ID, "2026-01-04")
	if err != nil || report.TotalDebit != 0 || report.TotalCredit != 0 {
		t.Fatalf("balance after reversal: %+v, %v", report, err)
	}
}
