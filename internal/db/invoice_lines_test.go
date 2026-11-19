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
}
