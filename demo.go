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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	changed := doc.diffCells(cells)
	if len(changed) == 0 {
		// nothing changed → no KV write, so no watcher push. we have to
		// exit edit mode ourselves
		datastar.NewSSE(w, r).PatchElementTempl(Demo(doc, nil, 0))
		return
	}

	doc.setCells(changed)
	data, _ := json.Marshal(changed)
	rev, _ := a.kv.Put(r.Context(), "demo.update", data)
	fmt.Printf("demo.update @ %d -> %s\n", rev, data)
	// deliberately no patch here: the watcher will push Demo(doc, changed, rev)
	// to every client including this one, so there's exactly one render and one flash
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) demoEditHandler(w http.ResponseWriter, r *http.Request) {
	datastar.NewSSE(w, r).PatchElementTempl(DemoEdit(doc))
}

func (a *App) demoCancelHandler(w http.ResponseWriter, r *http.Request) {
	datastar.NewSSE(w, r).PatchElementTempl(Demo(doc, nil, 0))
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
// the entry's value is the delta of changed cells, so we pass it (plus the revision)
// into the templ so only the cells that changed get the flash animation
// easiest real-time collaborative functionality ever
func (a *App) demoWatcherHandler(w http.ResponseWriter, r *http.Request) {
	watcher, err := a.kv.Watch(r.Context(), "demo.*", jetstream.UpdatesOnly())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer watcher.Stop()
	sse := datastar.NewSSE(w, r)
	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		var changed map[string]int64
		json.Unmarshal(entry.Value(), &changed)
		if err := sse.PatchElementTempl(Demo(doc, changed, entry.Revision())); err != nil {
			return
		}
	}
}
