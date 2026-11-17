package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"summa/internal/db"
)

// ReverseManual reverses one manual adjustment exactly once. It deliberately
// excludes invoice transactions and reversals, which have other workflows.
func ReverseManual(ctx context.Context, conn *sql.DB, orgID, originalID int64, date string) (db.Transaction, error) {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return db.Transaction{}, errors.New("enter a valid reversal date")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return db.Transaction{}, err
	}
	defer tx.Rollback()
	var originalDate, description string
	err = tx.QueryRowContext(ctx, `SELECT t.date, t.description FROM transactions t
JOIN ledger_finalizations f ON f.transaction_id = t.id
JOIN manual_journal_transactions m ON m.transaction_id = t.id
LEFT JOIN manual_journal_reversals r ON r.original_transaction_id = t.id
WHERE t.organization_id = ? AND t.id = ? AND r.original_transaction_id IS NULL`, orgID, originalID).Scan(&originalDate, &description)
	if err != nil {
		return db.Transaction{}, err
	}
	if date < originalDate {
		return db.Transaction{}, errors.New("reversal date cannot be before the original entry")
	}
	q := db.New(tx)
	lines, err := q.ListEntries(ctx, originalID)
	if err != nil {
		return db.Transaction{}, err
	}
	entries := make([]db.CreateEntryParams, 0, len(lines))
	for _, line := range lines {
		direction := "debit"
		if line.Direction == "debit" {
			direction = "credit"
		}
		entries = append(entries, db.CreateEntryParams{AccountID: line.AccountID, Direction: direction, Amount: line.Amount})
	}
	posted, err := PostInTx(ctx, tx, db.CreateTransactionParams{OrganizationID: orgID, Date: date, Description: fmt.Sprintf("Reversal of JE-%d: %s", originalID, description)}, entries)
	if err != nil {
		return db.Transaction{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO manual_journal_reversals (original_transaction_id, reversal_transaction_id) VALUES (?, ?)`, originalID, posted.ID); err != nil {
		return db.Transaction{}, err
	}
	if err := tx.Commit(); err != nil {
		return db.Transaction{}, err
	}
	return posted, nil
}
