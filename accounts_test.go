package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAccountCreation(t *testing.T) {
	app := newNorthstarTestApp(t)
	post := func(name, kind string) *httptest.ResponseRecorder {
		form := url.Values{"name": {name}, "type": {kind}}
		req := httptest.NewRequest(http.MethodPost, "/accounts", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, req)
		return rec
	}
	if rec := post("  New Expense  ", "expense"); rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("new expense", "asset"); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d", rec.Code)
	}
	if rec := post("Other", "invalid"); rec.Code != http.StatusBadRequest {
		t.Fatalf("type: %d", rec.Code)
	}
	if rec := post("  ", "asset"); rec.Code != http.StatusBadRequest {
		t.Fatalf("name: %d", rec.Code)
	}
	page := httptest.NewRecorder()
	newRouter(app).ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/accounts", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "New Expense") {
		t.Fatalf("accounts page: %d", page.Code)
	}
}
