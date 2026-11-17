package main

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseCents(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		bad   bool
	}{
		{input: "", want: 0},
		{input: "12", want: 1200},
		{input: "12.5", want: 1250},
		{input: "12.50", want: 1250},
		{input: " 0.01 ", want: 1},
		{input: "-2", bad: true},
		{input: "1.234", bad: true},
		{input: "one", bad: true},
	}
	for _, test := range tests {
		got, err := parseCents(test.input)
		if test.bad && err == nil {
			t.Errorf("parseCents(%q) unexpectedly succeeded", test.input)
		}
		if !test.bad && (err != nil || got != test.want) {
			t.Errorf("parseCents(%q) = %d, %v; want %d, nil", test.input, got, err, test.want)
		}
	}
}

func TestManualJournalReversalWorkflow(t *testing.T) {
	app := newNorthstarTestApp(t)
	accounts, err := app.queries.ListAccounts(t.Context(), app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) < 2 {
		t.Fatal("missing accounts")
	}
	form := url.Values{"date": {"2026-11-01"}, "description": {"Test adjustment"}, "account_id": {fmt.Sprint(accounts[0].ID), fmt.Sprint(accounts[1].ID)}, "debit": {"5.00", ""}, "credit": {"", "5.00"}}
	req := httptest.NewRequest(http.MethodPost, "/journal", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	newRouter(app).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("post adjustment: %d %s", rec.Code, rec.Body.String())
	}
	transactions, err := app.queries.ListTransactions(t.Context(), app.orgID)
	if err != nil {
		t.Fatal(err)
	}
	original := transactions[0]
	page := httptest.NewRecorder()
	newRouter(app).ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/journal", nil))
	path := fmt.Sprintf("/journal/%d/reverse", original.ID)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), path) {
		t.Fatalf("missing reversal form: %d", page.Code)
	}
	perform := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"date": {"2026-11-02"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		newRouter(app).ServeHTTP(response, r)
		return response
	}
	if response := perform(); response.Code != http.StatusSeeOther {
		t.Fatalf("reverse: %d %s", response.Code, response.Body.String())
	}
	if response := perform(); response.Code != http.StatusNotFound {
		t.Fatalf("duplicate reversal: %d", response.Code)
	}
}

func TestFormatDollars(t *testing.T) {
	for _, test := range []struct {
		cents int64
		want  string
	}{
		{0, "$0.00"}, {1, "$0.01"}, {32_550, "$325.50"},
		{11_155_002, "$111,550.02"}, {-11_469_998, "-$114,699.98"},
		{math.MinInt64, "-$92,233,720,368,547,758.08"},
	} {
		if got := formatDollars(test.cents); got != test.want {
			t.Errorf("formatDollars(%d) = %q, want %q", test.cents, got, test.want)
		}
	}
}
