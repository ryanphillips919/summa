package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go/jetstream"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/starfederation/datastar-go/datastar"
)

// This state is isolated from the SQLite-backed accounting ledger.
type collaborationState struct {
	mu      sync.Mutex
	clients atomic.Int64
	editing atomic.Bool
}

func (a *App) collaborationRoutes() chi.Router {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a.kv == nil {
				http.Error(w, "Collaborative prototype unavailable", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/", a.collaborationPage)
	r.Post("/", a.collaborationPost)
	r.Get("/edit", a.collaborationEdit)
	r.Get("/cancel", a.collaborationCancel)
	r.Get("/watch", a.collaborationWatch)
	r.Get("/scrub", a.collaborationScrub)
	r.Get("/play", a.collaborationPlay)
	r.Post("/start", a.collaborationSetEditing(true))
	r.Post("/stop", a.collaborationSetEditing(false))
	r.Get("/qr", a.collaborationQR)
	return r
}

// Each revision contains a complete simulated document so retained history remains
// self-contained even after JetStream discards older revisions.
func (a *App) collaborationHistory(ctx context.Context) ([]jetstream.KeyValueEntry, error) {
	entries, err := a.kv.History(ctx, "prototype.update")
	if err == jetstream.ErrKeyNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Revision() < entries[j].Revision() })
	return entries, nil
}
func collaborationDocument(entries []jetstream.KeyValueEntry, n int) CollabDemoDocument {
	d := CollabdummyDocument()
	if n > 0 && n <= len(entries) {
		var cells map[string]int64
		if json.Unmarshal(entries[n-1].Value(), &cells) == nil {
			d.setCells(cells)
		}
	}
	return d
}
func (a *App) collaborationPage(w http.ResponseWriter, r *http.Request) {
	entries, err := a.collaborationHistory(r.Context())
	if err != nil {
		http.Error(w, "History unavailable", 500)
		return
	}
	CollabPage(collaborationDocument(entries, len(entries)), a.collaboration.clients.Load(), len(entries)).Render(r.Context(), w)
}
func (a *App) collaborationPost(w http.ResponseWriter, r *http.Request) {
	var cells map[string]int64
	if err := datastar.ReadSignals(r, &cells); err != nil {
		http.Error(w, "Invalid cells", 400)
		return
	}
	a.collaboration.mu.Lock()
	defer a.collaboration.mu.Unlock()
	entries, err := a.collaborationHistory(r.Context())
	if err != nil {
		http.Error(w, "History unavailable", 500)
		return
	}
	d := collaborationDocument(entries, len(entries))
	changed := d.diffCells(cells)
	if len(changed) == 0 {
		datastar.NewSSE(w, r).PatchElementTempl(CollabDemo(d, nil, 0), datastar.WithModeReplace())
		return
	}
	d.setCells(changed)
	data, err := json.Marshal(CollabcellSignals(d))
	if err != nil {
		http.Error(w, "Could not encode cells", 500)
		return
	}
	rev, err := a.kv.Put(r.Context(), "prototype.update", data)
	if err != nil {
		http.Error(w, "Could not save cells", 500)
		return
	}
	sse := datastar.NewSSE(w, r)
	sse.MarshalAndPatchSignals(map[string]int{"history": min(len(entries)+1, 64), "scrub": min(len(entries)+1, 64)})
	sse.PatchElementTempl(CollabDemo(d, changed, rev), datastar.WithModeReplace())
}
func (a *App) collaborationEdit(w http.ResponseWriter, r *http.Request) {
	entries, err := a.collaborationHistory(r.Context())
	if err != nil {
		http.Error(w, "History unavailable", 500)
		return
	}
	datastar.NewSSE(w, r).PatchElementTempl(CollabDemoEdit(collaborationDocument(entries, len(entries))))
}
func (a *App) collaborationCancel(w http.ResponseWriter, r *http.Request) {
	entries, err := a.collaborationHistory(r.Context())
	if err != nil {
		http.Error(w, "History unavailable", 500)
		return
	}
	d := collaborationDocument(entries, len(entries))
	sse := datastar.NewSSE(w, r)
	sse.MarshalAndPatchSignals(CollabcellSignals(d))
	sse.PatchElementTempl(CollabDemo(d, nil, 0), datastar.WithModeReplace())
}
func (a *App) collaborationWatch(w http.ResponseWriter, r *http.Request) {
	watcher, err := a.kv.Watch(r.Context(), "prototype.*", jetstream.UpdatesOnly())
	if err != nil {
		http.Error(w, "Live updates unavailable", 500)
		return
	}
	defer watcher.Stop()
	sse := datastar.NewSSE(w, r)
	a.collaboration.clients.Add(1)
	a.kv.Put(r.Context(), "prototype.clients", nil)
	defer func() {
		a.collaboration.clients.Add(-1)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		a.kv.Put(ctx, "prototype.clients", nil)
	}()
	sse.MarshalAndPatchSignals(map[string]bool{"open": a.collaboration.editing.Load()})
	// Refresh after subscribing to cover updates during page load.
	entries, err := a.collaborationHistory(r.Context())
	if err != nil {
		return
	}
	d := collaborationDocument(entries, len(entries))
	sse.MarshalAndPatchSignals(CollabcellSignals(d))
	sse.MarshalAndPatchSignals(map[string]int{"history": len(entries), "scrub": len(entries)})
	if sse.PatchElementTempl(CollabDemo(d, nil, 0)) != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				return
			}
			if entry == nil {
				continue
			}
			switch entry.Key() {
			case "prototype.clients":
				if sse.PatchElementTempl(CollabOnline(a.collaboration.clients.Load())) != nil {
					return
				}
			case "prototype.editing":
				sse.MarshalAndPatchSignals(map[string]bool{"open": a.collaboration.editing.Load()})
			case "prototype.update":
				entries, err := a.collaborationHistory(r.Context())
				if err != nil {
					return
				}
				// Find this event's predecessor, even if another edit has already arrived.
				idx := sort.Search(len(entries), func(i int) bool { return entries[i].Revision() >= entry.Revision() })
				if idx == len(entries) || entries[idx].Revision() != entry.Revision() {
					continue
				}
				prev := collaborationDocument(entries, idx)
				d := collaborationDocument(entries, idx+1)
				changed := prev.diffCells(CollabcellSignals(d))
				sse.MarshalAndPatchSignals(CollabcellSignals(d))
				sse.MarshalAndPatchSignals(map[string]int{"history": len(entries), "scrub": idx + 1})
				if sse.PatchElementTempl(CollabDemo(d, changed, entry.Revision())) != nil {
					return
				}
			}
		}
	}
}
func (a *App) collaborationScrub(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		Scrub int `json:"scrub"`
	}
	if datastar.ReadSignals(r, &sig) != nil {
		http.Error(w, "Invalid revision", 400)
		return
	}
	entries, err := a.collaborationHistory(r.Context())
	if err != nil {
		http.Error(w, "History unavailable", 500)
		return
	}
	n := max(0, min(sig.Scrub, len(entries)))
	d := collaborationDocument(entries, n)
	sse := datastar.NewSSE(w, r)
	sse.MarshalAndPatchSignals(CollabcellSignals(d))
	sse.PatchElementTempl(CollabDemo(d, nil, 0))
}
func (a *App) collaborationPlay(w http.ResponseWriter, r *http.Request) {
	entries, err := a.collaborationHistory(r.Context())
	if err != nil {
		http.Error(w, "History unavailable", 500)
		return
	}
	sse := datastar.NewSSE(w, r)
	prev := CollabdummyDocument()
	for i, e := range entries {
		d := collaborationDocument(entries, i+1)
		changed := prev.diffCells(CollabcellSignals(d))
		sse.MarshalAndPatchSignals(CollabcellSignals(d))
		if sse.PatchElementTempl(CollabDemo(d, changed, e.Revision())) != nil {
			return
		}
		sse.MarshalAndPatchSignals(map[string]int{"scrub": i + 1})
		prev = d
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
	}
}
func (a *App) collaborationSetEditing(open bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.collaboration.editing.Store(open)
		if _, err := a.kv.Put(r.Context(), "prototype.editing", nil); err != nil {
			http.Error(w, "Could not update prototype", 500)
		}
	}
}
func (a *App) collaborationQR(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	png, err := qrcode.Encode(scheme+"://"+r.Host+"/collaboration-demo", qrcode.Medium, 512)
	if err != nil {
		http.Error(w, "Could not create QR code", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}
