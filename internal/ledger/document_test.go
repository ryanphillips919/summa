package ledger

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"summa/internal/db"
)

// newLedgerTestDB gives each test a fresh database. Go passes *testing.T to
// each Test function; this helper uses that same test handle to report failures,
// create a temporary directory, and close the database when the test ends.
func newLedgerTestDB(test *testing.T) *sql.DB {
	test.Helper() // Attribute failures in this helper to the calling test.

	// Keep this test's database in its own temporary directory so it cannot
	// change a development database. Go removes the directory after the test.
	conn, err := sql.Open("sqlite", filepath.Join(test.TempDir(), "ledger.db"))
	if err != nil {
		test.Fatalf("open test database: %v", err)
	}
	// Close the connection when the test finishes, even if an assertion fails.
	test.Cleanup(func() {
		if err := conn.Close(); err != nil {
			test.Errorf("close test database: %v", err)
		}
	})
	// Use one connection and enforce the schema's foreign keys.
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(test.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		test.Fatalf("enable foreign keys: %v", err)
	}

	// Create the tables using the project's migrations.
	if err := goose.SetDialect("sqlite3"); err != nil {
		test.Fatalf("set migration dialect: %v", err)
	}
	if err := goose.Up(conn, "../db/migrations"); err != nil {
		test.Fatalf("apply schema migration: %v", err)
	}

	return conn
}

// TestBuildDocumentOneRentPayment posts one $300 rent payment and checks that
// the October report shows a negative Rent amount without a Cash row.
func TestBuildDocumentOneRentPayment(test *testing.T) {
	conn := newLedgerTestDB(test)
	queries := db.New(conn)

	// Create one company and its two accounts.
	company, err := queries.CreateOrganization(test.Context(), "Example Company")
	if err != nil {
		test.Fatalf("create example company: %v", err)
	}
	cash := createTestAccount(test, queries, company.ID, "Cash", "asset")
	rent := createTestAccount(test, queries, company.ID, "Rent", "expense")

	// Record one $300 rent payment.
	_, err = Post(test.Context(), conn, db.CreateTransactionParams{
		OrganizationID: company.ID,
		Date:           "2026-10-15",
		Description:    "October rent payment",
	}, []db.CreateEntryParams{
		{AccountID: rent.ID, Direction: "debit", Amount: 30_000},
		{AccountID: cash.ID, Direction: "credit", Amount: 30_000},
	})
	if err != nil {
		test.Fatalf("post rent payment: %v", err)
	}

	// Build a report with just an October column.
	months := []Dates{
		{Date: time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC)},
	}
	report, err := BuildDocument(test.Context(), conn, company.ID, months)
	if err != nil {
		test.Fatalf("build report: %v", err)
	}

	// Only Rent should appear; Cash is an asset.
	if len(report.Rows) != 1 {
		test.Fatalf("got %d report rows, want 1", len(report.Rows))
	}

	rentRow := report.Rows[0]
	if rentRow.Account.ID != rent.ID {
		test.Fatalf("got account %q, want Rent", rentRow.Account.Name)
	}

	// There should be one monthly amount: -$300.
	if len(rentRow.Data) != 1 {
		test.Fatalf("got %d monthly amounts, want 1", len(rentRow.Data))
	}
	if rentRow.Data[0] != -30_000 {
		test.Errorf("October rent = %d cents, want -30000", rentRow.Data[0])
	}

	test.Logf("October %s: %d cents", rentRow.Account.Name, rentRow.Data[0])
}

