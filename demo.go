package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/starfederation/datastar-go/datastar"
)

// before there's database tables just store state as json
var (
	doc     = dummyDocument()
	clients atomic.Int64
)

func (a *App) demoRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.demoHandler)
	r.Post("/", a.demoPostHandler)
	r.Get("/edit", a.demoEditHandler)
	r.Get("/cancel", a.demoCancelHandler)
	r.Get("/watch", a.demoWatcherHandler)
	r.Get("/scrub", a.demoScrubHandler)
	r.Get("/play", a.demoPlayHandler)
	return r
}

func (a *App) demoHandler(w http.ResponseWriter, r *http.Request) {
	// to start off we need to render the page or else it won't render
	Page(doc, clients.Load()).Render(r.Context(), w)
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
	clients.Add(1)
	a.kv.Put(r.Context(), "demo.clients", nil)
	defer func() {
		clients.Add(-1)
		a.kv.Put(context.Background(), "demo.clients", nil)
	}()
	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Key() == "demo.clients" {
			if err := sse.PatchElementTempl(Online(clients.Load())); err != nil {
				return
			}
			continue
		}
		var changed map[string]int64
		json.Unmarshal(entry.Value(), &changed)
		if err := sse.PatchElementTempl(Demo(doc, changed, entry.Revision())); err != nil {
			return
		}
	}
}

func (a *App) demoScrubHandler(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		Scrub int `json:"scrub"`
	}
	datastar.ReadSignals(r, &sig)
	entries, _ := a.kv.History(r.Context(), "demo.update")
	d := dummyDocument()
	var changed map[string]int64
	var rev uint64
	for i, e := range entries {
		if i >= sig.Scrub {
			break
		}
		changed = map[string]int64{}
		json.Unmarshal(e.Value(), &changed)
		d.setCells(changed)
		rev = e.Revision()
	}
	datastar.NewSSE(w, r).PatchElementTempl(Demo(d, changed, rev))
}

func (a *App) demoPlayHandler(w http.ResponseWriter, r *http.Request) {
	entries, _ := a.kv.History(r.Context(), "demo.update")
	sse := datastar.NewSSE(w, r)

	d := dummyDocument()
	for i, e := range entries {
		changed := map[string]int64{}
		json.Unmarshal(e.Value(), &changed)
		d.setCells(changed)

		if err := sse.PatchElementTempl(Demo(d, changed, e.Revision())); err != nil {
			return
		}
		sse.MarshalAndPatchSignals(map[string]int{"scrub": i + 1})

		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
	}
}
