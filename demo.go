package main

import (
	"context"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

func (a *App) demoRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.demoHandler)
	r.Get("/watch", a.demoWatcherHandler)
	return r
}

func (a *App) demoHandler(w http.ResponseWriter, r *http.Request) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	document, err := a.monthlyDocument(r.Context(), time.Time{})
	if err != nil {
		http.Error(w, "Could not load the monthly ledger.", http.StatusInternalServerError)
		return
	}
	if err := LedgerPage(org.Name, document).Render(r.Context(), w); err != nil {
		return
	}
}

func (a *App) demoWatcherHandler(w http.ResponseWriter, r *http.Request) {
	a.watchLedgerUpdates(w, r, func(ctx context.Context) (templ.Component, error) {
		document, err := a.monthlyDocument(ctx, time.Time{})
		if err != nil {
			return nil, err
		}
		return MonthlyLedgerTable(document), nil
	})
}
