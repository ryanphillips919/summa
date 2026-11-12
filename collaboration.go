package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/a-h/templ"
	"github.com/nats-io/nats.go"
	"github.com/starfederation/datastar-go/datastar"
)

func (a *App) ledgerSubject() string {
	return fmt.Sprintf("ledger.updated.%d", a.orgID)
}

// Notifications contain no financial data; listeners reload it from SQLite.
func (a *App) notifyLedgerChanged() {
	a.invalidateAnalyticsCache()

	if a.nc == nil {
		return
	}
	if err := a.nc.Publish(a.ledgerSubject(), nil); err != nil {
		log.Printf("notify ledger update: %v", err)
	}
}

func (a *App) watchLedgerUpdates(w http.ResponseWriter, r *http.Request, render func(context.Context) (templ.Component, error)) {
	if a.nc == nil {
		http.Error(w, "Live updates are unavailable.", http.StatusServiceUnavailable)
		return
	}
	updates := make(chan *nats.Msg, 16)
	subscription, err := a.nc.ChanSubscribe(a.ledgerSubject(), updates)
	if err != nil {
		http.Error(w, "Could not subscribe to ledger updates.", http.StatusInternalServerError)
		return
	}
	defer subscription.Unsubscribe()
	if err := a.nc.Flush(); err != nil {
		http.Error(w, "Could not subscribe to ledger updates.", http.StatusInternalServerError)
		return
	}
	sse := datastar.NewSSE(w, r)
	patch := func() error {
		component, err := render(r.Context())
		if err != nil {
			return err
		}
		return sse.PatchElementTempl(component)
	}
	// Refresh once after subscribing so a change during page load is not missed.
	if err := patch(); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-updates:
			if err := patch(); err != nil {
				return
			}
		}
	}
}
