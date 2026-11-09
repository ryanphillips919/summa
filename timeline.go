package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/starfederation/datastar-go/datastar"
)

func (a *App) timelineRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.timelineHandler)
	r.Get("/watch", a.timelineWatcherHandler)
	return r
}

func (a *App) timelineHandler(w http.ResponseWriter, r *http.Request) {
	Timeline().Render(r.Context(), w)
}

func (a *App) timelineWatcherHandler(w http.ResponseWriter, r *http.Request) {
	// I used IncludeHistory here because it includes all the history that nat's KV store has, not just updates since the server began
	watcher, _ := a.kv.Watch(r.Context(), "demo.update", jetstream.IncludeHistory())
	defer watcher.Stop()
	sse := datastar.NewSSE(w, r)
	var history []Entry
	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		// since we're not storing the timeline data in sqlite yet, we'll just have a local []Entry slice that we'll store the history
		history = append(history, Entry{
			created: entry.Created(),
			rev:     entry.Revision(),
			values:  string(entry.Value()),
		})
		if err := sse.PatchElementTempl(TimelineEntry(EntrySystem{entry: history})); err != nil {
			return
		}
	}
}