// TestBuildDocumentMonthlyAmounts posts sales and rent across two months, then
// checks their signs, month placement, and the zero November Sales amount.
func TestBuildDocumentMonthlyAmounts(t *testing.T) {
	conn := newLedgerTestDB(t)
	queries := db.New(conn)
	org, err := queries.CreateOrganization(t.Context(), "First organization")
	if err != nil {
		t.Fatalf("create first organization: %v", err)
	}
	cash := createTestAccount(t, queries, org.ID, "Cash", "asset")
	sales := createTestAccount(t, queries, org.ID, "Sales", "revenue")
	rent := createTestAccount(t, queries, org.ID, "Rent", "expense")

	// Sales credit revenue; rent debits expense and credits Cash.
	postTestTransaction(t, conn, org.ID, "2026-10-06", "October sale", cash, sales, 100_000)
	postTestTransaction(t, conn, org.ID, "2026-10-15", "October rent", rent, cash, 30_000)
	postTestTransaction(t, conn, org.ID, "2026-11-15", "November rent", rent, cash, 20_000)

	dates := []Dates{
		{Date: time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC)},
		{Date: time.Date(2026, time.November, 30, 0, 0, 0, 0, time.UTC)},
	}
	doc, err := BuildDocument(t.Context(), conn, org.ID, dates)
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	// Cash is an asset, so the exact expected rows also check that it is excluded.
	// Each slice follows the requested column order: October, then November.
	assertDocumentRows(t, doc, org.ID, map[int64][]int64{
		sales.ID: {100_000, 0},
		rent.ID:  {-30_000, -20_000},
	})
}

// createTestAccount stores one account under orgID and returns its database
// record so tests can use the assigned ID in entries and expected rows.
func createTestAccount(t *testing.T, queries *db.Queries, orgID int64, name, accountType string) db.Account {
	t.Helper()
	account, err := queries.CreateAccount(t.Context(), db.CreateAccountParams{
		OrganizationID: orgID,
		Name:           name,
		Type:           accountType,
	})
	if err != nil {
		t.Fatalf("create %s account for organization %d: %v", name, orgID, err)
	}
	return account
}

// postTestTransaction uses Post to save one balanced debit and credit. The
// amount is in cents, and both entries use the same amount.
func postTestTransaction(t *testing.T, conn *sql.DB, orgID int64, date, description string, debit, credit db.Account, amount int64) {
	t.Helper()
	_, err := Post(t.Context(), conn, db.CreateTransactionParams{
		OrganizationID: orgID,
		Date:           date,
		Description:    description,
	}, []db.CreateEntryParams{
		{AccountID: debit.ID, Direction: "debit", Amount: amount},
		{AccountID: credit.ID, Direction: "credit", Amount: amount},
	})
	if err != nil {
		t.Fatalf("post %s: %v", description, err)
	}
}

// assertDocumentRows compares the exact account set by ID, independent of row
// order. It also checks each month's cents and reports extra, missing, or
// duplicate rows.
func assertDocumentRows(t *testing.T, doc LedgerDocument, orgID int64, want map[int64][]int64) {
	t.Helper()
	if len(doc.Rows) != len(want) {
		t.Errorf("organization %d: got %d report rows, want %d", orgID, len(doc.Rows), len(want))
	}
	// Track returned IDs so an account cannot appear twice without failing.
	seen := make(map[int64]bool, len(doc.Rows))
	for _, row := range doc.Rows {
		id := row.Account.ID
		if seen[id] {
			t.Errorf("organization %d: duplicate row for account %d (%s)", orgID, id, row.Account.Name)
			continue
		}
		seen[id] = true
		amounts, ok := want[id]
		if !ok {
			t.Errorf("organization %d: unexpected account %d (%s)", orgID, id, row.Account.Name)
			continue
		}
		if row.Account.OrganizationID != orgID {
			t.Errorf("account %d (%s) belongs to organization %d, want %d", id, row.Account.Name, row.Account.OrganizationID, orgID)
		}
		if len(row.Data) != len(amounts) {
			t.Errorf("account %d (%s) has %d months, want %d", id, row.Account.Name, len(row.Data), len(amounts))
			continue
		}
		if !slices.Equal(row.Data, amounts) {
			t.Errorf("account %d (%s) monthly cents = %v, want %v", id, row.Account.Name, row.Data, amounts)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("organization %d: missing row for account %d", orgID, id)
		}
	}
}
