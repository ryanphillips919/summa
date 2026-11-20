package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"summa/internal/analytics"
	"summa/views"

	"github.com/go-chi/chi/v5"
)

// analyticsRoutes is the dashboard page, mounted at /analytics.
func (a *App) analyticsRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.analyticsPageHandler)
	// Serves analytics.js and any later analytics-specific browser assets.
	r.Handle("/*", http.StripPrefix("/analytics/", http.FileServer(http.Dir("analytics/web"))))
	return r
}

// analyticsAPIRoutes is the JSON the dashboard fetches, mounted at /api/analytics.
func (a *App) analyticsAPIRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.analyticsHandler)
	r.Get("/demo", a.analyticsDemoHandler)
	r.Post("/run", a.analyticsRunHandler)
	return r
}

type analyticsResponse struct {
	analytics.Response
	CashMovements      []analytics.CashMovement `json:"cash_movements"`
	OpeningCashBalance float64                  `json:"opening_cash_balance"`
}

func (a *App) analyticsPageHandler(w http.ResponseWriter, r *http.Request) {
	if err := views.AnalyticsPage().Render(r.Context(), w); err != nil {
		http.Error(w, "could not render analytics page", http.StatusInternalServerError)
	}
}

func (a *App) analyticsHandler(w http.ResponseWriter, r *http.Request) {
	snapshot, err := a.cashDataSource.LoadCashSnapshot(r.Context())
	if err != nil {
		log.Printf("load analytics data: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "analytics data is unavailable")
		return
	}

	result, err := analytics.Run(r.Context(), analytics.Request{
		CashMovements:      snapshot.Movements,
		ForecastDays:       snapshot.ForecastDays,
		OpeningCashBalance: snapshot.OpeningCashBalance,
	})
	if err != nil {
		log.Printf("run analytics: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "analytics could not run")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(analyticsResponse{
		Response:           result,
		CashMovements:      snapshot.Movements,
		OpeningCashBalance: snapshot.OpeningCashBalance,
	})
}

func (a *App) analyticsDemoHandler(w http.ResponseWriter, r *http.Request) {
	fixture, err := os.ReadFile("analytics/data/demo_cash_movements.json")
	if err != nil {
		log.Printf("read analytics demo fixture: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "analytics demo data is unavailable")
		return
	}

	var request analytics.Request
	if err := json.Unmarshal(fixture, &request); err != nil {
		log.Printf("decode analytics demo fixture: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "analytics demo data is invalid")
		return
	}

	result, err := analytics.Run(r.Context(), request)
	if err != nil {
		log.Printf("run analytics demo: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "analytics demo could not run")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(analyticsResponse{
		Response:           result,
		CashMovements:      request.CashMovements,
		OpeningCashBalance: request.OpeningCashBalance,
	})
}

func (a *App) analyticsRunHandler(w http.ResponseWriter, r *http.Request) {
	var request analytics.Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid analytics request: "+err.Error())
		return
	}

	result, err := analytics.Run(r.Context(), request)
	if err != nil {
		log.Printf("analytics request failed: %v", err)
		writeJSONError(w, http.StatusUnprocessableEntity, "analytics could not process these cash movements")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
