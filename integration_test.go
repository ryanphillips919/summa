package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"summa/internal/db"
	"summa/internal/ledger"
)

type streamRecorder struct {
	header  http.Header
	mu      sync.Mutex
	body    bytes.Buffer
	updates chan string
}

func (s *streamRecorder) Header() http.Header { return s.header }
func (s *streamRecorder) WriteHeader(int)     {}
func (s *streamRecorder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.Write(p)
}
func (s *streamRecorder) Flush() {
	s.mu.Lock()
	value := s.body.String()
	s.mu.Unlock()
	select {
	case s.updates <- value:
	default:
	}
}

func newNorthstarTestApp(t *testing.T) *App {
	t.Helper()
	conn, err := openDatabase(t.Context(), filepath.Join(t.TempDir(), "northstar.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	queries := db.New(conn)
	orgID, err := seedDevData(t.Context(), conn, queries)
	if err != nil {
		t.Fatalf("seed Northstar: %v", err)
	}
	source, err := newAnalyticsDataSource(conn, orgID)
	if err != nil {
		t.Fatalf("create analytics source: %v", err)
	}
	return &App{sqlDB: conn, queries: queries, orgID: orgID, cashDataSource: source}
}

func TestApplicationStartsWithNorthstarDatabase(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SUMMA_DB", "")
	for start := 0; start < 2; start++ {
		func() {
			app, cleanup, err := appInit()
			if err != nil {
				t.Fatalf("start %d: %v", start+1, err)
			}
			defer cleanup()
			transactions, err := app.queries.ListTransactions(t.Context(), app.orgID)
			if err != nil || len(transactions) != 99 {
				t.Fatalf("start %d: transactions = %d, error = %v", start+1, len(transactions), err)
			}
			response := httptest.NewRecorder()
			newRouter(app).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Northstar Equipment &amp; Supply") {
				t.Fatalf("start %d: dashboard status = %d", start+1, response.Code)
			}
		}()
	}
	if _, err := os.Stat(filepath.Join("data", "summa.db")); err != nil {
		t.Fatalf("default development database was not created: %v", err)
	}
}

func TestNorthstarSeed(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	org, err := app.queries.GetOrganization(ctx, app.orgID)
	if err != nil || org.Name != "Northstar Equipment & Supply" {
		t.Fatalf("organization = %+v, error = %v", org, err)
	}
	customers, err := app.queries.ListCustomers(ctx, app.orgID)
	if err != nil || len(customers) != 12 {
		t.Fatalf("customers = %d, error = %v; want 12", len(customers), err)
	}
	customerIDs := make(map[int64]bool, len(customers))
	for _, customer := range customers {
		if customer.OrganizationID != app.orgID {
			t.Errorf("customer belongs to organization %d", customer.OrganizationID)
		}
		customerIDs[customer.ID] = true
	}
	accounts, err := app.queries.ListAccounts(ctx, app.orgID)
	if err != nil || len(accounts) != 16 {
		t.Fatalf("accounts = %d, error = %v; want 16", len(accounts), err)
	}
	accountIDs := make(map[int64]string)
	for _, account := range accounts {
		if account.OrganizationID != app.orgID {
			t.Errorf("account belongs to organization %d", account.OrganizationID)
		}
		accountIDs[account.ID] = account.Name
	}
	transactions, err := app.queries.ListTransactions(ctx, app.orgID)
	if err != nil || len(transactions) != 99 {
		t.Fatalf("transactions = %d, error = %v; want 99", len(transactions), err)
	}
	for _, transaction := range transactions {
		entries, err := app.queries.ListEntries(ctx, transaction.ID)
		if err != nil || len(entries) < 2 {
			t.Fatalf("transaction %d entries = %d, error = %v", transaction.ID, len(entries), err)
		}
		var debit, credit int64
		for _, entry := range entries {
			if accountIDs[entry.AccountID] == "" {
				t.Errorf("transaction %d uses another organization's account %d", transaction.ID, entry.AccountID)
			}
			if entry.Direction == "debit" {
				debit += entry.Amount
			} else {
				credit += entry.Amount
			}
		}
		if debit != credit {
			t.Errorf("transaction %d is unbalanced: %d vs %d", transaction.ID, debit, credit)
		}
	}

	invoices, err := app.queries.ListInvoices(ctx, app.orgID)
	if err != nil || len(invoices) != 20 {
		t.Fatalf("invoices = %d, error = %v; want 20", len(invoices), err)
	}
	var posted, drafts int
	for _, invoice := range invoices {
		if invoice.OrganizationID != app.orgID || !customerIDs[invoice.CustomerID] {
			t.Errorf("invoice %s has an incorrect organization or customer", invoice.Number)
		}
		lines, err := app.queries.ListInvoiceLines(ctx, db.ListInvoiceLinesParams{OrganizationID: app.orgID, InvoiceID: invoice.ID})
		if err != nil || len(lines) != 2 {
			t.Fatalf("invoice %s lines = %d, error = %v", invoice.Number, len(lines), err)
		}
		total, err := app.queries.InvoiceTotal(ctx, db.InvoiceTotalParams{OrganizationID: app.orgID, InvoiceID: invoice.ID})
		if err != nil || total != lines[0].Quantity*lines[0].UnitPriceCents+lines[1].Quantity*lines[1].UnitPriceCents {
			t.Fatalf("invoice %s total = %d, error = %v", invoice.Number, total, err)
		}
		if invoice.Number == "INV-001" && total != 980_000 {
			t.Errorf("Summit invoice total = %d, want 980000", total)
		}
		if invoice.Number == "INV-002" && total != 1_200_000 {
			t.Errorf("Canyon invoice total = %d, want 1200000", total)
		}
		if invoice.PostedTransactionID.Valid {
			posted++
			transaction, err := app.queries.GetTransaction(ctx, db.GetTransactionParams{OrganizationID: app.orgID, TransactionID: invoice.PostedTransactionID.Int64})
			if err != nil || transaction.OrganizationID != app.orgID {
				t.Errorf("invoice %s posting = %+v, error = %v", invoice.Number, transaction, err)
			}
			entries, err := app.queries.ListEntries(ctx, transaction.ID)
			if err != nil || len(entries) != 2 {
				t.Fatalf("invoice %s entries = %+v, error = %v", invoice.Number, entries, err)
			}
			if accountIDs[entries[0].AccountID] != "Accounts Receivable" || entries[0].Direction != "debit" || entries[0].Amount != total ||
				accountIDs[entries[1].AccountID] != "Sales Revenue" || entries[1].Direction != "credit" || entries[1].Amount != total {
				t.Errorf("invoice %s entries = %+v, want receivable and revenue for %d", invoice.Number, entries, total)
			}
		} else {
			drafts++
		}
	}
	if posted != 18 || drafts != 2 {
		t.Errorf("posted = %d, drafts = %d; want 18 and 2", posted, drafts)
	}

	totals, err := app.queries.AccountTotals(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	balances := make(map[string]int64)
	for _, total := range totals {
		balances[total.Name] = ledger.Balance(total.Type, total.Debits, total.Credits)
	}
	for name, want := range map[string]int64{
		"Cash": 11_155_002, "Accounts Receivable": 5_475_000,
		"Inventory": 3_600_000, "Accounts Payable": 3_600_000,
		"Loan Payable": 7_100_000, "Sales Revenue": 16_475_000,
	} {
		if balances[name] != want {
			t.Errorf("%s balance = %d, want %d cents", name, balances[name], want)
		}
	}
	secondID, err := seedDevData(ctx, app.sqlDB, app.queries)
	if err != nil || secondID != app.orgID {
		t.Errorf("second seed organization = %d, error = %v", secondID, err)
	}
	transactions, err = app.queries.ListTransactions(ctx, app.orgID)
	if err != nil || len(transactions) != 99 {
		t.Errorf("transactions after second seed = %d, error = %v; want 99", len(transactions), err)
	}
}

func TestInvoiceWorkflowUpdatesSharedViews(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	router := newRouter(app)
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	for _, path := range []string{"/", "/customers", "/invoices", "/invoices/new", "/journal", "/demo/", "/timeline/", "/analytics/"} {
		response := request(http.MethodGet, path, nil)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Northstar Equipment &amp; Supply") {
			t.Errorf("GET %s status = %d, missing organization = %v", path, response.Code, !strings.Contains(response.Body.String(), "Northstar Equipment &amp; Supply"))
		}
	}

	customers, err := app.queries.ListCustomers(ctx, app.orgID)
	if err != nil || len(customers) == 0 {
		t.Fatalf("customers: %v", err)
	}
	created := request(http.MethodPost, "/invoices", url.Values{
		"customer_id": {strconv.FormatInt(customers[0].ID, 10)}, "number": {"INV-021"},
		"issue_date": {"2026-10-27"}, "due_date": {"2026-11-26"},
	})
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create invoice: status %d, body %s", created.Code, created.Body.String())
	}
	path := created.Header().Get("Location")
	if path == "" {
		t.Fatal("create invoice returned no location")
	}
	for _, line := range []url.Values{
		{"description": {"Equipment"}, "quantity": {"2"}, "unit_price": {"150.00"}},
		{"description": {"Parts"}, "quantity": {"1"}, "unit_price": {"25.50"}},
	} {
		response := request(http.MethodPost, path+"/lines", line)
		if response.Code != http.StatusSeeOther {
			t.Fatalf("add invoice line: status %d, body %s", response.Code, response.Body.String())
		}
	}
	beforePage := request(http.MethodGet, path, nil)
	if beforePage.Code != http.StatusOK || !strings.Contains(beforePage.Body.String(), "$325.50") || !strings.Contains(beforePage.Body.String(), "Draft") {
		t.Fatalf("draft page: status %d, body %s", beforePage.Code, beforePage.Body.String())
	}
	asOf := time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC)
	before, err := app.loadDashboard(ctx, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if before.Cash != 11_155_002 || before.Receivables != 5_475_000 || before.Payables != 3_600_000 || before.Revenue != 16_475_000 || before.Expenses != 27_944_998 || before.NetIncome != -11_469_998 {
		t.Fatalf("dashboard balances = %+v", before)
	}
	if len(before.Trends) != 6 || before.Trends[5].Month != "Oct 2026" || before.Trends[5].Revenue != 2_850_000 || before.Trends[5].Expenses != 6_407_498 || before.Trends[5].Net != -3_557_498 {
		t.Fatalf("October trend = %+v", before.Trends)
	}
	cashBefore, err := app.cashDataSource.LoadCashSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	transactionsBefore, err := app.queries.ListTransactions(ctx, app.orgID)
	if err != nil || len(transactionsBefore) != 99 {
		t.Fatalf("draft affected ledger: transactions = %d, error = %v", len(transactionsBefore), err)
	}
	accounts, err := app.queries.ListAccounts(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, account := range accounts {
		ids[account.Name] = strconv.FormatInt(account.ID, 10)
	}
	postForm := url.Values{"receivable_account_id": {ids["Accounts Receivable"]}, "revenue_account_id": {ids["Sales Revenue"]}}
	posted := request(http.MethodPost, path+"/post", postForm)
	if posted.Code != http.StatusSeeOther {
		t.Fatalf("post invoice: status %d, body %s", posted.Code, posted.Body.String())
	}
	after, err := app.loadDashboard(ctx, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if after.Cash != before.Cash || after.Receivables != before.Receivables+32_550 || after.Revenue != before.Revenue+32_550 {
		t.Errorf("posting changed balances: before %+v, after %+v", before, after)
	}
	if after.Trends[5].Revenue != before.Trends[5].Revenue+32_550 || after.Trends[5].Net != before.Trends[5].Net+32_550 {
		t.Errorf("October report did not update: before %+v, after %+v", before.Trends[5], after.Trends[5])
	}
	cashAfter, err := app.cashDataSource.LoadCashSnapshot(ctx)
	if err != nil || !reflect.DeepEqual(cashBefore, cashAfter) {
		t.Errorf("invoice posting changed cash analytics: error %v", err)
	}
	for _, check := range []struct{ path, text string }{
		{path, "Ledger transaction"}, {"/journal", "Invoice INV-021"}, {"/timeline/", "Invoice INV-021"}, {"/demo/", formatDollars(after.Trends[5].Revenue)},
	} {
		response := request(http.MethodGet, check.path, nil)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), check.text) {
			t.Errorf("GET %s status = %d; missing %q", check.path, response.Code, check.text)
		}
	}
	if response := request(http.MethodPost, path+"/post", postForm); response.Code != http.StatusConflict {
		t.Errorf("duplicate post status = %d, want 409", response.Code)
	}
	if response := request(http.MethodPost, path+"/lines", url.Values{"description": {"Late"}, "quantity": {"1"}, "unit_price": {"1.00"}}); response.Code != http.StatusConflict {
		t.Errorf("posted invoice line status = %d, want 409", response.Code)
	}
}

