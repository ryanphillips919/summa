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
	// Every slide in the tour must exist, or → would lead nowhere after a renumber.
	for _, stop := range tourStops {
		if slide, ok := strings.CutPrefix(stop, "/presentation#"); ok && !strings.Contains(response.Body.String(), fmt.Sprintf(`id="%s"`, slide)) {
			t.Errorf("tour stop %q is not a slide in the deck", stop)
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
