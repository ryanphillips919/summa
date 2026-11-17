package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"summa/internal/db"
	"summa/internal/invoicing"
)

func TestDraftInvoiceEditing(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	invoices, err := app.queries.ListInvoices(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	var draft, posted db.Invoice
	for _, invoice := range invoices {
		if invoice.PostedTransactionID.Valid {
			posted = invoice
		} else {
			draft = invoice
		}
	}
	if draft.ID == 0 || posted.ID == 0 {
		t.Fatal("fixture needs draft and posted invoices")
	}
	lines, err := app.queries.ListInvoiceLines(ctx, db.ListInvoiceLinesParams{OrganizationID: app.orgID, InvoiceID: draft.ID})
	if err != nil || len(lines) < 2 {
		t.Fatalf("draft lines: %v, %v", lines, err)
	}
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, req)
		return rec
	}
	base := fmt.Sprintf("/invoices/%d", draft.ID)
	edit := url.Values{"customer_id": {fmt.Sprint(draft.CustomerID)}, "issue_date": {"2026-11-01"}, "due_date": {"2026-11-30"}}
	if response := post(base, edit); response.Code != http.StatusSeeOther {
		t.Fatalf("edit details: %d %s", response.Code, response.Body.String())
	}
	updated, err := app.queries.GetInvoice(ctx, db.GetInvoiceParams{OrganizationID: app.orgID, InvoiceID: draft.ID})
	if err != nil || updated.IssueDate != "2026-11-01" || updated.DueDate != "2026-11-30" {
		t.Fatalf("updated invoice: %+v, %v", updated, err)
	}
	linePath := fmt.Sprintf("%s/lines/%d", base, lines[0].ID)
	lineForm := url.Values{"description": {"Edited"}, "quantity": {"3"}, "unit_price": {"12.50"}}
	if response := post(linePath, lineForm); response.Code != http.StatusSeeOther {
		t.Fatalf("edit line: %d %s", response.Code, response.Body.String())
	}
	lines, err = app.queries.ListInvoiceLines(ctx, db.ListInvoiceLinesParams{OrganizationID: app.orgID, InvoiceID: draft.ID})
	if err != nil || lines[0].Description != "Edited" || lines[0].Quantity != 3 || lines[0].UnitPriceCents != 1250 {
		t.Fatalf("updated lines: %+v, %v", lines, err)
	}
	if response := post(fmt.Sprintf("%s/lines/%d/delete", base, lines[1].ID), nil); response.Code != http.StatusSeeOther {
		t.Fatalf("delete line: %d %s", response.Code, response.Body.String())
	}
	if response := post(fmt.Sprintf("/invoices/%d", posted.ID), edit); response.Code != http.StatusConflict {
		t.Fatalf("edited posted invoice: %d", response.Code)
	}
	page := httptest.NewRecorder()
	newRouter(app).ServeHTTP(page, httptest.NewRequest(http.MethodGet, base, nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Edit line items") {
		t.Fatalf("draft page: %d %s", page.Code, page.Body.String())
	}
}

func TestConcurrentInvoicePaymentsDoNotOverallocate(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	invoices, err := app.queries.ListInvoices(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	var invoice db.Invoice
	for _, candidate := range invoices {
		if candidate.PostedTransactionID.Valid {
			invoice = candidate
			break
		}
	}
	accounts, err := app.queries.ListAccounts(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	var cashID int64
	for _, account := range accounts {
		if account.Name == "Cash" {
			cashID = account.ID
		}
	}
	total, err := app.queries.InvoiceTotal(ctx, db.InvoiceTotalParams{OrganizationID: app.orgID, InvoiceID: invoice.ID})
	if err != nil || total <= 0 || cashID == 0 {
		t.Fatalf("invalid fixture: %d, %d, %v", total, cashID, err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := invoicing.RecordReceipt(ctx, app.sqlDB, app.orgID, invoice.ID, cashID, total, invoice.IssueDate, fmt.Sprintf("concurrent-payment-%d", i))
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one successful payment, got %d", succeeded)
	}
	var count int
	if err := app.sqlDB.QueryRowContext(ctx, "SELECT count(*) FROM invoice_receipts WHERE invoice_id = ?", invoice.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipts = %d, %v", count, err)
	}
}

func TestRecordInvoicePaymentHTTP(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	invoices, err := app.queries.ListInvoices(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	var invoice db.Invoice
	for _, candidate := range invoices {
		if candidate.PostedTransactionID.Valid {
			invoice = candidate
			break
		}
	}
	accounts, err := app.queries.ListAccounts(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	var cashID int64
	for _, account := range accounts {
		if account.Name == "Cash" {
			cashID = account.ID
		}
	}
	if invoice.ID == 0 || cashID == 0 {
		t.Fatal("missing fixture invoice or cash")
	}
	path := fmt.Sprintf("/invoices/%d/payments", invoice.ID)
	form := url.Values{"date": {invoice.IssueDate}, "deposit_account_id": {fmt.Sprint(cashID)}, "amount": {"1.25"}, "request_key": {"payment-http-request-01"}}
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("payment %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	var count int
	if err := app.sqlDB.QueryRowContext(ctx, "SELECT count(*) FROM invoice_receipts WHERE invoice_id = ?", invoice.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipts = %d, %v", count, err)
	}
	page := httptest.NewRecorder()
	newRouter(app).ServeHTTP(page, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/invoices/%d", invoice.ID), nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "$1.25") {
		t.Fatalf("payment page: %d %s", page.Code, page.Body.String())
	}
}
