package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/starfederation/datastar-go/datastar"
	"summa/views"
)

func (a *App) timelineRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.timelineHandler)
	r.Get("/watch", a.timelineWatcherHandler)
	return r
}

func (a *App) timelineHandler(w http.ResponseWriter, r *http.Request) {
	views.Timeline().Render(r.Context(), w)
}

func (a *App) timelineWatcherHandler(w http.ResponseWriter, r *http.Request) {
	// I used IncludeHistory here because it includes all the history that nat's KV store has, not just updates since the server began
	watcher, _ := a.kv.Watch(r.Context(), "demo.update", jetstream.IncludeHistory())
	defer watcher.Stop()
	sse := datastar.NewSSE(w, r)
	var history []views.Entry
	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		// since we're not storing the timeline data in sqlite yet, we'll just have a local []Entry slice that we'll store the history
		history = append(history, views.Entry{
			Created: entry.Created(),
			Rev:     entry.Revision(),
			Values:  string(entry.Value()),
		})
		if err := sse.PatchElementTempl(views.TimelineEntry(views.EntrySystem{Entry: history})); err != nil {
			return
		}
	}
}
