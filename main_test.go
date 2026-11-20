package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"summa/internal/db"
)

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	newRouter(&App{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var response struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if response.Status != "ok" || response.Service != "summa" {
		t.Errorf("unexpected health response: %+v", response)
	}
}

func TestPresentationSeed(t *testing.T) {
	conn, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "summa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	queries := db.New(conn)
	orgID, err := seedDevData(context.Background(), conn, queries)
	if err != nil {
		t.Fatal(err)
	}
	customers, err := queries.ListCustomers(context.Background(), orgID)
	if err != nil || len(customers) != 12 {
		t.Fatalf("seed customers: got %d, error %v", len(customers), err)
	}
	invoices, err := queries.ListInvoices(context.Background(), orgID)
	if err != nil || len(invoices) != 20 {
		t.Fatalf("seed invoices: got %d, error %v", len(invoices), err)
	}
}
