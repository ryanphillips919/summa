package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/starfederation/datastar-go/datastar"
)

// before there's database tables just store state as json
var doc = dummyDocument()

func (a *App) demoRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.demoHandler)
	r.Post("/", a.demoPostHandler)
	r.Get("/edit", a.demoEditHandler)
	r.Get("/cancel", a.demoCancelHandler)
	r.Get("/watch", a.demoWatcherHandler)
	return r
}

func (a *App) demoHandler(w http.ResponseWriter, r *http.Request) {
	// to start off we need to render the page or else it won't render
	Page(doc).Render(r.Context(), w)
}

func (a *App) demoPostHandler(w http.ResponseWriter, r *http.Request) {
	var cells map[string]int64
	if err := datastar.ReadSignals(r, &cells); err != nil {
		return
	}
	doc.setCells(cells)
	// usually we wouldn't need to do this because signals are json under the hood
	// but since we converted back to a map we turn it back into json so we can
	// pass it around NATS kv storage
	data, _ := json.Marshal(cells)
	a.kv.Put(r.Context(), "demo.update", data)
	entry, _ := a.kv.Get(r.Context(), "demo.update")
	// we can see under the hood what exactly NATS is doing when we use kv.PUT
	fmt.Printf("%s @ %d -> %q\n", entry.Key(), entry.Revision(), string(entry.Value()))
	datastar.NewSSE(w, r).PatchElementTempl(Demo(doc))
}

func (a *App) demoEditHandler(w http.ResponseWriter, r *http.Request) {
	datastar.NewSSE(w, r).PatchElementTempl(DemoEdit(doc))
}

func (a *App) demoCancelHandler(w http.ResponseWriter, r *http.Request) {
	datastar.NewSSE(w, r).PatchElementTempl(Demo(doc))
}

// this is the function that lets us update all clients in real-time!
// the lines of code and logic itself is really simple
// all we do is call the watch function on our jetstream bucket.
// we add the key to be "demo.*" because NATS "subscribes" on channels, not urls
// we also add the jetstream.UpdatesOnly() which means it should only be called when there's updates,
// then, using datastar start up a new Server Sent Event
// After that we just have a loop where watcher will return all the entries that have been updated in the bucket
// if no updates? do nothing
// if there's an update? re-render the page using datastar!
// easiest real-time collaborative functionality ever
func (a *App) demoWatcherHandler(w http.ResponseWriter, r *http.Request) {
	watcher, _ := a.kv.Watch(r.Context(), "demo.*", jetstream.UpdatesOnly())
	defer watcher.Stop()
	sse := datastar.NewSSE(w, r)
	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if err := sse.PatchElementTempl(Demo(doc)); err != nil {
			return
		}
	}
}
