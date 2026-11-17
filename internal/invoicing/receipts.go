package invoicing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"summa/internal/db"
	"summa/internal/ledger"
)

type Receipt struct {
	TransactionID  int64
	Date           string
	Amount         int64
	DepositAccount string
}

// ListReceipts only returns allocations explicitly recorded for this invoice.
func ListReceipts(ctx context.Context, conn *sql.DB, orgID, invoiceID int64) ([]Receipt, error) {
	rows, err := conn.QueryContext(ctx, `SELECT r.transaction_id, r.date, r.amount, a.name
FROM invoice_receipts r JOIN accounts a ON a.id = r.deposit_account_id
WHERE r.organization_id = ? AND r.invoice_id = ? ORDER BY r.date, r.id`, orgID, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Receipt
	for rows.Next() {
		var receipt Receipt
		if err := rows.Scan(&receipt.TransactionID, &receipt.Date, &receipt.Amount, &receipt.DepositAccount); err != nil {
			return nil, err
		}
		result = append(result, receipt)
	}
	return result, rows.Err()
}

// RecordReceipt posts one payment and allocates it to a single invoice. A
// repeated request key with identical inputs returns the original transaction.
func RecordReceipt(ctx context.Context, conn *sql.DB, orgID, invoiceID, depositID, amount int64, date, requestKey string) (db.Transaction, error) {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return db.Transaction{}, errors.New("enter a valid payment date")
	}
	if amount <= 0 || len(requestKey) < 16 || len(requestKey) > 128 || strings.TrimSpace(requestKey) != requestKey {
		return db.Transaction{}, errors.New("invalid payment amount or request key")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return db.Transaction{}, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	var previousInvoice, previousDeposit, previousAmount, previousTransaction int64
	var previousDate string
	err = tx.QueryRowContext(ctx, `SELECT invoice_id, deposit_account_id, amount, date, transaction_id FROM invoice_receipts WHERE organization_id = ? AND request_key = ?`, orgID, requestKey).Scan(&previousInvoice, &previousDeposit, &previousAmount, &previousDate, &previousTransaction)
	if err == nil {
		if previousInvoice != invoiceID || previousDeposit != depositID || previousAmount != amount || previousDate != date {
			return db.Transaction{}, errors.New("payment request key was already used for different details")
		}
		return q.GetTransaction(ctx, db.GetTransactionParams{OrganizationID: orgID, TransactionID: previousTransaction})
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return db.Transaction{}, err
	}
	invoice, err := q.GetInvoice(ctx, db.GetInvoiceParams{OrganizationID: orgID, InvoiceID: invoiceID})
	if err != nil {
		return db.Transaction{}, err
	}
	if !invoice.PostedTransactionID.Valid {
		return db.Transaction{}, errors.New("post the invoice before recording a payment")
	}
	if date < invoice.IssueDate {
		return db.Transaction{}, errors.New("payment date cannot be before the invoice date")
	}
	deposit, err := q.GetAccount(ctx, db.GetAccountParams{OrganizationID: orgID, AccountID: depositID})
	if err != nil {
		return db.Transaction{}, err
	}
	if deposit.Type != "asset" {
		return db.Transaction{}, errors.New("deposit account must be an asset")
	}
	var receivableID int64
	if err := tx.QueryRowContext(ctx, `SELECT e.account_id FROM entries e JOIN accounts a ON a.id = e.account_id
WHERE e.transaction_id = ? AND a.organization_id = ? AND a.type = 'asset' AND e.direction = 'debit'`, invoice.PostedTransactionID.Int64, orgID).Scan(&receivableID); err != nil {
		return db.Transaction{}, err
	}
	if depositID == receivableID {
		return db.Transaction{}, errors.New("deposit account must differ from receivables")
	}
	total, err := q.InvoiceTotal(ctx, db.InvoiceTotalParams{OrganizationID: orgID, InvoiceID: invoiceID})
	if err != nil {
		return db.Transaction{}, err
	}
	var allocated int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM invoice_receipts WHERE organization_id = ? AND invoice_id = ?`, orgID, invoiceID).Scan(&allocated); err != nil {
		return db.Transaction{}, err
	}
	if allocated > total || amount > total-allocated {
		return db.Transaction{}, errors.New("payment exceeds the invoice balance")
	}
	posted, err := ledger.PostInTx(ctx, tx, db.CreateTransactionParams{OrganizationID: orgID, Date: date, Description: fmt.Sprintf("Payment for invoice %s", invoice.Number)}, []db.CreateEntryParams{
		{AccountID: depositID, Direction: "debit", Amount: amount},
		{AccountID: receivableID, Direction: "credit", Amount: amount},
	})
	if err != nil {
		return db.Transaction{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_receipts (organization_id, invoice_id, transaction_id, deposit_account_id, date, amount, request_key) VALUES (?, ?, ?, ?, ?, ?, ?)`, orgID, invoiceID, posted.ID, depositID, date, amount, requestKey); err != nil {
		return db.Transaction{}, err
	}
	if err := tx.Commit(); err != nil {
		return db.Transaction{}, err
	}
	return posted, nil
}
