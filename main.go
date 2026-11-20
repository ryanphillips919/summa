package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"summa/internal/analytics"
	"summa/internal/db"
	"summa/views"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

type App struct {
	nc             *nats.Conn
	kv             jetstream.KeyValue
	sqlDB          *sql.DB
	queries        *db.Queries
	orgID          int64
	cashDataSource analytics.CashDataSource
}

func main() {
	app, cleanup, err := appInit()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromRemoteAddr)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/presentation", templ.Handler(views.Presentation()).ServeHTTP)
	r.Mount("/demo", app.demoRoutes())
	r.Mount("/timeline", app.timelineRoutes())

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))

		r.Get("/health", healthHandler)
		r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))

		r.Mount("/", app.homepageRoutes())
		r.Mount("/analytics", app.analyticsRoutes())
		r.Mount("/api/analytics", app.analyticsAPIRoutes())
	})

	csrf := http.NewCrossOriginProtection()
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           csrf.Handler(r),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Println("http://localhost:8080")
	log.Fatal(srv.ListenAndServe())
}

func appInit() (*App, func(), error) {
	nc, ns, err := startEmbeddedNATS(true, true)
	if err != nil {
		return nil, nil, fmt.Errorf("starting nats: %w", err)
	}

	kv, err := StartKV(nc)
	if err != nil {
		shutdownEmbeddedNATS(nc, ns)
		return nil, nil, fmt.Errorf("starting kv: %w", err)
	}

	sqldb, err := sql.Open("sqlite", "file:data/summa.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		shutdownEmbeddedNATS(nc, ns)
		return nil, nil, fmt.Errorf("opening db: %w", err)
	}

	cleanup := func() {
		sqldb.Close()
		shutdownEmbeddedNATS(nc, ns)
	}

	ctx := context.Background()
	if err := migrate(ctx, sqldb); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("migrating: %w", err)
	}

	store := db.New(sqldb)
	orgID, err := seedDevData(ctx, sqldb, store)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("seeding db: %w", err)
	}

	cashDataSource, err := analytics.NewCashDataSource(sqldb, orgID)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("analytics source: %w", err)
	}

	return &App{nc: nc, kv: kv, sqlDB: sqldb, queries: store, orgID: orgID, cashDataSource: cashDataSource}, cleanup, nil
}

//go:embed internal/db/migrations/*.sql
var migrations embed.FS

func migrate(ctx context.Context, sqldb *sql.DB) error {
	files, err := fs.Sub(migrations, "internal/db/migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqldb, files)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"summa"}`))
}
