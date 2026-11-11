package invoicing

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"summa/internal/db"
)

type invoiceFixture struct {
	conn       *sql.DB
	queries    *db.Queries
	org        db.Organization
	invoice    db.Invoice
	receivable db.Account
	revenue    db.Account
}

func newInvoiceFixture(t *testing.T) invoiceFixture {
	t.Helper()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(t.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set migration dialect: %v", err)
	}
	if err := goose.Up(conn, "../db/migrations"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	q := db.New(conn)
	org, err := q.CreateOrganization(t.Context(), "First organization")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	customer, err := q.CreateCustomer(t.Context(), db.CreateCustomerParams{
		OrganizationID: org.ID,
		Name:           "Alice",
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	invoice, err := q.CreateInvoice(t.Context(), db.CreateInvoiceParams{
		OrganizationID: org.ID,
		CustomerID:     customer.ID,
		Number:         "INV-001",
		IssueDate:      "2026-11-06",
		DueDate:        "2026-12-06",
	})
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	receivable, err := q.CreateAccount(t.Context(), db.CreateAccountParams{
		OrganizationID: org.ID,
		Name:           "Accounts Receivable",
		Type:           "asset",
	})
	if err != nil {
		t.Fatalf("create receivable account: %v", err)
	}
	revenue, err := q.CreateAccount(t.Context(), db.CreateAccountParams{
		OrganizationID: org.ID,
		Name:           "Sales Revenue",
		Type:           "revenue",
	})
	if err != nil {
		t.Fatalf("create revenue account: %v", err)
	}
	return invoiceFixture{
		conn:       conn,
		queries:    q,
		org:        org,
		invoice:    invoice,
		receivable: receivable,
		revenue:    revenue,
	}
}

func (f invoiceFixture) addLine(t *testing.T) {
	t.Helper()
	_, err := f.queries.CreateInvoiceLine(t.Context(), db.CreateInvoiceLineParams{
		OrganizationID: f.org.ID,
		InvoiceID:      f.invoice.ID,
		Description:    "Consulting",
		Quantity:       2,
		UnitPriceCents: 1250,
	})
	if err != nil {
		t.Fatalf("create invoice line: %v", err)
	}
}

func (f invoiceFixture) post(t *testing.T) (db.Transaction, error) {
	t.Helper()
	return PostInvoice(t.Context(), f.conn, f.org.ID, f.invoice.ID, f.receivable.ID, f.revenue.ID)
}

func TestPostInvoice(t *testing.T) {
	f := newInvoiceFixture(t)
	f.addLine(t)

	posted, err := f.post(t)
	if err != nil {
		t.Fatalf("post invoice: %v", err)
	}
	if posted.OrganizationID != f.org.ID || posted.Date != f.invoice.IssueDate || posted.Description != "Invoice INV-001" {
		t.Errorf("posted transaction = %+v, want invoice organization, date and number", posted)
	}

	invoice, err := f.queries.GetInvoice(t.Context(), db.GetInvoiceParams{
		OrganizationID: f.org.ID,
		InvoiceID:      f.invoice.ID,
	})
	if err != nil {
		t.Fatalf("get posted invoice: %v", err)
	}
	if !invoice.PostedTransactionID.Valid || invoice.PostedTransactionID.Int64 != posted.ID {
		t.Errorf("posted transaction ID = %+v, want %d", invoice.PostedTransactionID, posted.ID)
	}
	entries, err := f.queries.ListEntries(t.Context(), posted.ID)
	if err != nil {
		t.Fatalf("list ledger entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d ledger entries, want 2", len(entries))
	}
	if entries[0].AccountID != f.receivable.ID || entries[0].Direction != "debit" || entries[0].Amount != 2500 ||
		entries[1].AccountID != f.revenue.ID || entries[1].Direction != "credit" || entries[1].Amount != 2500 {
		t.Errorf("ledger entries = %+v, want balanced receivable and revenue", entries)
	}

	_, err = f.post(t)
	if !errors.Is(err, ErrAlreadyPosted) {
		t.Errorf("duplicate post error = %v, want ErrAlreadyPosted", err)
	}
	transactions, err := f.queries.ListTransactions(t.Context(), f.org.ID)
	if err != nil || len(transactions) != 1 {
		t.Errorf("transactions after duplicate post = %+v, error = %v; want one", transactions, err)
	}
	_, err = f.queries.CreateInvoiceLine(t.Context(), db.CreateInvoiceLineParams{
		OrganizationID: f.org.ID,
		InvoiceID:      f.invoice.ID,
		Description:    "Late line",
		Quantity:       1,
		UnitPriceCents: 100,
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("add line after posting error = %v, want sql.ErrNoRows", err)
	}
}

func TestPostInvoiceRejectsOtherOrganization(t *testing.T) {
	f := newInvoiceFixture(t)
	f.addLine(t)
	otherOrg, err := f.queries.CreateOrganization(t.Context(), "Second organization")
	if err != nil {
		t.Fatalf("create second organization: %v", err)
	}
	otherAccount, err := f.queries.CreateAccount(t.Context(), db.CreateAccountParams{
		OrganizationID: otherOrg.ID,
		Name:           "Other receivable",
		Type:           "asset",
	})
	if err != nil {
		t.Fatalf("create second organization's account: %v", err)
	}

	_, err = PostInvoice(t.Context(), f.conn, otherOrg.ID, f.invoice.ID, otherAccount.ID, f.revenue.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("post another organization's invoice error = %v, want sql.ErrNoRows", err)
	}
	_, err = PostInvoice(t.Context(), f.conn, f.org.ID, f.invoice.ID, otherAccount.ID, f.revenue.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("post with another organization's account error = %v, want sql.ErrNoRows", err)
	}
	transactions, err := f.queries.ListTransactions(t.Context(), f.org.ID)
	if err != nil || len(transactions) != 0 {
		t.Errorf("transactions after rejected posts = %+v, error = %v; want none", transactions, err)
	}
}

func TestPostInvoiceRequiresPositiveTotalAndCorrectAccountTypes(t *testing.T) {
	f := newInvoiceFixture(t)
	_, err := f.post(t)
	if err == nil {
		t.Error("posted an empty invoice")
	}
	f.addLine(t)
	_, err = PostInvoice(t.Context(), f.conn, f.org.ID, f.invoice.ID, f.revenue.ID, f.receivable.ID)
	if err == nil {
		t.Error("posted with the wrong account types")
	}
	transactions, err := f.queries.ListTransactions(t.Context(), f.org.ID)
	if err != nil || len(transactions) != 0 {
		t.Errorf("transactions after rejected posts = %+v, error = %v; want none", transactions, err)
	}
}

func TestPostInvoiceRollsBackWhenInvoiceLinkFails(t *testing.T) {
	f := newInvoiceFixture(t)
	f.addLine(t)
	_, err := f.conn.ExecContext(t.Context(), `
		CREATE TRIGGER reject_invoice_link BEFORE UPDATE OF posted_transaction_id ON invoices
		BEGIN SELECT RAISE(ABORT, 'invoice link failed'); END;
	`)
	if err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	_, err = f.post(t)
	if err == nil {
		t.Fatal("posted invoice despite rejected link")
	}
	transactions, err := f.queries.ListTransactions(t.Context(), f.org.ID)
	if err != nil || len(transactions) != 0 {
		t.Errorf("transactions after failed link = %+v, error = %v; want none", transactions, err)
	}
	invoice, err := f.queries.GetInvoice(t.Context(), db.GetInvoiceParams{
		OrganizationID: f.org.ID,
		InvoiceID:      f.invoice.ID,
	})
	if err != nil {
		t.Fatalf("get invoice after failed link: %v", err)
	}
	if invoice.PostedTransactionID.Valid {
		t.Errorf("invoice linked to transaction %d after rollback", invoice.PostedTransactionID.Int64)
	}
}
