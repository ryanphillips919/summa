package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// newCustomerTestDB gives each test a fresh database with the project's migrations.
func newCustomerTestDB(test *testing.T) *sql.DB {
	test.Helper()

	conn, err := sql.Open("sqlite", filepath.Join(test.TempDir(), "customers.db"))
	if err != nil {
		test.Fatalf("open test database: %v", err)
	}
	test.Cleanup(func() {
		if err := conn.Close(); err != nil {
			test.Errorf("close test database: %v", err)
		}
	})

	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(test.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		test.Fatalf("enable foreign keys: %v", err)
	}

	if err := goose.SetDialect("sqlite3"); err != nil {
		test.Fatalf("set migration dialect: %v", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		test.Fatalf("apply migrations: %v", err)
	}

	return conn
}

func TestCustomersBelongToOrganizations(t *testing.T) {
	conn := newCustomerTestDB(t)
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
