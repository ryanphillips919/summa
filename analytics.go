package main

import (
	"encoding/json"
	"log"
	"net/http"

	"summa/internal/analytics"

	"github.com/go-chi/chi/v5"
)

func (app *App) analyticsRoutes() chi.Router {
	r := chi.NewRouter()

	// The short analytics dashboard URL.
	r.Get("/", app.analyticsPageHandler)

	// Serves dashboard.js and any later analytics-specific browser assets.
	r.Handle(
		"/*",
		http.StripPrefix(
			"/analytics/",
			http.FileServer(http.Dir("analytics/demo")),
		),
	)

	return r
}

func (app *App) analyticsPageHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if err := AnalyticsPage().Render(r.Context(), w); err != nil {
		http.Error(
			w,
			"could not render analytics page",
			http.StatusInternalServerError,
		)
	}
}

type analyticsDemoResponse struct {
	analytics.Response
	CashMovements      []analytics.CashMovement `json:"cash_movements"`
	OpeningCashBalance float64                  `json:"opening_cash_balance"`
}

func (app *App) demoAnalyticsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	snapshot, err := app.cashDataSource.LoadCashSnapshot(r.Context())
	if err != nil {
		log.Printf("load analytics demo data: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"analytics demo data is unavailable",
		)
		return
	}

	result, err := analytics.Run(r.Context(), analytics.Request{
		CashMovements:      snapshot.Movements,
		ForecastDays:       snapshot.ForecastDays,
		OpeningCashBalance: snapshot.OpeningCashBalance,
	})
	if err != nil {
		log.Printf("run analytics demo: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"analytics demo could not run",
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(analyticsDemoResponse{
		Response:           result,
		CashMovements:      snapshot.Movements,
		OpeningCashBalance: snapshot.OpeningCashBalance,
	})
}
