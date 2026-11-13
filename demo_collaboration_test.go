package main

import (
	"context"
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
	for _, body := range []string{`{"r0c0":123}`, `{"r1c0":456}`} {
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
	app.collaborationSetEditing(true)(w, httptest.NewRequest("POST", "/collaboration-demo/start", nil))
	if w.Code != 200 || !app.collaboration.editing.Load() {
		t.Fatal("start failed")
	}
	app.collaborationPost(httptest.NewRecorder(), httptest.NewRequest("POST", "/collaboration-demo", strings.NewReader(`{"r0c0":999}`)))
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
	app.collaborationSetEditing(false)(httptest.NewRecorder(), httptest.NewRequest("POST", "/collaboration-demo/stop", nil))
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
