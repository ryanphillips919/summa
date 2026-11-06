package db

import "testing"

// TestInvoicesBelongToOrganizations checks that invoices are listed only for
// their organization and cannot use another organization's customer.
func TestInvoicesBelongToOrganizations(t *testing.T) {
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
	secondCustomer, err := queries.CreateCustomer(t.Context(), CreateCustomerParams{
		OrganizationID: secondOrg.ID,
		Name:           "Bob",
	})
	if err != nil {
		t.Fatalf("create second customer: %v", err)
	}

	// Each organization can use its own invoice number.
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
	_, err = queries.CreateInvoice(t.Context(), CreateInvoiceParams{
		OrganizationID: secondOrg.ID,
		CustomerID:     secondCustomer.ID,
		Number:         "INV-001",
		IssueDate:      "2026-11-06",
		DueDate:        "2026-12-06",
	})
	if err != nil {
		t.Fatalf("create second invoice: %v", err)
	}

	// Listing the first organization must exclude the second one's invoice.
	invoices, err := queries.ListInvoices(t.Context(), firstOrg.ID)
	if err != nil {
		t.Fatalf("list first organization's invoices: %v", err)
	}
	if len(invoices) != 1 {
		t.Fatalf("got %d invoices for first organization, want 1", len(invoices))
	}
	if invoices[0] != firstInvoice {
		t.Errorf("got invoice %+v, want %+v", invoices[0], firstInvoice)
	}

	// The customer must belong to the same organization as the invoice.
	_, err = queries.CreateInvoice(t.Context(), CreateInvoiceParams{
		OrganizationID: firstOrg.ID,
		CustomerID:     secondCustomer.ID,
		Number:         "INV-002",
		IssueDate:      "2026-11-06",
		DueDate:        "2026-12-06",
	})
	if err == nil {
		t.Error("created an invoice for a customer in another organization")
	}
}
