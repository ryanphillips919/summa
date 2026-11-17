package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"summa/internal/analytics"
	"summa/internal/db"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
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

	go app.warmAnalyticsCache()

	log.Println("Embedded NATS ready")
	address := listenAddress()
	log.Printf("Summa listening on %s", address)
	return http.ListenAndServe(address, newRouter(app))
}

func listenAddress() string {
	if address := os.Getenv("SUMMA_LISTEN_ADDR"); address != "" {
		return address
	}
	return "127.0.0.1:8080"
}

func newRouter(app *App) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromRemoteAddr)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Server-sent event streams (/demo/watch, /timeline/watch) stay open for as
	// long as a tab does, so the timeout only wraps ordinary requests.
	r.Get("/presentation", templ.Handler(Presentation()).ServeHTTP)
	r.Mount("/demo", app.demoRoutes())
	r.Mount("/collaboration-demo", app.collaborationRoutes())
	r.Mount("/timeline", app.timelineRoutes())

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))
		app.mountRoutes(r)
	})
	return r
}

// revalidate makes browsers check with the server before reusing a cached
// asset, so CSS/JS edits show up on every page without a hard refresh.
// Unchanged files still come back as a cheap 304 Not Modified.
func revalidate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (a *App) mountRoutes(r chi.Router) {
	r.Handle("/assets/*", revalidate(http.StripPrefix("/assets/", http.FileServer(http.Dir("assets")))))
	r.Get("/", a.homepageHandler)
	r.Get("/journal", a.journalHandler)
	r.Post("/journal", a.journalPostHandler)
	r.Post("/journal/{transactionID}/reverse", a.reverseJournalHandler)
	r.Get("/accounts", a.accountsHandler)
	r.Post("/accounts", a.createAccountHandler)
	r.Get("/reports/trial-balance", a.trialBalanceHandler)
	r.Get("/reports/profit-and-loss", a.incomeStatementHandler)
	r.Get("/reports/balance-sheet", a.balanceSheetHandler)
	r.Get("/reports/invoice-aging", a.invoiceAgingHandler)
	r.Get("/reports/bill-aging", a.billAgingHandler)
	r.Get("/reports/general-ledger", a.generalLedgerHandler)
	r.Get("/customers", a.customersHandler)
	r.Post("/customers", a.createCustomerHandler)
	r.Post("/customers/{customerID}", a.updateCustomerHandler)
	r.Get("/vendors", a.vendorsHandler)
	r.Post("/vendors", a.createVendorHandler)
	r.Post("/vendors/{vendorID}", a.updateVendorHandler)
	r.Get("/bills", a.billsHandler)
	r.Get("/bills/new", a.newBillHandler)
	r.Post("/bills", a.createBillHandler)
	r.Get("/bills/{billID}", a.billDetailHandler)
	r.Post("/bills/{billID}/payments", a.payBillHandler)
	r.Get("/invoices", a.invoicesHandler)
	r.Get("/invoices/new", a.newInvoiceHandler)
	r.Post("/invoices", a.createInvoiceHandler)
	r.Get("/invoices/{invoiceID}", a.invoiceDetailHandler)
	r.Post("/invoices/{invoiceID}", a.updateInvoiceHandler)
	r.Post("/invoices/{invoiceID}/lines", a.addInvoiceLineHandler)
	r.Post("/invoices/{invoiceID}/lines/{lineID}", a.updateInvoiceLineHandler)
	r.Post("/invoices/{invoiceID}/lines/{lineID}/delete", a.deleteInvoiceLineHandler)
	r.Post("/invoices/{invoiceID}/post", a.postInvoiceHandler)
	r.Post("/invoices/{invoiceID}/payments", a.recordInvoicePaymentHandler)
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

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"summa"}`))
	})

	r.Get("/api/analytics", a.analyticsHandler)
	r.Mount("/analytics", a.analyticsRoutes())
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

type App struct {
	kv             jetstream.KeyValue
	collaboration  collaborationState
	nc             *nats.Conn
	sqlDB          *sql.DB
	queries        *db.Queries
	orgID          int64 // one organization for now; see seedDevData
	cashDataSource analytics.CashDataSource
	analyticsCache analyticsCache
}

func appInit() (*App, func(), error) {
	ns, nc, err := startEmbeddedNATS("./data/js", true)
	if err != nil {
		return nil, nil, fmt.Errorf("starting NATS: %w", err)
	}

	kv, err := StartKV(nc)
	if err != nil {
		shutdownEmbeddedNATS(nc, ns)
		return nil, nil, fmt.Errorf("starting prototype KV: %w", err)
	}

	dbPath := os.Getenv("SUMMA_DB")
	usingDefaultDB := dbPath == ""
	if dbPath == "" {
		dbPath = filepath.Join("data", "summa.db")
	}
	ctx := context.Background()
	conn, err := openDatabase(ctx, dbPath)
	if usingDefaultDB && errors.Is(err, errLegacyDatabase) {
		legacyPath := dbPath
		dbPath = filepath.Join("data", "northstar.db")
		log.Printf("Database %s uses an older schema; keeping it separate and opening demo database %s", legacyPath, dbPath)
		conn, err = openDatabase(ctx, dbPath)
	}
	if err != nil {
		shutdownEmbeddedNATS(nc, ns)
		return nil, nil, err
	}
	queries := db.New(conn)
	orgID, err := seedDevData(ctx, conn, queries)
	if err != nil {
		conn.Close()
		shutdownEmbeddedNATS(nc, ns)
		return nil, nil, fmt.Errorf("seeding database: %w", err)
	}
	log.Printf("Database ready at %s", dbPath)

	cashDataSource, err := newAnalyticsDataSource(conn, orgID)
	if err != nil {
		conn.Close()
		shutdownEmbeddedNATS(nc, ns)
		return nil, nil, err
	}

	cleanup := func() {
		conn.Close()
		shutdownEmbeddedNATS(nc, ns)
	}
	return &App{
		nc:             nc,
		kv:             kv,
		sqlDB:          conn,
		queries:        queries,
		orgID:          orgID,
		cashDataSource: cashDataSource,
	}, cleanup, nil
}

var errLegacyDatabase = errors.New("incompatible older Summa schema")

func newAnalyticsDataSource(
	conn *sql.DB,
	orgID int64,
) (analytics.CashDataSource, error) {
	return analytics.DatabaseCashDataSource{
		DB: conn, OrganizationID: orgID, ForecastDays: 30, ObservationDays: 120,
	}, nil
}

func openDatabase(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	// foreign_keys makes SQLite enforce the schema's REFERENCES, and
	// _txlock=immediate makes concurrent postings queue instead of failing
	// with SQLITE_BUSY
	params := url.Values{
		"_pragma": {"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(5000)"},
		"_txlock": {"immediate"},
	}
	conn, err := sql.Open("sqlite", "file:"+path+"?"+params.Encode())
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	// Older Summa prototypes used different schemas under the same migration
	// versions. Never apply the current migrations to those databases: doing
	// so can leave a partially upgraded database or misinterpret its records.
	var legacyTransactions int
	if err := conn.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM pragma_table_info('transactions') WHERE name = 'occurred_on'
	) AND NOT EXISTS (
		SELECT 1 FROM pragma_table_info('transactions') WHERE name = 'date'
	)`).Scan(&legacyTransactions); err != nil {
		conn.Close()
		return nil, fmt.Errorf("inspect database schema: %w", err)
	}
	if legacyTransactions != 0 {
		conn.Close()
		return nil, fmt.Errorf("database %s uses an %w; preserve it and start a separate demo database with SUMMA_DB=data/northstar.db go run .", path, errLegacyDatabase)
	}
	if err := migrate(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

//go:embed internal/db/migrations/*.sql
var migrations embed.FS

// migrate applies any goose migrations the database hasn't run yet.
func migrate(ctx context.Context, conn *sql.DB) error {
	files, err := fs.Sub(migrations, "internal/db/migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, files)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
