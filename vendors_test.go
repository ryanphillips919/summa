package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"summa/internal/db"
)

func TestVendors(t *testing.T) {
	app := newNorthstarTestApp(t)
	post := func(name, email string) *httptest.ResponseRecorder {
		form := url.Values{"name": {name}, "email": {email}}
		req := httptest.NewRequest(http.MethodPost, "/vendors", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, req)
		return rec
	}
	if rec := post("  Supplier  ", "supplier@example.com"); rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("Supplier", "not-email"); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid email: %d", rec.Code)
	}
	if rec := post(" ", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid name: %d", rec.Code)
	}
	other, err := app.queries.CreateOrganization(t.Context(), "Other")
	if err != nil {
		t.Fatal(err)
	}
	otherVendor, err := app.queries.CreateVendor(t.Context(), db.CreateVendorParams{OrganizationID: other.ID, Name: "Other Vendor"})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"name": {"Changed"}, "email": {""}}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/vendors/%d", otherVendor.ID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	blocked := httptest.NewRecorder()
	newRouter(app).ServeHTTP(blocked, req)
	if blocked.Code != http.StatusNotFound {
		t.Fatalf("cross-organization update status %d", blocked.Code)
	}
	owned, err := app.queries.ListVendors(t.Context(), app.orgID)
	if err != nil || len(owned) != 1 {
		t.Fatalf("owned vendors: %v, %v", owned, err)
	}
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/vendors/%d", owned[0].ID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updated := httptest.NewRecorder()
	newRouter(app).ServeHTTP(updated, req)
	if updated.Code != http.StatusSeeOther {
		t.Fatalf("update vendor: %d", updated.Code)
	}
	page := httptest.NewRecorder()
	newRouter(app).ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/vendors", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Changed") || strings.Contains(page.Body.String(), "Other Vendor") {
		t.Fatalf("vendor page status %d", page.Code)
	}
}

func TestCustomerUpdateOrganizationScope(t *testing.T) {
	app := newNorthstarTestApp(t)
	ctx := t.Context()
	other, err := app.queries.CreateOrganization(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := app.queries.CreateCustomer(ctx, db.CreateCustomerParams{OrganizationID: other.ID, Name: "Other Customer"})
	if err != nil {
		t.Fatal(err)
	}
	own, err := app.queries.ListCustomers(ctx, app.orgID)
	if err != nil || len(own) == 0 {
		t.Fatalf("customers: %v, %v", own, err)
	}
	post := func(id int64) int {
		form := url.Values{"name": {"Updated"}, "email": {"updated@example.com"}}
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/customers/%d", id), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		newRouter(app).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(foreign.ID); code != http.StatusNotFound {
		t.Fatalf("foreign customer status %d", code)
	}
	if code := post(own[0].ID); code != http.StatusSeeOther {
		t.Fatalf("own customer status %d", code)
	}
	updated, err := app.queries.GetCustomer(ctx, db.GetCustomerParams{OrganizationID: app.orgID, CustomerID: own[0].ID})
	if err != nil || updated.Name != "Updated" || updated.Email.String != "updated@example.com" {
		t.Fatalf("updated customer: %+v, %v", updated, err)
	}
}
