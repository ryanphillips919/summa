package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCollaborationPrototypeHistoryAndIsolation(t *testing.T) {
	app := newNorthstarTestApp(t)
	ns, nc, err := startEmbeddedNATS(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownEmbeddedNATS(nc, ns) })
	app.kv, err = StartKV(nc)
	if err != nil {
		t.Fatal(err)
	}
	before, err := app.monthlyDocument(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	router := newRouter(app)
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest("GET", "/collaboration-demo", nil))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "simulated data") || !strings.Contains(page.Body.String(), "Revenue: Product Sales") {
		t.Fatalf("prototype page: %d %s", page.Code, page.Body.String())
	}
	for _, body := range []string{`{"admin":"gradmin","editCell":"r0c0","editValue":123}`, `{"admin":"gradmin","editCell":"r1c0","editValue":456}`} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/collaboration-demo", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
	}
	entries, err := app.collaborationHistory(context.Background())
	if err != nil || len(entries) != 2 {
		t.Fatalf("history: %d %v", len(entries), err)
	}
	first := CollabcellSignals(collaborationDocument(entries, 1))
	last := CollabcellSignals(collaborationDocument(entries, 2))
	if first["r0c0"] != 123 || last["r0c0"] != 123 || last["r1c0"] != 456 {
		t.Fatalf("history snapshots: %v %v", first, last)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/collaboration-demo/scrub?datastar="+url.QueryEscape(`{"scrub":1}`), nil))
	if !strings.Contains(w.Body.String(), `"r0c0":123`) || strings.Contains(w.Body.String(), `"r1c0":456`) {
		t.Fatalf("scrub response: %s", w.Body.String())
	}
	play := httptest.NewRecorder()
	router.ServeHTTP(play, httptest.NewRequest("GET", "/collaboration-demo/play", nil))
	firstEdit := strings.Index(play.Body.String(), `"r0c0":123`)
	secondEdit := strings.Index(play.Body.String(), `"r1c0":456`)
	if firstEdit < 0 || secondEdit <= firstEdit {
		t.Fatalf("playback order: %s", play.Body.String())
	}
	after, err := app.monthlyDocument(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("prototype edits changed accounting ledger")
	}
	qr := httptest.NewRecorder()
	router.ServeHTTP(qr, httptest.NewRequest(http.MethodGet, "http://example.com/collaboration-demo/qr", nil))
	if qr.Code != 200 || qr.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("QR: %d %s", qr.Code, qr.Header().Get("Content-Type"))
	}
}

func TestCollaborationLiveUpdatesAndControls(t *testing.T) {
	ns, nc, err := startEmbeddedNATS(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownEmbeddedNATS(nc, ns) })
	kv, err := StartKV(nc)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{kv: kv}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streams := make([]*streamRecorder, 2)
	done := make(chan struct{}, 2)
	for i := range streams {
		streams[i] = &streamRecorder{header: make(http.Header), updates: make(chan string, 100)}
		go func(s *streamRecorder) {
			app.collaborationWatch(s, httptest.NewRequest("GET", "/collaboration-demo/watch", nil).WithContext(ctx))
			done <- struct{}{}
		}(streams[i])
	}
	deadline := time.After(5 * time.Second)
	for app.collaboration.clients.Load() != 2 {
		select {
		case <-deadline:
			t.Fatal("viewers did not connect")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	w := httptest.NewRecorder()
	app.collaborationSetEditing(true)(w, httptest.NewRequest("POST", "/collaboration-demo/start", strings.NewReader(`{"admin":"gradmin"}`)))
	if w.Code != 200 || !app.collaboration.editing.Load() {
		t.Fatal("start failed")
	}
	app.collaborationPost(httptest.NewRecorder(), httptest.NewRequest("POST", "/collaboration-demo", strings.NewReader(`{"editCell":"r0c0","editValue":999}`)))
	for _, stream := range streams {
		timeout := time.After(5 * time.Second)
		found := false
		for !found {
			select {
			case output := <-stream.updates:
				found = strings.Contains(output, `"r0c0":999`) && strings.Contains(output, "2 online")
			case <-timeout:
				t.Fatal("viewer did not receive edit and viewer count")
			}
		}
	}
	app.collaborationSetEditing(false)(httptest.NewRecorder(), httptest.NewRequest("POST", "/collaboration-demo/stop", strings.NewReader(`{"admin":"gradmin"}`)))
	if app.collaboration.editing.Load() {
		t.Fatal("stop failed")
	}
	cancel()
	for range streams {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("viewer did not disconnect")
		}
	}
	if app.collaboration.clients.Load() != 0 {
		t.Fatal("viewer count did not clear")
	}
}

func newCollaborationTestApp(t *testing.T) *App {
	t.Helper()
	ns, nc, err := startEmbeddedNATS(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownEmbeddedNATS(nc, ns) })
	kv, err := StartKV(nc)
	if err != nil {
		t.Fatal(err)
	}
	return &App{kv: kv}
}

