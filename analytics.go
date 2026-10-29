package main

import (
	"net/http"

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
	http.ServeFile(w, r, "analytics/demo/dashboard.html")
}
