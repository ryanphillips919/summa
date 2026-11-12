package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"summa/internal/analytics"

	"github.com/go-chi/chi/v5"
)

func (app *App) analyticsRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", app.analyticsPageHandler)

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

// analyticsCache retains the latest ledger-derived response. The mutex also
// ensures that concurrent first requests do not start multiple Python models.
type analyticsCache struct {
	mu       sync.Mutex
	response *analyticsResponse
}

func (app *App) analyticsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	response, available, err := app.loadAnalyticsResponse(r.Context())
	if err != nil {
		log.Printf("load analytics: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"analytics could not run",
		)
		return
	}

	if !available {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (app *App) loadAnalyticsResponse(
	ctx context.Context,
) (analyticsResponse, bool, error) {
	app.analyticsCache.mu.Lock()
	defer app.analyticsCache.mu.Unlock()

	if app.analyticsCache.response != nil {
		return *app.analyticsCache.response, true, nil
	}

	snapshot, err := app.cashDataSource.LoadCashSnapshot(ctx)
	if err != nil {
		return analyticsResponse{}, false, err
	}
	if len(snapshot.Movements) == 0 {
		return analyticsResponse{}, false, nil
	}

	result, err := analytics.Run(ctx, analytics.Request{
		CashMovements:      snapshot.Movements,
		ForecastDays:       snapshot.ForecastDays,
		OpeningCashBalance: snapshot.OpeningCashBalance,
	})
	if err != nil {
		return analyticsResponse{}, false, err
	}

	response := analyticsResponse{
		Response:           result,
		CashMovements:      snapshot.Movements,
		OpeningCashBalance: snapshot.OpeningCashBalance,
	}
	app.analyticsCache.response = &response

	return response, true, nil
}

func (app *App) invalidateAnalyticsCache() {
	app.analyticsCache.mu.Lock()
	defer app.analyticsCache.mu.Unlock()
	app.analyticsCache.response = nil
}

// warmAnalyticsCache runs after app startup, so a presenter normally reaches
// Analytics after its response is already available.
func (app *App) warmAnalyticsCache() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, _, err := app.loadAnalyticsResponse(ctx); err != nil {
		log.Printf("warm analytics cache: %v", err)
	}
}
