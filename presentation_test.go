package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPresentationAndAssets(t *testing.T) {
	router := newRouter(&App{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/presentation", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("presentation status = %d, want 200", response.Code)
	}
	for slide := 1; slide <= totalSlides; slide++ {
		if !strings.Contains(response.Body.String(), fmt.Sprintf(`id="s%d"`, slide)) {
			t.Errorf("presentation missing slide %d", slide)
		}
	}
	for _, path := range []string{
		"/assets/images/summa-tour.gif",
		"/assets/images/architecture.gif",
		"/assets/images/presentation-journal-qr.png",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Errorf("asset %s: status = %d, bytes = %d", path, response.Code, response.Body.Len())
		}
	}
}
