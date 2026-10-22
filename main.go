package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"summa/internal/analytics"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	app, cleanup, err := appInit()
	if err != nil {
		return err
	}
	defer cleanup()

	log.Println("Embedded NATS ready")
	log.Println("Summa running on http://localhost:8080")
	return http.ListenAndServe(":8080", newRouter(app))
}

func newRouter(app *App) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromRemoteAddr)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))
	r.Handle("/analytics/demo/*", http.StripPrefix("/analytics/demo", http.FileServer(http.Dir("analytics/demo"))))
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Summa is running. POST cash movements to /api/analytics/run."))
	})

	r.Post("/api/analytics/run", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var request analytics.Request
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid analytics request: "+err.Error())
			return
		}
		result, err := analytics.Run(r.Context(), request)
		if err != nil {
			log.Printf("analytics request failed: %v", err)
			writeJSONError(w, http.StatusUnprocessableEntity, "analytics could not process these cash movements")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})

	r.Get("/api/analytics/demo", demoAnalyticsHandler)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"summa"}`))
	})

	r.Mount("/demo", app.demoRoutes())
	return r
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

type analyticsDemoResponse struct {
	analytics.Response
	CashMovements      []analytics.CashMovement `json:"cash_movements"`
	OpeningCashBalance float64                  `json:"opening_cash_balance"`
}

func demoAnalyticsHandler(w http.ResponseWriter, r *http.Request) {
	fixture, err := os.ReadFile("analytics/data/demo_cash_movements.json")
	if err != nil {
		log.Printf("read analytics demo fixture: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "analytics demo data is unavailable")
		return
	}

	var request analytics.Request
	if err := json.Unmarshal(fixture, &request); err != nil {
		log.Printf("decode analytics demo fixture: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "analytics demo data is invalid")
		return
	}

	result, err := analytics.Run(r.Context(), request)
	if err != nil {
		log.Printf("run analytics demo: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "analytics demo could not run")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(analyticsDemoResponse{
		Response:           result,
		CashMovements:      request.CashMovements,
		OpeningCashBalance: request.OpeningCashBalance,
	})
}

type App struct {
	nc *nats.Conn
	kv jetstream.KeyValue
}

func appInit() (*App, func(), error) {
	ns, nc, err := startEmbeddedNATS("./data/js", true)
	if err != nil {
		return nil, nil, fmt.Errorf("starting NATS: %w", err)
	}

	kv, err := StartKV(nc)
	if err != nil {
		shutdownEmbeddedNATS(nc, ns)
		return nil, nil, fmt.Errorf("starting KV: %w", err)
	}

	cleanup := func() {
		shutdownEmbeddedNATS(nc, ns)
	}
	return &App{nc: nc, kv: kv}, cleanup, nil
}
