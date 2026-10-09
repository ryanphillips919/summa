
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	newRouter().ServeHTTP(rec, req)

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