func TestNorthstarCashAnalytics(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	snapshot, err := app.cashDataSource.LoadCashSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	balance := int64(math.Round(snapshot.OpeningCashBalance * 100))
	var duplicates, repairs int
	for _, movement := range snapshot.Movements {
		balance += int64(math.Round(movement.Amount * 100))
		if strings.HasPrefix(movement.Description, "Invoice INV-") {
			t.Errorf("invoice posting appeared as cash movement: %+v", movement)
		}
		if movement.Description == "Software renewal" && movement.Amount == -1249.99 {
			if movement.Category != "Subscriptions Expense" || movement.Vendor != "" {
				t.Errorf("cash movement category/vendor = %q/%q", movement.Category, movement.Vendor)
			}
			duplicates++
		}
		if movement.Description == "Major warehouse equipment repair" && movement.Amount == -18500 {
			repairs++
		}
	}
	if balance != 11_155_002 || duplicates != 2 || repairs != 1 {
		t.Errorf("cash analytics balance = %d, duplicate candidates = %d, repairs = %d", balance, duplicates, repairs)
	}
	accounts, err := app.queries.ListAccounts(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]int64)
	for _, account := range accounts {
		ids[account.Name] = account.ID
	}
	_, err = ledger.Post(ctx, app.sqlDB, db.CreateTransactionParams{OrganizationID: app.orgID, Date: "2026-10-30", Description: "Split cash receipt"}, []db.CreateEntryParams{
		{AccountID: ids["Cash"], Direction: "debit", Amount: 10_000},
		{AccountID: ids["Sales Revenue"], Direction: "credit", Amount: 6_000},
		{AccountID: ids["Owner Equity"], Direction: "credit", Amount: 4_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = app.cashDataSource.LoadCashSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, movement := range snapshot.Movements {
		if movement.Description == "Split cash receipt" {
			found = true
			if movement.Amount != 100 {
				t.Errorf("split cash receipt = %v, want 100", movement.Amount)
			}
		}
	}
	if !found {
		t.Error("split cash receipt missing from analytics")
	}
}

func TestNorthstarAnalyticsEndpoint(t *testing.T) {
	app := newNorthstarTestApp(t)
	response := httptest.NewRecorder()
	newRouter(app).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/analytics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("analytics status = %d, body = %s", response.Code, response.Body.String())
	}
	var data analyticsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	var duplicates, repairs int
	for _, anomaly := range data.Anomalies {
		if anomaly.Type == "potential_duplicate" && anomaly.Description == "Software renewal" {
			duplicates++
		}
		if anomaly.Type == "amount_outlier" && anomaly.Description == "Major warehouse equipment repair" {
			repairs++
		}
	}
	if duplicates != 1 || repairs != 1 {
		t.Errorf("analytics duplicates = %d, repair outliers = %d, anomalies = %+v", duplicates, repairs, data.Anomalies)
	}
}

func TestJournalNotifiesAfterPosting(t *testing.T) {
	app := newNorthstarTestApp(t)
	server, nc, err := startEmbeddedNATS(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownEmbeddedNATS(nc, server) })
	app.nc = nc
	subscription, err := nc.SubscribeSync(app.ledgerSubject())
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Unsubscribe()
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	accounts, err := app.queries.ListAccounts(t.Context(), app.orgID)
	if err != nil || len(accounts) < 2 {
		t.Fatalf("accounts = %d, error = %v", len(accounts), err)
	}
	form := url.Values{"date": {"2026-10-31"}, "description": {"Manual adjustment test"}}
	form.Add("account_id", strconv.FormatInt(accounts[0].ID, 10))
	form.Add("account_id", strconv.FormatInt(accounts[1].ID, 10))
	form.Add("debit", "10.00")
	form.Add("debit", "")
	form.Add("credit", "")
	form.Add("credit", "10.00")
	req := httptest.NewRequest(http.MethodPost, "/journal", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	newRouter(app).ServeHTTP(response, req)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("post journal status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := subscription.NextMsg(2 * time.Second); err != nil {
		t.Fatalf("ledger notification: %v", err)
	}
	transactions, err := app.queries.ListTransactions(t.Context(), app.orgID)
	if err != nil || len(transactions) == 0 || transactions[0].Description != "Manual adjustment test" {
		t.Fatalf("notification arrived without committed entry: %+v, error = %v", transactions, err)
	}
}

func TestMonthlyLedgerReloadsAfterPosting(t *testing.T) {
	app := newNorthstarTestApp(t)
	server, nc, err := startEmbeddedNATS(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownEmbeddedNATS(nc, server) })
	app.nc = nc
	ctx, cancel := context.WithCancel(t.Context())
	writer := &streamRecorder{header: make(http.Header), updates: make(chan string, 16)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.demoWatcherHandler(writer, httptest.NewRequest(http.MethodGet, "/demo/watch", nil).WithContext(ctx))
	}()
	defer func() { cancel(); <-done }()
	waitFor := func(want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case body := <-writer.updates:
				if strings.Contains(body, want) {
					return
				}
			case <-deadline:
				t.Fatalf("live report did not contain %q", want)
			}
		}
	}
	waitFor("Monthly revenue and expenses")
	invoices, err := app.queries.ListInvoices(t.Context(), app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	var draft db.Invoice
	for _, invoice := range invoices {
		if invoice.Number == "INV-019" {
			draft = invoice
		}
	}
	if draft.ID == 0 {
		t.Fatal("draft invoice not found")
	}
	accounts, err := app.queries.ListAccounts(t.Context(), app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, account := range accounts {
		ids[account.Name] = strconv.FormatInt(account.ID, 10)
	}
	form := url.Values{"receivable_account_id": {ids["Accounts Receivable"]}, "revenue_account_id": {ids["Sales Revenue"]}}
	req := httptest.NewRequest(http.MethodPost, "/invoices/"+strconv.FormatInt(draft.ID, 10)+"/post", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	newRouter(app).ServeHTTP(response, req)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("post draft: status %d, body %s", response.Code, response.Body.String())
	}
	// October revenue rises from $28,500 to $35,000 after posting INV-019.
	waitFor("$35,000.00")
}

func TestInvoicePageIsOrganizationScoped(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	other, err := app.queries.CreateOrganization(ctx, "Other organization")
	if err != nil {
		t.Fatal(err)
	}
	customer, err := app.queries.CreateCustomer(ctx, db.CreateCustomerParams{OrganizationID: other.ID, Name: "Other customer"})
	if err != nil {
		t.Fatal(err)
	}
	invoice, err := app.queries.CreateInvoice(ctx, db.CreateInvoiceParams{
		OrganizationID: other.ID, CustomerID: customer.ID, Number: "OTHER-1", IssueDate: "2026-10-01", DueDate: "2026-11-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	newRouter(app).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/invoices/"+strconv.FormatInt(invoice.ID, 10), nil))
	if response.Code != http.StatusNotFound {
		t.Errorf("other organization's invoice status = %d, want 404", response.Code)
	}
}

func TestJournalRejectsUnbalancedAndOtherOrganizationAccounts(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	accounts, err := app.queries.ListAccounts(ctx, app.orgID)
	if err != nil || len(accounts) < 2 {
		t.Fatalf("accounts = %d, error = %v", len(accounts), err)
	}
	other, err := app.queries.CreateOrganization(ctx, "Other organization")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := app.queries.CreateAccount(ctx, db.CreateAccountParams{OrganizationID: other.ID, Name: "Other Cash", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, secondAccount, secondCredit string
	}{
		{"unbalanced", strconv.FormatInt(accounts[1].ID, 10), "9.00"},
		{"other organization", strconv.FormatInt(foreign.ID, 10), "10.00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			form := url.Values{"date": {"2026-10-31"}, "description": {"Invalid adjustment"}}
			form.Add("account_id", strconv.FormatInt(accounts[0].ID, 10))
			form.Add("account_id", test.secondAccount)
			form.Add("debit", "10.00")
			form.Add("debit", "")
			form.Add("credit", "")
			form.Add("credit", test.secondCredit)
			req := httptest.NewRequest(http.MethodPost, "/journal", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			newRouter(app).ServeHTTP(response, req)
			if response.Code != http.StatusUnprocessableEntity {
				t.Errorf("invalid journal status = %d, want 422", response.Code)
			}
		})
	}
	transactions, err := app.queries.ListTransactions(ctx, app.orgID)
	if err != nil || len(transactions) != 99 {
		t.Errorf("transactions after rejected posts = %d, error = %v", len(transactions), err)
	}
}

func TestEmptyOrganizationViews(t *testing.T) {
	conn, err := openDatabase(t.Context(), filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	queries := db.New(conn)
	org, err := queries.CreateOrganization(t.Context(), "Empty organization")
	if err != nil {
		t.Fatal(err)
	}
	source, err := newAnalyticsDataSource(conn, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{sqlDB: conn, queries: queries, orgID: org.ID, cashDataSource: source}
	router := newRouter(app)
	for _, path := range []string{"/", "/customers", "/invoices", "/invoices/new", "/journal", "/demo/", "/timeline/", "/analytics/"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("empty organization GET %s = %d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/analytics", nil))
	if response.Code != http.StatusNoContent {
		t.Errorf("empty analytics status = %d, want 204", response.Code)
	}
}
