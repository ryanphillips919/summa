package invoicing

import (
	"database/sql"
	"errors"
	"testing"

	"summa/internal/db"
)

func TestRecordReceiptAndIdempotency(t *testing.T) {
	f := newInvoiceFixture(t)
	ctx := t.Context()
	cash, err := f.queries.CreateAccount(ctx, db.CreateAccountParams{OrganizationID: f.org.ID, Name: "Cash", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	f.addLine(t)
	key := "receipt-request-0001"
	if _, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, cash.ID, 1000, "2026-11-06", key); err == nil {
		t.Fatal("accepted payment for draft")
	}
	if _, err := f.post(t); err != nil {
		t.Fatal(err)
	}
	other, err := f.queries.CreateOrganization(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	otherCash, err := f.queries.CreateAccount(ctx, db.CreateAccountParams{OrganizationID: other.ID, Name: "Cash", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordReceipt(ctx, f.conn, other.ID, f.invoice.ID, otherCash.ID, 1000, "2026-11-06", key); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-organization invoice: %v", err)
	}
	if _, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, otherCash.ID, 1000, "2026-11-06", key); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-organization deposit: %v", err)
	}
	if _, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, cash.ID, 1000, "2026-11-05", key); err == nil {
		t.Fatal("accepted payment before invoice")
	}
	if _, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, f.receivable.ID, 1000, "2026-11-06", key); err == nil {
		t.Fatal("accepted receivable account as deposit")
	}
	first, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, cash.ID, 1000, "2026-11-06", key)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := BuildAging(ctx, f.conn, f.org.ID, "2026-11-06")
	if err != nil || partial.Current != 1500 || partial.Outstanding != 1500 {
		t.Fatalf("partial aging: %+v, %v", partial, err)
	}
	again, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, cash.ID, 1000, "2026-11-06", key)
	if err != nil || again.ID != first.ID {
		t.Fatalf("idempotent retry: %+v %v", again, err)
	}
	if _, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, cash.ID, 1200, "2026-11-06", key); err == nil {
		t.Fatal("accepted key with changed details")
	}
	if _, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, cash.ID, 1501, "2026-11-07", "receipt-request-0002"); err == nil {
		t.Fatal("accepted overpayment")
	}
	second, err := RecordReceipt(ctx, f.conn, f.org.ID, f.invoice.ID, cash.ID, 1500, "2026-11-07", "receipt-request-0002")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := f.queries.ListEntries(ctx, second.ID)
	if err != nil || len(entries) != 2 || entries[0].AccountID != cash.ID || entries[0].Direction != "debit" || entries[1].AccountID != f.receivable.ID || entries[1].Direction != "credit" {
		t.Fatalf("receipt entries = %+v, %v", entries, err)
	}
	receipts, err := ListReceipts(ctx, f.conn, f.org.ID, f.invoice.ID)
	if err != nil || len(receipts) != 2 || receipts[0].Amount != 1000 || receipts[1].Amount != 1500 {
		t.Fatalf("receipts = %+v, %v", receipts, err)
	}
	for _, query := range []string{
		"UPDATE invoice_receipts SET amount = 1 WHERE transaction_id = ?",
		"DELETE FROM invoice_receipts WHERE transaction_id = ?",
	} {
		if _, err := f.conn.ExecContext(ctx, query, first.ID); err == nil {
			t.Fatalf("database accepted receipt change: %s", query)
		}
	}
	earlier, err := BuildAging(ctx, f.conn, f.org.ID, "2026-11-06")
	if err != nil || earlier.Outstanding != 1500 {
		t.Fatalf("as-of aging: %+v, %v", earlier, err)
	}
	paid, err := BuildAging(ctx, f.conn, f.org.ID, "2026-11-07")
	if err != nil || paid.Outstanding != 0 || len(paid.Rows) != 0 {
		t.Fatalf("paid aging: %+v, %v", paid, err)
	}
	transactions, err := f.queries.ListTransactions(ctx, f.org.ID)
	if err != nil || len(transactions) != 3 {
		t.Fatalf("transactions = %+v, %v", transactions, err)
	}
}

func TestAgingBuckets(t *testing.T) {
	f := newInvoiceFixture(t)
	f.addLine(t)
	if _, err := f.post(t); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		date   string
		bucket int
	}{
		{"2026-12-06", 0}, {"2026-12-07", 1}, {"2027-01-05", 1}, {"2027-01-06", 2}, {"2027-02-05", 3}, {"2027-03-07", 4},
	} {
		report, err := BuildAging(t.Context(), f.conn, f.org.ID, tc.date)
		if err != nil {
			t.Fatal(err)
		}
		values := []int64{report.Current, report.Days1To30, report.Days31To60, report.Days61To90, report.Over90}
		for i, value := range values {
			expected := int64(0)
			if i == tc.bucket {
				expected = 2500
			}
			if value != expected {
				t.Fatalf("%s: buckets = %v", tc.date, values)
			}
		}
	}
}
