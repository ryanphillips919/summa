# AGENTS.md

Guidance for AI coding agents (Claude, Codex, etc.) working in this repository. These rules come from the code review after the interactive demo (`review.org`), which has the full reasoning and longer examples.

## Stack

Go, SQLite, SQLC (generated queries and types in package `db`), chi (routing), templ (views), Datastar (front-end reactivity over SSE).

## Core principle

The database is the source of truth. If SQLC already generated a type or a query, use it directly. Do not rewrite it, wrap it, or mirror it in a hand-written struct.

## Database

- **Need a struct type? Check the schema first.** If the shape exists in the database, use the SQLC-generated type. If it doesn't, add it to the schema or queries, regenerate, and use the generated type. Do not hand-write a parallel struct.
- **Call generated queries directly from handlers.** Do not add service or repository wrapper functions (e.g. a `PostInTx` that only sequences `CreateTransaction` and `CreateEntry`). The handler is the right place for that sequencing.
- **Put validation in the schema.** Use `CHECK`, `NOT NULL`, foreign keys, and similar constraints, and let SQLite's error come back through the SQLC call. Do not duplicate in Go a check the database already enforces.
- **Exception:** security-sensitive input (login, sign-up, and similar) is still validated explicitly in Go.
- **Prefer SQL for shaping data.** If a page needs joined, aggregated, or ranked data, write a query (joins, window functions) so SQLC generates the type, instead of assembling it in Go.

## Routes

- The repository root contains only `main.go` and routing files, one per feature (`invoices.go`, `login.go`, `homepage.go`, `settings.go`, ...). Templ code goes in `views/`, tests in `tests/`. Do not add other kinds of code to the root.
- Each feature file starts with one function that lists every route for that feature and returns a `chi.Router`. Its handlers follow in the same file:

  ```go
  func (a *App) exampleRoutes() chi.Router {
      r := chi.NewRouter()
      r.Get("/", a.exampleHandler)
      r.Post("/", a.examplePostHandler)
      r.Get("/watch", a.exampleWatcherHandler)
      return r
  }
  ```

- Handlers are short: read input, call the generated query, render a view. A GET handler is often under ten lines.
- `main.go` stays simple: create the router, add middleware, `r.Mount("/feature", app.featureRoutes())` for each feature, start the server. Do not add a separate function just to mount routes.

## Views

- Templ components take SQLC-generated types as arguments, e.g. `templ BigGetPage(u db.Users, b []db.Balance, txn []db.Transaction)`. Do not create view-model structs to pass data into templates.
- Rendering helpers (formatting amounts, building display strings) live in the `.templ` file that uses them. When there are enough of them, move them to `views/views_helper.go` in `package views`.
- Simple display formatting (money, basic string manipulation) may be done on the front end with Datastar where that is simpler.
- Selection and filtering for display (e.g. "last three transactions per user") can be done in the templ itself.

## Helper structs

Default: do not create them. Most of the time they are unnecessary.

Before adding one, in order:

1. Can this be a SQL query or window function, so SQLC generates the type? Do that.
2. Can templ take the generated types and do the selection while rendering? Do that.
3. Only if neither fits: use an anonymous struct declared inside the handler. Never a named package-level helper type.

If anonymous structs start showing up in many handlers, stop and move the shape into the database.

## Before you finish a change

- No new hand-written struct duplicates a table or query result.
- No new function exists only to wrap generated SQLC calls.
- No Go validation repeats a database constraint (outside auth flows).
- No non-routing code was added to the repository root.
- `main.go` is still just middleware, mounts, and server start.
