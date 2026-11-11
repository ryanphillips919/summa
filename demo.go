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
	qrcode "github.com/skip2/go-qrcode"
	"github.com/starfederation/datastar-go/datastar"
)

// before there's database tables just store state as json
var (
	doc     = dummyDocument()
	clients atomic.Int64
	editing atomic.Bool
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
	r.Post("/start", a.demoSetEditing(true))
	r.Post("/stop", a.demoSetEditing(false))
	r.Get("/qr", a.demoQRHandler)
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
		datastar.NewSSE(w, r).PatchElementTempl(Demo(doc, nil, 0), datastar.WithModeReplace())
		return
	}
	doc.setCells(changed)
	data, _ := json.Marshal(changed)
	rev, _ := a.kv.Put(r.Context(), "demo.update", data)
	fmt.Printf("demo.update @ %d -> %s\n", rev, data)
	datastar.NewSSE(w, r).PatchElementTempl(Demo(doc, changed, rev), datastar.WithModeReplace())
}

func (a *App) demoEditHandler(w http.ResponseWriter, r *http.Request) {
	datastar.NewSSE(w, r).PatchElementTempl(DemoEdit(doc))
}

func (a *App) demoCancelHandler(w http.ResponseWriter, r *http.Request) {
	datastar.NewSSE(w, r).PatchElementTempl(Demo(doc, nil, 0), datastar.WithModeReplace())
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
	sse.MarshalAndPatchSignals(map[string]bool{"open": editing.Load()})
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
		if entry.Key() == "demo.editing" {
			sse.MarshalAndPatchSignals(map[string]bool{"open": editing.Load()})
			continue
		}
		var changed map[string]int64
		json.Unmarshal(entry.Value(), &changed)
		sse.MarshalAndPatchSignals(changed)
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

func (a *App) demoSetEditing(open bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		editing.Store(open)
		a.kv.Put(r.Context(), "demo.editing", nil)
	}
}

func (a *App) demoQRHandler(w http.ResponseWriter, r *http.Request) {
	png, _ := qrcode.Encode("https://"+r.Host+"/demo", qrcode.Medium, 512)
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}
