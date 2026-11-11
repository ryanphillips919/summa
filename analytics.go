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

	// Serves analytics.js and any later analytics-specific browser assets.
	r.Handle(
		"/*",
		http.StripPrefix(
			"/analytics/",
			http.FileServer(http.Dir("analytics/web")),
		),
	)

	return r
}

func (app *App) analyticsPageHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	org, err := app.queries.GetOrganization(r.Context(), app.orgID)
	if err != nil {
		http.Error(w, "could not load organization", http.StatusInternalServerError)
		return
	}
	if err := AnalyticsPage(org.Name).Render(r.Context(), w); err != nil {
		http.Error(
			w,
			"could not render analytics page",
			http.StatusInternalServerError,
		)
	}
}

type analyticsResponse struct {
	analytics.Response
	CashMovements      []analytics.CashMovement `json:"cash_movements"`
	OpeningCashBalance float64                  `json:"opening_cash_balance"`
}

func (app *App) analyticsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	snapshot, err := app.cashDataSource.LoadCashSnapshot(r.Context())
	if err != nil {
		log.Printf("load analytics data: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"analytics data is unavailable",
		)
		return
	}
	if len(snapshot.Movements) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	result, err := analytics.Run(r.Context(), analytics.Request{
		CashMovements:      snapshot.Movements,
		ForecastDays:       snapshot.ForecastDays,
		OpeningCashBalance: snapshot.OpeningCashBalance,
	})
	if err != nil {
		log.Printf("run analytics: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"analytics could not run",
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(analyticsResponse{
		Response:           result,
		CashMovements:      snapshot.Movements,
		OpeningCashBalance: snapshot.OpeningCashBalance,
	})
}
