package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
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
	r.Post("/unlock", a.collaborationUnlock)
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

// collaborationSignals is everything a browser needs to show document d: every cell
// value, plus which cells and EBITDA columns changed in revision rev so they flash.
// Updates are sent as signals only, so the table markup (and any cell someone is
// typing in) is never replaced.
func collaborationSignals(d CollabDemoDocument, changed map[string]int64, rev uint64) map[string]any {
	signals := map[string]any{"rev": rev}
	for key, value := range CollabcellSignals(d) {
		signals[key] = value
	}
	flash := map[string]uint64{}
	for key := range changed {
		flash[key] = rev
	}
	for col := range CollabchangedCols(changed) {
		flash[fmt.Sprintf("c%d", col)] = rev
	}
	signals["flash"] = flash
	return signals
}

// collaborationCellEdit is one cell being typed in: which cell, what's in the box, and the
// presenter code if they have it.
type collaborationCellEdit struct {
	EditCell  string `json:"editCell"`
	EditValue any    `json:"editValue"`
	Admin     string `json:"admin"`
}

// collaborationPost saves the one cell someone is typing in. Each pause in typing sends
// it, so other people see the edit as it happens; edits to other cells are untouched.
// Two people typing in the same cell: the last one to pause wins.
func (a *App) collaborationPost(w http.ResponseWriter, r *http.Request) {
	var edit collaborationCellEdit
	if err := datastar.ReadSignals(r, &edit); err != nil {
		http.Error(w, "Invalid cell", 400)
		return
	}
	sse := datastar.NewSSE(w, r)
	if !a.collaboration.editing.Load() && !collaborationPresenter(edit.Admin) {
		sse.MarshalAndPatchSignals(map[string]any{"_saveError": "Editing is locked. Ask the presenter to start the session.", "editCell": ""})
		return
	}
	cells, err := collaborationCells(map[string]any{edit.EditCell: edit.EditValue})
	if err != nil {
		sse.MarshalAndPatchSignals(map[string]any{"_saveError": err.Error()})
		return
	}
	a.collaboration.mu.Lock()
	defer a.collaboration.mu.Unlock()
	entries, err := a.collaborationHistory(r.Context())
	if err != nil {
		sse.MarshalAndPatchSignals(map[string]any{"_saveError": "History unavailable. Try again."})
		return
	}
	d := collaborationDocument(entries, len(entries))
	// diffCells ignores names that aren't cells in the document
	changed := d.diffCells(cells)
	if len(changed) == 0 {
		sse.MarshalAndPatchSignals(map[string]any{"_saveError": ""})
		return
	}
	d.setCells(changed)
	data, err := json.Marshal(CollabcellSignals(d))
	if err != nil {
		sse.MarshalAndPatchSignals(map[string]any{"_saveError": "Could not encode cells."})
		return
	}
	if _, err := a.kv.Put(r.Context(), "prototype.update", data); err != nil {
		sse.MarshalAndPatchSignals(map[string]any{"_saveError": "Could not save the cell. Try again."})
		return
	}
	// the watch stream delivers the new values to everyone, including this browser
	sse.MarshalAndPatchSignals(map[string]any{"_saveError": ""})
}

// collaborationCells turns typed cell values into whole dollars. A blank input leaves
// the cell unchanged and decimals round to the nearest dollar.
func collaborationCells(draft map[string]any) (map[string]int64, error) {
	cells := make(map[string]int64, len(draft))
	for key, value := range draft {
		var f float64
		switch v := value.(type) {
		case nil:
			continue
		case float64:
			f = v
		case string:
			text := strings.TrimSpace(strings.NewReplacer("$", "", ",", "").Replace(v))
			if text == "" {
				continue
			}
			parsed, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number.", v)
			}
			f = parsed
		default:
			return nil, fmt.Errorf("%v is not a number.", v)
		}
		if math.IsNaN(f) || math.Abs(f) >= 1e12 {
			return nil, fmt.Errorf("%v is not a usable amount.", value)
		}
		cells[key] = int64(math.Round(f))
	}
	return cells, nil
}

// collaborationPresenter reports whether code is the presenter code. Checking it here
// keeps the code out of the page source. SUMMA_PRESENTER_CODE overrides the default.
func collaborationPresenter(code string) bool {
	want := os.Getenv("SUMMA_PRESENTER_CODE")
	if want == "" {
		want = "gradmin"
	}
	return subtle.ConstantTimeCompare([]byte(code), []byte(want)) == 1
}

type collaborationAdmin struct {
	Admin string `json:"admin"`
}

// collaborationUnlock shows the presenter controls once the right code is typed.
func (a *App) collaborationUnlock(w http.ResponseWriter, r *http.Request) {
	var sig collaborationAdmin
	if datastar.ReadSignals(r, &sig) != nil {
		http.Error(w, "Invalid code", 400)
		return
	}
	datastar.NewSSE(w, r).MarshalAndPatchSignals(map[string]bool{"_unlocked": collaborationPresenter(sig.Admin)})
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
	sse.MarshalAndPatchSignals(map[string]int{"history": len(entries), "scrub": len(entries)})
	if sse.MarshalAndPatchSignals(collaborationSignals(d, nil, 0)) != nil {
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
				sse.MarshalAndPatchSignals(map[string]int{"history": len(entries), "scrub": idx + 1})
				if sse.MarshalAndPatchSignals(collaborationSignals(d, changed, entry.Revision())) != nil {
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
	sse := datastar.NewSSE(w, r)
	sse.MarshalAndPatchSignals(collaborationSignals(collaborationDocument(entries, n), nil, 0))
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
		if sse.MarshalAndPatchSignals(collaborationSignals(d, changed, e.Revision())) != nil {
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
		var sig collaborationAdmin
		if datastar.ReadSignals(r, &sig) != nil || !collaborationPresenter(sig.Admin) {
			http.Error(w, "Presenter code required", http.StatusForbidden)
			return
		}
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
