package main

import (
	"context"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

func (a *App) timelineRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.timelineHandler)
	r.Get("/watch", a.timelineWatcherHandler)
	return r
}

func (a *App) timelineHandler(w http.ResponseWriter, r *http.Request) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	transactions, err := a.journalTransactions(r.Context())
	if err != nil {
		http.Error(w, "Could not load ledger history.", http.StatusInternalServerError)
		return
	}
	if err := TimelinePage(org.Name, transactions).Render(r.Context(), w); err != nil {
		return
	}
}

func (a *App) timelineWatcherHandler(w http.ResponseWriter, r *http.Request) {
	a.watchLedgerUpdates(w, r, func(ctx context.Context) (templ.Component, error) {
		transactions, err := a.journalTransactions(ctx)
		if err != nil {
			return nil, err
		}
		return TimelineEntries(transactions), nil
	})
}
