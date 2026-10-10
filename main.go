package main

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Start NATS
	natsServer, natsConn, err := startEmbeddedNATS()
	if err != nil {
		log.Fatal(err)
	}

	defer natsServer.Shutdown()
	defer natsConn.Close()

	log.Println("Embedded NATS ready")

	// Start HTTP server
	router := newRouter()

	log.Println("Summa running on http://localhost:8080")

	if err := http.ListenAndServe(":8080", router); err != nil {
		log.Printf("HTTP server stopped: %v", err)
	}
}

func newRouter() http.Handler {
	router := chi.NewRouter()

	// Middleware
	router.Use(middleware.RequestID)
	router.Use(middleware.ClientIPFromRemoteAddr)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(60 * time.Second))

	// Routes
	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hi"))
	})

	return router
}