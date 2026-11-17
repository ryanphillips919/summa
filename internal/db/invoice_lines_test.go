package db

import (
	"database/sql"
	"errors"
	"testing"
)

func TestInvoiceLinesAndTotal(t *testing.T) {
	conn := newTestDB(t)
	queries := New(conn)

	firstOrg, err := queries.CreateOrganization(t.Context(), "First organization")
	if err != nil {
		t.Fatalf("create first organization: %v", err)
	}
	secondOrg, err := queries.CreateOrganization(t.Context(), "Second organization")
	if err != nil {
		t.Fatalf("create second organization: %v", err)
	}
	firstCustomer, err := queries.CreateCustomer(t.Context(), CreateCustomerParams{
		OrganizationID: firstOrg.ID,
		Name:           "Alice",
	})
	if err != nil {
		t.Fatalf("create first customer: %v", err)
	}
	firstInvoice, err := queries.CreateInvoice(t.Context(), CreateInvoiceParams{
		OrganizationID: firstOrg.ID,
		CustomerID:     firstCustomer.ID,
		Number:         "INV-001",
		IssueDate:      "2026-11-06",
		DueDate:        "2026-12-06",
	})
	if err != nil {
		t.Fatalf("create first invoice: %v", err)
	}
	firstScope := InvoiceTotalParams{OrganizationID: firstOrg.ID, InvoiceID: firstInvoice.ID}
	total, err := queries.InvoiceTotal(t.Context(), firstScope)
	if err != nil || total != 0 {
		t.Fatalf("empty invoice total = %d, error = %v; want 0", total, err)
	}

	firstLine, err := queries.CreateInvoiceLine(t.Context(), CreateInvoiceLineParams{
		OrganizationID: firstOrg.ID,
		InvoiceID:      firstInvoice.ID,
		Description:    "Consulting",
		Quantity:       2,
		UnitPriceCents: 1250,
	})
	if err != nil {
		t.Fatalf("create first line: %v", err)
	}
	secondLine, err := queries.CreateInvoiceLine(t.Context(), CreateInvoiceLineParams{
		OrganizationID: firstOrg.ID,
		InvoiceID:      firstInvoice.ID,
		Description:    "Materials",
		Quantity:       1,
		UnitPriceCents: 499,
	})
	if err != nil {
		t.Fatalf("create second line: %v", err)
	}
	lines, err := queries.ListInvoiceLines(t.Context(), ListInvoiceLinesParams{
		OrganizationID: firstOrg.ID,
		InvoiceID:      firstInvoice.ID,
	})
	if err != nil {
		t.Fatalf("list first invoice's lines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines for first invoice, want 2", len(lines))
	}
	if lines[0] != firstLine || lines[1] != secondLine {
		t.Errorf("got lines %+v, want [%+v %+v]", lines, firstLine, secondLine)
	}

	total, err = queries.InvoiceTotal(t.Context(), firstScope)
	if err != nil || total != 2999 {
		t.Errorf("first invoice total = %d, error = %v; want 2999", total, err)
	}

	// The second organization cannot read or add lines on the first invoice.
	otherLines, err := queries.ListInvoiceLines(t.Context(), ListInvoiceLinesParams{
		OrganizationID: secondOrg.ID,
		InvoiceID:      firstInvoice.ID,
	})
	if err != nil || len(otherLines) != 0 {
		t.Errorf("other organization's lines = %+v, error = %v; want none", otherLines, err)
	}
	_, err = queries.InvoiceTotal(t.Context(), InvoiceTotalParams{
		OrganizationID: secondOrg.ID,
		InvoiceID:      firstInvoice.ID,
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("other organization's total error = %v, want sql.ErrNoRows", err)
	}
	_, err = queries.CreateInvoiceLine(t.Context(), CreateInvoiceLineParams{
		OrganizationID: secondOrg.ID,
		InvoiceID:      firstInvoice.ID,
		Description:    "Wrong organization",
		Quantity:       1,
		UnitPriceCents: 100,
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("cross-organization create error = %v, want sql.ErrNoRows", err)
	}

	// A line amount must fit in integer cents.
	_, err = queries.CreateInvoiceLine(t.Context(), CreateInvoiceLineParams{
		OrganizationID: firstOrg.ID,
		InvoiceID:      firstInvoice.ID,
		Description:    "Too large",
		Quantity:       9223372036854775807,
		UnitPriceCents: 2,
	})
	if err == nil {
		t.Error("created a line whose amount overflows integer cents")
	}

	update := UpdateInvoiceLineParams{LineID: firstLine.ID, InvoiceID: firstInvoice.ID, OrganizationID: secondOrg.ID, Description: "Updated", Quantity: 3, UnitPriceCents: 1200}
	if _, err := queries.UpdateInvoiceLine(t.Context(), update); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-organization update: %v", err)
	}
	if _, err := queries.DeleteInvoiceLine(t.Context(), DeleteInvoiceLineParams{LineID: secondLine.ID, InvoiceID: firstInvoice.ID, OrganizationID: secondOrg.ID}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-organization delete: %v", err)
	}
	update.OrganizationID = firstOrg.ID
	if _, err := queries.UpdateInvoiceLine(t.Context(), update); err != nil {
		t.Fatal(err)
	}
	if total, err := queries.InvoiceTotal(t.Context(), firstScope); err != nil || total != 4099 {
		t.Fatalf("updated total %d, %v", total, err)
	}
	if _, err := queries.DeleteInvoiceLine(t.Context(), DeleteInvoiceLineParams{LineID: secondLine.ID, InvoiceID: firstInvoice.ID, OrganizationID: firstOrg.ID}); err != nil {
		t.Fatal(err)
	}
	txn, err := queries.CreateTransaction(t.Context(), CreateTransactionParams{OrganizationID: firstOrg.ID, Date: "2026-11-06", Description: "Posted"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queries.MarkInvoicePosted(t.Context(), MarkInvoicePostedParams{InvoiceID: firstInvoice.ID, OrganizationID: firstOrg.ID, TransactionID: sql.NullInt64{Int64: txn.ID, Valid: true}}); err == nil {
		t.Fatal("linked an unfinalized transaction")
	}
	otherTxn, err := queries.CreateTransaction(t.Context(), CreateTransactionParams{OrganizationID: secondOrg.ID, Date: "2026-11-06", Description: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	otherAccount, err := queries.CreateAccount(t.Context(), CreateAccountParams{OrganizationID: secondOrg.ID, Name: "Other", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"debit", "credit"} {
		if err := queries.CreateEntry(t.Context(), CreateEntryParams{TransactionID: otherTxn.ID, AccountID: otherAccount.ID, Direction: direction, Amount: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.ExecContext(t.Context(), "INSERT INTO ledger_finalizations (transaction_id) VALUES (?)", otherTxn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.MarkInvoicePosted(t.Context(), MarkInvoicePostedParams{InvoiceID: firstInvoice.ID, OrganizationID: firstOrg.ID, TransactionID: sql.NullInt64{Int64: otherTxn.ID, Valid: true}}); err == nil {
		t.Fatal("linked another organization's transaction")
	}
	asset, err := queries.CreateAccount(t.Context(), CreateAccountParams{OrganizationID: firstOrg.ID, Name: "Receivable", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	revenue, err := queries.CreateAccount(t.Context(), CreateAccountParams{OrganizationID: firstOrg.ID, Name: "Revenue", Type: "revenue"})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []CreateEntryParams{{TransactionID: txn.ID, AccountID: asset.ID, Direction: "debit", Amount: 3600}, {TransactionID: txn.ID, AccountID: revenue.ID, Direction: "credit", Amount: 3600}} {
		if err := queries.CreateEntry(t.Context(), line); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.ExecContext(t.Context(), "INSERT INTO ledger_finalizations (transaction_id) VALUES (?)", txn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.MarkInvoicePosted(t.Context(), MarkInvoicePostedParams{InvoiceID: firstInvoice.ID, OrganizationID: firstOrg.ID, TransactionID: sql.NullInt64{Int64: txn.ID, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.UpdateInvoiceLine(t.Context(), update); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("posted update: %v", err)
	}
	if _, err := queries.DeleteInvoiceLine(t.Context(), DeleteInvoiceLineParams{LineID: firstLine.ID, InvoiceID: firstInvoice.ID, OrganizationID: firstOrg.ID}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("posted delete: %v", err)
	}
	for _, query := range []string{
		"UPDATE invoice_lines SET unit_price_cents = 1 WHERE id = ?",
		"DELETE FROM invoice_lines WHERE id = ?",
	} {
		if _, err := conn.ExecContext(t.Context(), query, firstLine.ID); err == nil {
			t.Fatalf("database allowed posted line change: %s", query)
		}
	}
	if _, err := conn.ExecContext(t.Context(), "UPDATE invoices SET due_date = '2026-01-01' WHERE id = ?", firstInvoice.ID); err == nil {
		t.Fatal("database allowed posted invoice change")
	}
}
