package main

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"summa/internal/db"
	"summa/internal/ledger"
)

func TestGeneralLedgerHTTP(t *testing.T) {
	app := newNorthstarTestApp(t)
	accounts, err := app.queries.ListAccounts(t.Context(), app.orgID)
	if err != nil || len(accounts) == 0 {
		t.Fatalf("accounts = %v, %v", accounts, err)
	}
	for _, tc := range []struct {
		query    string
		status   int
		contains string
	}{
		{fmt.Sprintf("?account_id=%d&from=2026-05-01&to=2026-10-31", accounts[0].ID), 200, "Opening balance"},
		{fmt.Sprintf("?account_id=%d&from=2026-05-01&to=2026-10-31&format=csv", accounts[0].ID), 200, "Journal entry"},
		{"?account_id=999999999&from=2026-05-01&to=2026-10-31", 404, "404"},
		{"?from=2026-10-31&to=2026-05-01", 400, "valid date range"},
	} {
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/general-ledger"+tc.query, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("%s: status %d, body %q", tc.query, rec.Code, rec.Body.String())
		}
	}
}

func TestCSVSafeText(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"Rent", "Rent"}, {"=1+1", "'=1+1"}, {"  @cmd", "'  @cmd"}, {"\ttext", "'\ttext"}, {"-1", "'-1"},
	} {
		if got := csvSafeText(tc.input); got != tc.want {
			t.Errorf("csvSafeText(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestIncomeStatementNorthstar(t *testing.T) {
	app := newNorthstarTestApp(t)
	report, err := ledger.BuildIncomeStatement(t.Context(), app.sqlDB, app.orgID, "2026-05-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	if report.Revenue != 16_475_000 || report.Expenses != 27_944_998 || report.NetIncome != -11_469_998 {
		t.Fatalf("unexpected income statement: %+v", report)
	}
	if got := centsCSV(math.MinInt64); got != "-92233720368547758.08" {
		t.Errorf("minimum cents = %s", got)
	}
	for _, tc := range []struct {
		query    string
		status   int
		contains string
	}{
		{"?from=2026-05-01&to=2026-10-31", 200, "-$114,699.98"},
		{"?from=2026-05-01&to=2026-10-31&format=csv", 200, "-114699.98"},
		{"?from=2026-10-31&to=2026-05-01", 400, "valid date range"},
	} {
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/profit-and-loss"+tc.query, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("%s: status %d, body %q", tc.query, rec.Code, rec.Body.String())
		}
	}
}

func TestBalanceSheetNorthstar(t *testing.T) {
	app := newNorthstarTestApp(t)
	report, err := ledger.BuildBalanceSheet(t.Context(), app.sqlDB, app.orgID, "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	if report.Assets != report.TotalLiabilitiesAndEquity || report.AccumulatedIncome != -11_469_998 {
		t.Fatalf("unexpected balance sheet: %+v", report)
	}
	for _, tc := range []struct {
		query    string
		status   int
		contains string
	}{
		{"?as_of=2026-10-31", 200, "-$114,699.98"},
		{"?as_of=2026-10-31&format=csv", 200, "-114699.98"},
		{"?as_of=2026-02-30", 400, "valid report date"},
	} {
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/balance-sheet"+tc.query, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("%s: status %d, body %q", tc.query, rec.Code, rec.Body.String())
		}
	}
}

func TestAnalyticsIgnoresUnfinalizedCash(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	before, err := app.cashDataSource.LoadCashSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
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
	if cashID == 0 {
		t.Fatal("cash account missing")
	}
	txn, err := app.queries.CreateTransaction(ctx, db.CreateTransactionParams{OrganizationID: app.orgID, Date: "2030-01-01", Description: "Incomplete"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.queries.CreateEntry(ctx, db.CreateEntryParams{TransactionID: txn.ID, AccountID: cashID, Direction: "debit", Amount: 100_000}); err != nil {
		t.Fatal(err)
	}
	after, err := app.cashDataSource.LoadCashSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("unfinalized entry changed cash snapshot: before %+v, after %+v", before, after)
	}
}

func TestInvoiceAgingHTTP(t *testing.T) {
	app := newNorthstarTestApp(t)
	for _, tc := range []struct {
		query    string
		status   int
		contains string
	}{
		{"?as_of=2026-10-31", 200, "Earlier receipts recorded directly"},
		{"?as_of=2026-10-31&format=csv", 200, "Earlier unallocated ledger receipts"},
		{"?as_of=2026-02-30", 400, "valid report date"},
	} {
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/invoice-aging"+tc.query, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("%s: status %d, body %q", tc.query, rec.Code, rec.Body.String())
		}
	}
}

func TestTrialBalanceHTTP(t *testing.T) {
	app := newNorthstarTestApp(t)
	for _, tc := range []struct {
		query                 string
		status                int
		contains, contentType string
	}{
		{"?as_of=2026-10-31", http.StatusOK, "Trial balance", "text/html"},
		{"?as_of=2026-10-31&format=csv", http.StatusOK, "Account,Type,Debit,Credit", "text/csv"},
		{"?as_of=2026-02-30", http.StatusBadRequest, "valid report date", "text/plain"},
	} {
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/trial-balance"+tc.query, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) || !strings.HasPrefix(rec.Header().Get("Content-Type"), tc.contentType) {
			t.Errorf("%s: status %d, content type %q, body %q", tc.query, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
}
