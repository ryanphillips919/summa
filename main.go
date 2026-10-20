package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"summa/internal/analytics"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	// start up our app by starting the NATS server, KV storage, TODO sqlite database and associated queries
	app, cleanup, err := appInit()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()
	r := chi.NewRouter()
	// A good base middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromRemoteAddr) // pick one ClientIPFrom* based on your infra, see below
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Set a timeout value on the request context (ctx), that will signal
	// through ctx.Done() that the request has timed out and further
	// processing should be stopped.
	r.Use(middleware.Timeout(60 * time.Second))
	// serve static css/javascript files using this line
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))
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

	// instead of writing routes directly into main.go, write them
	// inside of a given file, ie the routes for /demo is in demo.go
	r.Mount("/demo", app.demoRoutes())

	// Start HTTP server
	log.Println("Summa running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// this is a really useful way to pass around different parts of the system without needing to include those values as function arguments
// especially around database operations. You can think of it as an Class like in Java, though a bit simpler
type App struct {
	nc *nats.Conn
	kv jetstream.KeyValue
}

func appInit() (*App, func(), error) {
	// this function is to actually set up the NATS server and a JetStream bucket
	// look at nats.go for more information about what NATS is doing here/
	// how it's actually set up, but we'll make heavy use of NATS in our routes
	nc, ns, err := StartEmbeddedServer(true, true)
	if err != nil {
		return nil, nil, fmt.Errorf("starting nats: %w", err)
	}
	kv, err := StartKV(nc)
	if err != nil {
		nc.Close()
		ns.Shutdown()
		return nil, nil, fmt.Errorf("starting kv: %w", err)
	}
	// we need this because if our main server goes down/disconnects, NATS won't automatically shut down as well, which can
	// create some big problems on the ec2 instance otherwise.
	cleanup := func() {
		nc.Drain()
		ns.Shutdown()
		ns.WaitForShutdown()
	}

	return &App{nc: nc, kv: kv}, cleanup, nil
}
