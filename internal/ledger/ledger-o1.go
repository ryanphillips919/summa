package ledger

// ledger-o1.go shows the books as a spreadsheet: one row per revenue or
// expense account and one column per month. Cells are never stored.
// BuildDocument works them out from the ledger's entries, and SaveCell saves
// an edit as a balanced transaction through Post.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"summa/internal/db"
)

// LedgerDocument is the spreadsheet: one column per month, one row per
// account.
type LedgerDocument struct {
	Dates []Dates
	Rows  []LedgerRow
}

// Dates is one column: the last day of a month, e.g. 2026-10-31 for October.
type Dates struct {
	Date time.Time
}

// LedgerRow is one account's amount for each month, in cents, where Data[i]
// is the month in Dates[i]. Revenue is positive and costs are negative:
// $3,200 of rent is -320000.
type LedgerRow struct {
	Account db.Account
	Data    []int64
}

// BuildDocument works out the spreadsheet from the ledger.
func BuildDocument(ctx context.Context, conn *sql.DB, orgID int64, dates []Dates) (LedgerDocument, error) {
	q := db.New(conn)
	doc := LedgerDocument{Dates: dates}

	// a row for each revenue and expense account, starting at zero
	accounts, err := q.ListAccounts(ctx, orgID)
	if err != nil {
		return doc, err
	}
	rowFor := map[int64]int{} // account ID -> index in doc.Rows
	for _, a := range accounts {
		if a.Type == "revenue" || a.Type == "expense" {
			rowFor[a.ID] = len(doc.Rows)
			doc.Rows = append(doc.Rows, LedgerRow{Account: a, Data: make([]int64, len(dates))})
		}
	}

	colFor := map[string]int{} // "2026-10" -> index in doc.Dates
	for i, d := range dates {
		colFor[d.Date.Format("2006-01")] = i
	}

	// add each entry to its account's row, in its month's column
	transactions, err := q.ListTransactions(ctx, orgID)
	if err != nil {
		return doc, err
	}
	for _, t := range transactions {
		col, shown := colFor[t.Date[:7]] // "2026-10-31" -> "2026-10"
		if !shown {
			continue
		}
		entries, err := q.ListEntries(ctx, t.ID)
		if err != nil {
			return doc, err
		}
		for _, e := range entries {
			row, shown := rowFor[e.AccountID]
			if !shown {
				continue // e.g. Cash, which has no row
			}
			// credits add and debits subtract, so revenue (credited) comes
			// out positive and costs (debited) come out negative
			if e.Direction == "credit" {
				doc.Rows[row].Data[col] += e.Amount
			} else {
				doc.Rows[row].Data[col] -= e.Amount
			}
		}
	}
	return doc, nil
}

// SaveCell saves an edit to one cell by posting the difference as a
// transaction dated that month, with Cash on the other side:
//
//	Rent  Oct: -$3,200 to -$3,500   debit Rent $300,  credit Cash $300
//	Sales Oct:  $9,000 to  $8,500   debit Sales $500, credit Cash $500
//
// oldValue should come from BuildDocument, not the browser, so the change is
// measured against what's actually in the books. If the value didn't change,
// nothing is posted.
func SaveCell(ctx context.Context, conn *sql.DB, orgID, cashID int64, account db.Account, date Dates, oldValue, newValue int64) (db.Transaction, error) {
	change := newValue - oldValue
	if change == 0 {
		return db.Transaction{}, nil
	}

	// a cell is credits minus debits, so raising it credits the account and
	// lowering it debits the account. Cash takes the other side to balance
	accountSide, cashSide, amount := "credit", "debit", change
	if change < 0 {
		accountSide, cashSide, amount = "debit", "credit", -change
	}

	return Post(ctx, conn,
		db.CreateTransactionParams{
			OrganizationID: orgID,
			Date:           date.Date.Format("2006-01-02"),
			Description:    fmt.Sprintf("%s for %s: %s to %s", account.Name, date.Date.Format("Jan, 06"), dollars(oldValue), dollars(newValue)),
		},
		[]db.CreateEntryParams{
			{AccountID: account.ID, Direction: accountSide, Amount: amount},
			{AccountID: cashID, Direction: cashSide, Amount: amount},
		},
	)
}

// dollars formats cents for descriptions: -320000 -> "-$3200.00".
func dollars(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}
