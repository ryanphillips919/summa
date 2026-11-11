// Package ledger is Summa's double-entry ledger. It works directly with the
// types sqlc generates in internal/db, so names match the tables: directions
// are "debit" or "credit", and account types are "asset", "expense", etc.
//
//   - ledger.go is the engine. Post and PostInTx save entries, and Balance
//     turns an account's totals into its balance.
//   - ledger-o1.go shows the books as a spreadsheet (accounts by month)
//     and saves each cell edit as a transaction through Post.
package ledger

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"summa/internal/db"
)

// Post saves a transaction and its entries. It refuses anything unbalanced
// and saves inside one database transaction, so either all of it is stored
// or none of it is. Entries need only AccountID, Direction and Amount; Post
// fills in TransactionID.
func Post(ctx context.Context, conn *sql.DB, transactionParams db.CreateTransactionParams, entries []db.CreateEntryParams) (db.Transaction, error) {
	// if anything below fails, Rollback undoes it all; after Commit it does
	// nothing
	sqlTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return db.Transaction{}, err
	}
	defer sqlTx.Rollback()

	ledgerTransaction, err := PostInTx(ctx, sqlTx, transactionParams, entries)
	if err != nil {
		return db.Transaction{}, err
	}
	return ledgerTransaction, sqlTx.Commit()
}

// PostInTx writes a balanced ledger transaction inside a caller-owned SQL
// transaction. The caller commits or rolls back the transaction.
func PostInTx(ctx context.Context, sqlTx *sql.Tx, transactionParams db.CreateTransactionParams, entries []db.CreateEntryParams) (db.Transaction, error) {
	// money always moves from somewhere to somewhere: one line for each side
	if len(entries) < 2 {
		return db.Transaction{}, errors.New("a transaction needs at least 2 entries")
	}

	// debits must equal credits, to the cent
	var debits, credits int64
	for _, e := range entries {
		if e.Amount <= 0 {
			return db.Transaction{}, errors.New("entry amounts must be positive")
		}
		switch e.Direction {
		case "debit":
			if debits > math.MaxInt64-e.Amount {
				return db.Transaction{}, errors.New("debit total is too large")
			}
			debits += e.Amount
		case "credit":
			if credits > math.MaxInt64-e.Amount {
				return db.Transaction{}, errors.New("credit total is too large")
			}
			credits += e.Amount
		default:
			return db.Transaction{}, errors.New("entry direction must be debit or credit")
		}
	}
	if debits != credits {
		return db.Transaction{}, errors.New("debits must equal credits")
	}

	q := db.New(sqlTx)

	// save the transaction first so its entries have an ID to point at
	ledgerTransaction, err := q.CreateTransaction(ctx, transactionParams)
	if err != nil {
		return db.Transaction{}, err
	}
	for _, e := range entries {
		e.TransactionID = ledgerTransaction.ID
		if err := q.CreateEntry(ctx, e); err != nil {
			return db.Transaction{}, err
		}
	}
	return ledgerTransaction, nil
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
