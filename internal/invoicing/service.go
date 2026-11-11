// Package invoicing connects finalized invoices to the double-entry ledger.
package invoicing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"summa/internal/db"
	"summa/internal/ledger"
)

var ErrAlreadyPosted = errors.New("invoice is already posted")

// PostInvoice records an invoice as receivable and earned revenue. The caller
// chooses the two accounts; both must belong to the invoice's organization.
// The ledger entries and invoice link commit together.
func PostInvoice(ctx context.Context, conn *sql.DB, organizationID, invoiceID, receivableAccountID, revenueAccountID int64) (db.Transaction, error) {
	// ctx lets the caller cancel the work; sqlTx groups the database changes in
	// a transaction. The ctx can be used to cancel the transaction, but the
	// transaction must be committed or rolled back before returning.
	sqlTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return db.Transaction{}, err
	}
	defer sqlTx.Rollback()
	q := db.New(sqlTx)

	invoice, err := q.GetInvoice(ctx, db.GetInvoiceParams{
		InvoiceID:      invoiceID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return db.Transaction{}, fmt.Errorf("get invoice: %w", err)
	}
	if invoice.PostedTransactionID.Valid {
		return db.Transaction{}, ErrAlreadyPosted
	}

	total, err := q.InvoiceTotal(ctx, db.InvoiceTotalParams{
		OrganizationID: organizationID,
		InvoiceID:      invoiceID,
	})
	if err != nil {
		return db.Transaction{}, fmt.Errorf("calculate invoice total: %w", err)
	}
	if total <= 0 {
		return db.Transaction{}, errors.New("invoice total must be positive")
	}

	receivable, err := q.GetAccount(ctx, db.GetAccountParams{
		AccountID:      receivableAccountID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return db.Transaction{}, fmt.Errorf("get receivable account: %w", err)
	}
	if receivable.Type != "asset" {
		return db.Transaction{}, errors.New("receivable account must be an asset")
	}
	revenue, err := q.GetAccount(ctx, db.GetAccountParams{
		AccountID:      revenueAccountID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return db.Transaction{}, fmt.Errorf("get revenue account: %w", err)
	}
	if revenue.Type != "revenue" {
		return db.Transaction{}, errors.New("revenue account must be revenue")
	}

	ledgerTransaction, err := ledger.PostInTx(ctx, sqlTx, db.CreateTransactionParams{
		OrganizationID: organizationID,
		Date:           invoice.IssueDate,
		Description:    "Invoice " + invoice.Number,
	}, []db.CreateEntryParams{
		{AccountID: receivable.ID, Direction: "debit", Amount: total},
		{AccountID: revenue.ID, Direction: "credit", Amount: total},
	})
	if err != nil {
		return db.Transaction{}, fmt.Errorf("post invoice entries: %w", err)
	}

	_, err = q.MarkInvoicePosted(ctx, db.MarkInvoicePostedParams{
		TransactionID:  sql.NullInt64{Int64: ledgerTransaction.ID, Valid: true},
		InvoiceID:      invoiceID,
		OrganizationID: organizationID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return db.Transaction{}, ErrAlreadyPosted
	}
	if err != nil {
		return db.Transaction{}, fmt.Errorf("mark invoice posted: %w", err)
	}
	if err := sqlTx.Commit(); err != nil {
		return db.Transaction{}, err
	}
	return ledgerTransaction, nil
}
