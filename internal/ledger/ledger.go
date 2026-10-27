// Package ledger is Summa's double-entry ledger. It works directly with the
// types sqlc generates in internal/db, so names match the tables: directions
// are "debit" or "credit", and account types are "asset", "expense", etc.
//
//   - ledger.go is the engine. Post is the only way money enters the books,
//     and Balance turns an account's totals into its balance.
//   - ledger-o1.go shows the books as a spreadsheet (accounts by month)
//     and saves each cell edit as a transaction through Post.
package ledger

import (
	"context"
	"database/sql"
	"errors"

	"summa/internal/db"
)

// Post saves a transaction and its entries. It refuses anything unbalanced
// and saves inside one database transaction, so either all of it is stored
// or none of it is. Entries need only AccountID, Direction and Amount; Post
// fills in TransactionID.
func Post(ctx context.Context, conn *sql.DB, t db.CreateTransactionParams, entries []db.CreateEntryParams) (db.Transaction, error) {
	// money always moves from somewhere to somewhere: one line for each side
	if len(entries) < 2 {
		return db.Transaction{}, errors.New("a transaction needs at least 2 entries")
	}

	// debits must equal credits, to the cent
	var debits, credits int64
	for _, e := range entries {
		if e.Direction == "debit" {
			debits += e.Amount
		} else {
			credits += e.Amount
		}
	}
	if debits != credits {
		return db.Transaction{}, errors.New("debits must equal credits")
	}

	// if anything below fails, Rollback undoes it all; after Commit it does
	// nothing
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return db.Transaction{}, err
	}
	defer tx.Rollback()
	q := db.New(tx)

	// save the transaction first so its entries have an ID to point at
	saved, err := q.CreateTransaction(ctx, t)
	if err != nil {
		return db.Transaction{}, err
	}
	for _, e := range entries {
		e.TransactionID = saved.ID
		// the database rejects a misspelled direction or a non-positive
		// amount, which rolls everything back
		if err := q.CreateEntry(ctx, e); err != nil {
			return db.Transaction{}, err
		}
	}
	return saved, tx.Commit()
}

// Balance turns an account's debit and credit totals (from AccountTotals)
// into its balance. Debits increase assets and expenses; credits increase
// liabilities, equity and revenue. Cash with $10,000 of debits and $3,200 of
// credits has a balance of $6,800.
func Balance(accountType string, debits, credits int64) int64 {
	if accountType == "asset" || accountType == "expense" {
		return debits - credits
	}
	return credits - debits
}
