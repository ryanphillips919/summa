# Summa

## CI and deployment

Linux is Summa's primary testing environment and planned deployment platform.

GitHub Actions runs automated formatting, testing, and build checks on Ubuntu. GitLab CI is configured to run equivalent checks in a Debian-based Go container using our self-hosted Docker runner.

Developers using Windows can work through WSL or Docker.

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


## Analytics starter

Go owns authorization and database access. After it loads organization-scoped
cash movements, it sends only that data to `analytics/engine.py` over stdin as
JSON. Python returns JSON for a templ component or Datastar update and has no
database credentials. The Go client stops the subprocess after 10 seconds.

`POST /api/analytics/run` accepts signed cash movements (inflows positive and
expenses negative):

```json
{
  "forecast_days": 60,
  "cash_movements": [
    {"id": "txn-1", "date": "2026-10-01", "amount": 1200, "description": "Invoice payment"},
    {"id": "txn-2", "date": "2026-10-02", "amount": -185, "vendor": "AWS", "description": "Hosting"}
  ]
}
```

The engine supports 1-90 day forecasts. It backtests a 7-day rolling-mean
baseline against a linear trend with weekday seasonality using rolling-origin
MAE, then returns the better model and both error values. It flags expense
amount outliers using median absolute deviation (threshold `3.5`) and potential
duplicates matching date, amount, and normalized vendor or description. These
are review signals, not accounting advice. Show the selected method and interval
in the UI and let a bookkeeper mark a flag reviewed without altering the ledger.

Use Apache ECharts for the first dashboard: plot observed daily cash flow, the
selected forecast, a confidence band, and a linked anomaly-review table. D3 is
better reserved for a bespoke ledger-flow or relationship visualization.

Run the integration test with `go test ./...`. The container includes Python 3.
Set `SUMMA_ANALYTICS_PYTHON` or `SUMMA_ANALYTICS_SCRIPT` when non-default paths
are required.