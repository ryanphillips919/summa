# AGENTS.md

Guidance for AI coding agents (Claude, Codex, etc.) working in this repository. These rules come from the code review after the interactive demo (`review.org`), which has the full reasoning and longer examples.

## Stack

Go, SQLite, SQLC (generated queries and types in package `db`), chi (routing), templ (views), Datastar (front-end reactivity over SSE).

## Core principle

The database is the source of truth. Prefer SQLC-generated queries and types over redundant handwritten models and wrappers. Shared business operations may enforce accounting invariants and coordinate atomic database changes.

## Database

- **Need a database shape? Check the schema and queries first.** Use SQLC-generated types instead of duplicating them in handwritten structs. Named structs are appropriate for meaningful non-database shapes, such as test fixtures and external API contracts.
- **Call generated queries directly when practical.** Do not add functions that merely wrap SQLC calls. Shared operations such as `PostInTx` may enforce accounting invariants and coordinate atomic changes.
- **Enforce integrity in the schema where practical.** Use `CHECK`, `NOT NULL`, foreign keys, and similar constraints. Keep existing Go safeguards until equivalent protection exists elsewhere.
- **Exception:** security-sensitive input (login, sign-up, and similar) is still validated explicitly in Go.
- **Prefer SQL for shaping data.** If a page needs joined, aggregated, or ranked data, write a query (joins, window functions) so SQLC generates the type, instead of assembling it in Go.

## Routes

- The repository root contains `main.go` and routing files, one per feature (`invoices.go`, `login.go`, `homepage.go`, `settings.go`, ...), with their tests alongside them. Templ code goes in `views/`. Keep package tests alongside the code they exercise; use `tests/` for integration or end-to-end tests when appropriate. Do not add other kinds of code to the root.
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

- Prefer passing SQLC-generated types directly to templ components; avoid redundant view models.
- Rendering helpers (formatting amounts, building display strings) live in the `.templ` file that uses them. When there are enough of them, move them to `views/views_helper.go` in `package views`.
- Simple display formatting (money, basic string manipulation) may be done on the front end with Datastar where that is simpler.
- Selection and filtering for display (e.g. "last three transactions per user") can be done in the templ itself.

## Helper structs

Prefer SQL for shaping database results and templ for simple display selection. Use a named struct when it represents a meaningful concept, external contract, or test fixture and improves clarity. Avoid duplicating generated database types.

## Before you finish a change

- No new hand-written struct duplicates a table or query result.
- No new function exists only to wrap generated SQLC calls; shared invariant and transaction logic is welcome.
- Existing safeguards remain until equivalent protection exists elsewhere.
- No non-routing code was added to the repository root.
- `main.go` is still just middleware, mounts, and server start.
