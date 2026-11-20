package main

import (
	"errors"
	"log"
	"net/http"

	"summa/internal/analytics"
	"summa/views"

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
	if err := views.AnalyticsPage().Render(r.Context(), w); err != nil {
		http.Error(
			w,
			"could not render analytics page",
			http.StatusInternalServerError,
		)
	}
}

func (app *App) analyticsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	result, err := analytics.RunDatabase(
		r.Context(),
		app.databasePath,
		app.orgID,
		30,
		120,
	)
	if err != nil {
		if errors.Is(err, analytics.ErrNoCashMovements) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Printf("run analytics: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"analytics could not run",
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(result)
}
