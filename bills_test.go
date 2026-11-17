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
	"summa/internal/payables"
)

func TestBillWorkflowHTTP(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	vendor, err := app.queries.CreateVendor(ctx, db.CreateVendorParams{OrganizationID: app.orgID, Name: "Supplier"})
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := app.queries.ListAccounts(ctx, app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	var expense, payable, cash int64
	for _, a := range accounts {
		if a.Name == "Rent Expense" {
			expense = a.ID
		}
		if a.Name == "Accounts Payable" {
			payable = a.ID
		}
		if a.Name == "Cash" {
			cash = a.ID
		}
	}
	if expense == 0 || payable == 0 || cash == 0 {
		t.Fatal("fixture accounts missing")
	}
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, req)
		return rec
	}
	form := url.Values{"vendor_id": {fmt.Sprint(vendor.ID)}, "expense_account_id": {fmt.Sprint(expense)}, "payable_account_id": {fmt.Sprint(payable)}, "number": {"INV-SUP-1"}, "description": {"Rent"}, "issue_date": {"2026-11-01"}, "due_date": {"2026-11-30"}, "amount": {"125.00"}, "request_key": {"test-bill-request-0001"}}
	first := post("/bills", form)
	if first.Code != http.StatusSeeOther {
		t.Fatalf("post bill: %d %s", first.Code, first.Body.String())
	}
	second := post("/bills", form)
	if second.Code != http.StatusSeeOther || second.Header().Get("Location") != first.Header().Get("Location") {
		t.Fatalf("retry bill: %d", second.Code)
	}
	bills, err := payables.List(ctx, app.sqlDB, app.orgID)
	if err != nil || len(bills) != 1 {
		t.Fatalf("bills: %+v %v", bills, err)
	}
	path := first.Header().Get("Location")
	page := httptest.NewRecorder()
	newRouter(app).ServeHTTP(page, httptest.NewRequest(http.MethodGet, path, nil))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "$125.00") {
		t.Fatalf("bill page %d", page.Code)
	}
	payment := url.Values{"payment_account_id": {fmt.Sprint(cash)}, "amount": {"25.00"}, "date": {"2026-11-02"}, "request_key": {"test-payment-request-001"}}
	if rec := post(path+"/payments", payment); rec.Code != http.StatusSeeOther {
		t.Fatalf("payment: %d %s", rec.Code, rec.Body.String())
	}
	bill, err := payables.Get(ctx, app.sqlDB, app.orgID, bills[0].ID)
	if err != nil || bill.Paid != 2500 {
		t.Fatalf("paid bill: %+v %v", bill, err)
	}
	for _, tc := range []struct{ query, contains string }{
		{"?as_of=2026-11-02", "$100.00"},
		{"?as_of=2026-11-02&format=csv", "100.00"},
	} {
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/bill-aging"+tc.query, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Fatalf("bill aging %s: %d %s", tc.query, rec.Code, rec.Body.String())
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := payables.Pay(ctx, app.sqlDB, app.orgID, bill.ID, cash, 10000, "2026-11-03", fmt.Sprintf("bill-concurrent-payment-%d", i))
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent bill payments: %d succeeded", success)
	}
	bill, err = payables.Get(ctx, app.sqlDB, app.orgID, bill.ID)
	if err != nil || bill.Paid != 12500 {
		t.Fatalf("concurrent bill balance: %+v %v", bill, err)
	}
}
