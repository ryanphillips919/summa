# Summa

## CI and deployment

Linux is Summa's primary testing environment and planned deployment platform.

GitHub Actions runs automated formatting, testing, and build checks on Ubuntu. GitLab CI is configured to run equivalent checks in a Debian-based Go container using our self-hosted Docker runner.

Developers using Windows can work through WSL or Docker.

## Ledger rules

All code that handles money must follow [docs/ledger-rules.md](docs/ledger-rules.md) (double-entry, append-only, derived balances).

## Team
- David Kendall
- Ryan Phillips
- Graham Taggart
- Sawyer Evans

## Status
CS 4000 Capstone project, Fall 2026.

## Installation for dependencies 
```sh
# use this on to install templ onto your path
go install github.com/a-h/templ/cmd/templ@latest
# or install locally to the project if you don't want templ on your path
go get -tool github.com/a-h/templ/cmd/templ@latest 
# for the actual dependencies that aren't the standard lib
go get -u github.com/starfederation/datastar-go modernc.org/sqlite github.com/go-chi/chi/v5 github.com/nats-io/nats.go@latest github.com/nats-io/nats-server/v2@latest
# sqlc and goose
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
```


## Generating and running the code
For the templ code.
```sh
templ generate # whenever you change a templ file you need to run this to apply the changes. templ is compiled into Go code so without it the pages won't update
# otherwise for local development you can use this to watch all the changes and apply in real-time
templ generate --watch --proxy="http://localhost:8080" --cmd="go run ."
```

For the sqlc and goose migrations
```sh
sqlc generate # this generates the sql into Golang code. Whenever you update your SQL commands/queries run sqlc generate.
```

Goose is for our database migrations, and creating the tables.
Whenenver you need to alter a table/create a new table you'll need to run the below command. Always add the -s flag because it
generates the sql file as 00000_some_database_manipulation.sql, and goose migrates the database in lexicographical order.
```sh
goose -s create some_database_manipulation sql
# to run migrations themselves
goose up
# when you need to downgrade
goose down
```

For the server itself for local testing use go run ., you can also use go run main.go if you feel more comfortable that way.
```sh
go run . # basically says run this code at the root directory, so with chi it will start up the server and print the localhost to open the server and see
```

In the CI/CD pipeline there will be some commands that Go comes built in that are really useful for formatting, linting, testing etc.
```sh
go fmt ./.. # format your golang code in all directories
go test . # test all the test files. It's nice because we can have a single test.go file and Go knows how to run the tests, and will exclude them in the build
go build . # build this into a Golang binary for deployment/this is what Docker will build for us for the VM.
```


## Northstar development data

On a fresh SQLite database, Summa seeds **Northstar Equipment & Supply**, a
fictional equipment distributor. It creates 12 customers, 16 accounts, 20
invoices (18 posted and 2 drafts), and 99 balanced ledger transactions dated
May through October 2026. Each invoice has two lines. The seed uses the invoice
posting and ledger services; re-opening an existing database does not reseed it.
If seeding is interrupted, use a fresh development database; seeding does not
currently resume a partially created dataset.

To start with a new development database without touching an existing one:

```sh
SUMMA_DB=data/northstar.db go run .
```

Choose a path that does not already exist. Local SQLite databases and NATS
state are stored in `data/` and must not be committed. If you intentionally
reset a database, its local data is lost.

The initial account balances, in dollars, are:

| Account | Balance |
| --- | ---: |
| Cash | $111,550.02 |
| Accounts Receivable | $54,750.00 |
| Inventory | $36,000.00 |
| Accounts Payable | $36,000.00 |
| Loan Payable | $71,000.00 |
| Sales Revenue | $164,750.00 |
| Total expenses | $279,449.98 |
| Net income | -$114,699.98 |

Customer receipts are balanced ledger entries. The schema does not yet
allocate them to individual invoices, so invoice pages show **Draft** or
**Posted** rather than claiming that an invoice is paid or outstanding.
The monthly ledger is read-only; use the general journal for balanced manual
adjustments. After a journal adjustment or invoice posting, NATS notifies open
monthly-ledger and timeline views to reload their data from SQLite.

## Analytics

The analytics dashboard reads organization-scoped cash entries from SQLite.
Posting an invoice changes receivables and revenue, not cash. The engine uses
up to 60 days of recorded cash movements and the earlier cash balance as its
opening balance. The JSON demo fixture remains available for isolated tests.

For local `go run .`, install the Python requirements into a project virtual
environment:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install -r analytics/requirements.txt
go run .
```

The Go client uses `.venv` by default. Set `SUMMA_ANALYTICS_PYTHON` or
`SUMMA_ANALYTICS_SCRIPT` to use different paths. `GET /api/analytics` uses the
ledger. The separate `POST /api/analytics/run` endpoint accepts supplied cash
movements for isolated engine testing and does not alter the ledger.

The Go client passes only the selected cash movements to `analytics/engine.py`
over stdin; Python has no database credentials and is stopped after 10 seconds.
The engine supports 1-90 day forecasts, compares a rolling-mean baseline with
Holt-Winters when enough data is available, and flags unusual amounts and
possible duplicates for review. These flags do not change accounting records.

The isolated endpoint can also be exercised directly:

```json
{
  "forecast_days": 30,
  "opening_cash_balance": 4500,
  "cash_movements": [
    {"id": "txn-1", "date": "2026-10-01", "amount": 1200, "description": "Customer receipt"},
    {"id": "txn-2", "date": "2026-10-02", "amount": -185, "vendor": "Example Hosting", "description": "Hosting"}
  ]
}
```

After changing SQL queries or templates, regenerate and verify:

```sh
sqlc generate
go tool templ generate
go test ./...
go vet ./...
go build ./...
```