// collaborationTestSave posts one cell edit, as typing in a cell does.
func collaborationTestSave(t *testing.T, app *App, cell string, value any, admin string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"editCell": cell, "editValue": value, "admin": admin})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	app.collaborationPost(w, httptest.NewRequest("POST", "/collaboration-demo", strings.NewReader(string(body))))
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func collaborationTestLatest(t *testing.T, app *App) map[string]int64 {
	t.Helper()
	entries, err := app.collaborationHistory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return CollabcellSignals(collaborationDocument(entries, len(entries)))
}

// Regression: one person saving used to re-render the table for everyone, closing any
// cell someone else was typing in. Updates must now arrive as signals only, and two
// people typing in different cells must both keep their edits.
func TestCollaborationLiveEditsKeepOtherEditorsTyping(t *testing.T) {
	app := newCollaborationTestApp(t)
	app.collaboration.editing.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	viewer := &streamRecorder{header: make(http.Header), updates: make(chan string, 100)}
	go app.collaborationWatch(viewer, httptest.NewRequest("GET", "/collaboration-demo/watch", nil).WithContext(ctx))
	for deadline := time.Now().Add(5 * time.Second); app.collaboration.clients.Load() != 1; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("viewer did not connect")
		}
	}

	collaborationTestSave(t, app, "r0c0", 111, "")
	collaborationTestSave(t, app, "r1c1", "222", "")
	got := collaborationTestLatest(t, app)
	if got["r0c0"] != 111 || got["r1c1"] != 222 {
		t.Fatalf("latest cells r0c0=%d r1c1=%d, want 111 and 222", got["r0c0"], got["r1c1"])
	}

	var seen strings.Builder
	timeout := time.After(5 * time.Second)
	for !strings.Contains(seen.String(), `"r1c1":222`) {
		select {
		case output := <-viewer.updates:
			seen.WriteString(output)
		case <-timeout:
			t.Fatalf("viewer did not receive both edits: %s", seen.String())
		}
	}
	if !strings.Contains(seen.String(), `"flash":{`) {
		t.Errorf("live update has no flash signals: %s", seen.String())
	}
	if strings.Contains(seen.String(), `id="demo"`) {
		t.Error("live update re-rendered the table, which would close cells other people are typing in")
	}
}

func TestCollaborationIgnoresBlankAndUnknownCells(t *testing.T) {
	app := newCollaborationTestApp(t)
	app.collaboration.editing.Store(true)
	before := collaborationTestLatest(t, app)
	collaborationTestSave(t, app, "r0c0", "", "")
	collaborationTestSave(t, app, "r999c0", 5, "")
	collaborationTestSave(t, app, "", 5, "")
	if entries, _ := app.collaborationHistory(context.Background()); len(entries) != 0 {
		t.Fatalf("blank or unknown cells created %d revisions", len(entries))
	}
	collaborationTestSave(t, app, "r0c0", 12.6, "")
	if got := collaborationTestLatest(t, app); got["r0c0"] != 13 || got["r1c0"] != before["r1c0"] {
		t.Fatalf("decimal save: r0c0=%d r1c0=%d", got["r0c0"], got["r1c0"])
	}
}

func TestCollaborationSaveRequiresOpenSessionOrPresenter(t *testing.T) {
	app := newCollaborationTestApp(t)
	if body := collaborationTestSave(t, app, "r0c0", 1, "wrong"); !strings.Contains(body, "Editing is locked") {
		t.Fatalf("locked save response: %s", body)
	}
	if collaborationTestLatest(t, app)["r0c0"] == 1 {
		t.Fatal("locked save was stored")
	}
	collaborationTestSave(t, app, "r0c0", 1, "gradmin")
	if collaborationTestLatest(t, app)["r0c0"] != 1 {
		t.Fatal("presenter save was not stored")
	}

	w := httptest.NewRecorder()
	app.collaborationSetEditing(true)(w, httptest.NewRequest("POST", "/collaboration-demo/start", strings.NewReader(`{"admin":"wrong"}`)))
	if w.Code != http.StatusForbidden || app.collaboration.editing.Load() {
		t.Fatalf("start with wrong code: %d, editing=%v", w.Code, app.collaboration.editing.Load())
	}
	for code, want := range map[string]string{"gradmin": `"_unlocked":true`, "nope": `"_unlocked":false`} {
		w := httptest.NewRecorder()
		app.collaborationUnlock(w, httptest.NewRequest("POST", "/collaboration-demo/unlock", strings.NewReader(`{"admin":"`+code+`"}`)))
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("unlock %q: %s", code, w.Body.String())
		}
	}
}

func TestCollaborationCells(t *testing.T) {
	got, err := collaborationCells(map[string]any{"blank": "", "missing": nil, "decimal": 12.6, "typed": "1,234.4", "negative": -3.5})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"decimal": 13, "typed": 1234, "negative": -4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cells = %v, want %v", got, want)
	}
	for _, bad := range []any{"abc", true, 1e15} {
		if _, err := collaborationCells(map[string]any{"r0c0": bad}); err == nil {
			t.Errorf("collaborationCells accepted %v", bad)
		}
	}
}
