package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"

	"summa/internal/analytics"
	"summa/views"

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
	currentTransactionID, err := app.queries.LatestTransactionID(
		r.Context(),
		app.orgID,
	)
	if err != nil {
		log.Printf("read latest ledger transaction: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"analytics could not load",
		)
		return
	}

	run, err := app.queries.GetLatestAnalyticsRun(r.Context(), app.orgID)
	if errors.Is(err, sql.ErrNoRows) ||
		(err == nil && currentTransactionID > run.SourceTransactionID) {
		_, err = analytics.RunDatabase(
			r.Context(),
			app.databasePath,
			app.orgID,
			30,
			120,
		)
		if errors.Is(err, analytics.ErrNoCashMovements) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			log.Printf("run analytics: %v", err)
			writeJSONError(
				w,
				http.StatusInternalServerError,
				"analytics could not run",
			)
			return
		}
		run, err = app.queries.GetLatestAnalyticsRun(r.Context(), app.orgID)
	}
	if err != nil {
		log.Printf("read analytics run: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"analytics could not load",
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(run.ResultJson))
}
