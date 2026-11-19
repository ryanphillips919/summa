package db

import (
	"database/sql"
	"testing"
)

func TestCustomersBelongToOrganizations(t *testing.T) {
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
		Email:          sql.NullString{String: "alice@example.com", Valid: true},
	})
	if err != nil {
		t.Fatalf("create first customer: %v", err)
	}
	_, err = queries.CreateCustomer(t.Context(), CreateCustomerParams{
		OrganizationID: secondOrg.ID,
		Name:           "Bob",
	})
	if err != nil {
		t.Fatalf("create second customer: %v", err)
	}

	customers, err := queries.ListCustomers(t.Context(), firstOrg.ID)
	if err != nil {
		t.Fatalf("list first organization's customers: %v", err)
	}
	if len(customers) != 1 {
		t.Fatalf("got %d customers for first organization, want 1", len(customers))
	}
	if customers[0] != firstCustomer {
		t.Errorf("got customer %+v, want %+v", customers[0], firstCustomer)
	}
}
