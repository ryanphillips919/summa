// Package payables records vendor bills and their payments in the ledger.
package payables

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

type Bill struct {
	ID, OrganizationID, VendorID                              int64
	Number, IssueDate, DueDate, Description                   string
	Amount, ExpenseAccountID, PayableAccountID, TransactionID int64
	Vendor                                                    string
	Paid                                                      int64
}

type NewBill struct {
	OrganizationID, VendorID                   int64
	Number, IssueDate, DueDate, Description    string
	Amount, ExpenseAccountID, PayableAccountID int64
	RequestKey                                 string
}

type Payment struct {
	Date, Account         string
	Amount, TransactionID int64
}

func Payments(ctx context.Context, conn *sql.DB, orgID, billID int64) ([]Payment, error) {
	rows, err := conn.QueryContext(ctx, `SELECT p.date,a.name,p.amount,p.transaction_id FROM bill_payments p JOIN accounts a ON a.id=p.payment_account_id WHERE p.organization_id=? AND p.bill_id=? ORDER BY p.date,p.id`, orgID, billID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.Date, &p.Account, &p.Amount, &p.TransactionID); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func validDate(date string) bool {
	t, err := time.Parse("2006-01-02", date)
	return err == nil && t.Format("2006-01-02") == date
}

func validKey(key string) bool {
	return len(key) >= 16 && len(key) <= 128 && strings.TrimSpace(key) == key
}

// Create posts one expense line and its payable together with the bill record.
func Create(ctx context.Context, conn *sql.DB, input NewBill) (int64, error) {
	input.Number, input.Description = strings.TrimSpace(input.Number), strings.TrimSpace(input.Description)
	if input.Number == "" || len(input.Number) > 100 || input.Description == "" || len(input.Description) > 250 || input.Amount <= 0 || !validKey(input.RequestKey) || !validDate(input.IssueDate) || !validDate(input.DueDate) || input.DueDate < input.IssueDate {
		return 0, errors.New("invalid bill details")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var previous NewBill
	var existingID int64
	err = tx.QueryRowContext(ctx, `SELECT id,vendor_id,number,issue_date,due_date,description,amount,expense_account_id,payable_account_id FROM bills WHERE organization_id=? AND request_key=?`, input.OrganizationID, input.RequestKey).Scan(&existingID, &previous.VendorID, &previous.Number, &previous.IssueDate, &previous.DueDate, &previous.Description, &previous.Amount, &previous.ExpenseAccountID, &previous.PayableAccountID)
	if err == nil {
		if previous.VendorID != input.VendorID || previous.Number != input.Number || previous.IssueDate != input.IssueDate || previous.DueDate != input.DueDate || previous.Description != input.Description || previous.Amount != input.Amount || previous.ExpenseAccountID != input.ExpenseAccountID || previous.PayableAccountID != input.PayableAccountID {
			return 0, errors.New("bill request key was already used for different details")
		}
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var vendorID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM vendors WHERE organization_id=? AND id=?`, input.OrganizationID, input.VendorID).Scan(&vendorID); err != nil {
		return 0, err
	}
	q := db.New(tx)
	expense, err := q.GetAccount(ctx, db.GetAccountParams{OrganizationID: input.OrganizationID, AccountID: input.ExpenseAccountID})
	if err != nil {
		return 0, err
	}
	payable, err := q.GetAccount(ctx, db.GetAccountParams{OrganizationID: input.OrganizationID, AccountID: input.PayableAccountID})
	if err != nil {
		return 0, err
	}
	if expense.Type != "expense" || payable.Type != "liability" {
		return 0, errors.New("choose an expense and a liability account")
	}
	posted, err := ledger.PostInTx(ctx, tx, db.CreateTransactionParams{OrganizationID: input.OrganizationID, Date: input.IssueDate, Description: "Bill " + input.Number + ": " + input.Description}, []db.CreateEntryParams{{AccountID: expense.ID, Direction: "debit", Amount: input.Amount}, {AccountID: payable.ID, Direction: "credit", Amount: input.Amount}})
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO bills (organization_id,vendor_id,number,issue_date,due_date,description,amount,expense_account_id,payable_account_id,transaction_id,request_key) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, input.OrganizationID, input.VendorID, input.Number, input.IssueDate, input.DueDate, input.Description, input.Amount, expense.ID, payable.ID, posted.ID, input.RequestKey)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

const billSelect = `SELECT b.id,b.organization_id,b.vendor_id,b.number,b.issue_date,b.due_date,b.description,b.amount,b.expense_account_id,b.payable_account_id,b.transaction_id,v.name,COALESCE((SELECT SUM(p.amount) FROM bill_payments p WHERE p.bill_id=b.id),0) FROM bills b JOIN vendors v ON v.id=b.vendor_id AND v.organization_id=b.organization_id `

type scanner interface{ Scan(...any) error }

func scanBill(row scanner) (Bill, error) {
	var b Bill
	err := row.Scan(&b.ID, &b.OrganizationID, &b.VendorID, &b.Number, &b.IssueDate, &b.DueDate, &b.Description, &b.Amount, &b.ExpenseAccountID, &b.PayableAccountID, &b.TransactionID, &b.Vendor, &b.Paid)
	return b, err
}
func Get(ctx context.Context, conn *sql.DB, orgID, id int64) (Bill, error) {
	return scanBill(conn.QueryRowContext(ctx, billSelect+`WHERE b.organization_id=? AND b.id=?`, orgID, id))
}
func List(ctx context.Context, conn *sql.DB, orgID int64) ([]Bill, error) {
	rows, err := conn.QueryContext(ctx, billSelect+`WHERE b.organization_id=? ORDER BY b.due_date,b.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Bill
	for rows.Next() {
		b, err := scanBill(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// Pay records a partial or full payment, with idempotency for repeated submits.
func Pay(ctx context.Context, conn *sql.DB, orgID, billID, accountID, amount int64, date, key string) (int64, error) {
	if amount <= 0 || !validDate(date) || !validKey(key) {
		return 0, errors.New("invalid payment details")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var priorBill, priorAccount, priorAmount, priorTxn int64
	var priorDate string
	err = tx.QueryRowContext(ctx, `SELECT bill_id,payment_account_id,amount,date,transaction_id FROM bill_payments WHERE organization_id=? AND request_key=?`, orgID, key).Scan(&priorBill, &priorAccount, &priorAmount, &priorDate, &priorTxn)
	if err == nil {
		if priorBill != billID || priorAccount != accountID || priorAmount != amount || priorDate != date {
			return 0, errors.New("payment request key was already used for different details")
		}
		return priorTxn, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	bill, err := scanBill(tx.QueryRowContext(ctx, billSelect+`WHERE b.organization_id=? AND b.id=?`, orgID, billID))
	if err != nil {
		return 0, err
	}
	if date < bill.IssueDate {
		return 0, errors.New("payment date cannot precede the bill date")
	}
	if bill.Paid > bill.Amount || amount > bill.Amount-bill.Paid {
		return 0, errors.New("payment exceeds the bill balance")
	}
	account, err := db.New(tx).GetAccount(ctx, db.GetAccountParams{OrganizationID: orgID, AccountID: accountID})
	if err != nil {
		return 0, err
	}
	if account.Type != "asset" {
		return 0, errors.New("payment account must be an asset")
	}
	posted, err := ledger.PostInTx(ctx, tx, db.CreateTransactionParams{OrganizationID: orgID, Date: date, Description: fmt.Sprintf("Payment for bill %s", bill.Number)}, []db.CreateEntryParams{{AccountID: bill.PayableAccountID, Direction: "debit", Amount: amount}, {AccountID: accountID, Direction: "credit", Amount: amount}})
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bill_payments (organization_id,bill_id,date,amount,payment_account_id,transaction_id,request_key) VALUES (?,?,?,?,?,?,?)`, orgID, billID, date, amount, accountID, posted.ID, key); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return posted.ID, nil
}
