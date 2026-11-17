package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppInitFallsBackFromDefaultLegacyDatabase(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SUMMA_DB", "")
	if err := os.MkdirAll("data", 0o755); err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", "data/summa.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE transactions (id INTEGER PRIMARY KEY, occurred_on TEXT NOT NULL); INSERT INTO transactions VALUES (1, '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	app, cleanup, err := appInit()
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.queries.GetOrganization(t.Context(), app.orgID)
	if err != nil || org.Name != "Northstar Equipment & Supply" {
		t.Fatalf("fallback organization = %+v, %v", org, err)
	}
	cleanup()
	legacy, err = sql.Open("sqlite", "data/summa.db")
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	var date string
	if err := legacy.QueryRow(`SELECT occurred_on FROM transactions WHERE id = 1`).Scan(&date); err != nil || date != "2026-01-01" {
		t.Fatalf("legacy transaction = %q, %v", date, err)
	}
	var migrations int
	if err := legacy.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'goose_db_version'`).Scan(&migrations); err != nil || migrations != 0 {
		t.Fatalf("legacy migration table = %d, %v", migrations, err)
	}
}

func TestOpenDatabaseRejectsLegacySchemaBeforeMigrating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`CREATE TABLE transactions (id INTEGER PRIMARY KEY, occurred_on TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := openDatabase(context.Background(), path)
	if opened != nil {
		opened.Close()
		t.Fatal("opened incompatible database")
	}
	if err == nil || !strings.Contains(err.Error(), "incompatible older Summa schema") {
		t.Fatalf("unexpected error: %v", err)
	}
	conn, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var migrationTable int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'goose_db_version'`).Scan(&migrationTable); err != nil || migrationTable != 0 {
		t.Fatalf("migration table = %d, error = %v", migrationTable, err)
	}
}

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

func TestListenAddressDefaultsToLoopback(t *testing.T) {
	t.Setenv("SUMMA_LISTEN_ADDR", "")
	if got := listenAddress(); got != "127.0.0.1:8080" {
		t.Fatalf("default address = %q", got)
	}
	t.Setenv("SUMMA_LISTEN_ADDR", ":9090")
	if got := listenAddress(); got != ":9090" {
		t.Fatalf("custom address = %q", got)
	}
}
