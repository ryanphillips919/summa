package ledger

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"time"

	"summa/internal/db"
)

type GeneralLedgerLine struct {
	TransactionID          int64
	Date, Description      string
	Debit, Credit, Balance int64
}

type GeneralLedger struct {
	Account          db.Account
	From, To         string
	Opening, Closing int64
	Lines            []GeneralLedgerLine
}

// BuildGeneralLedger reads a single consistent snapshot of finalized entries.
// Balances use the selected account's normal sign and include opening activity.
func BuildGeneralLedger(ctx context.Context, conn *sql.DB, orgID, accountID int64, from, to string) (GeneralLedger, error) {
	start, err := time.Parse("2006-01-02", from)
	if err != nil || start.Format("2006-01-02") != from {
		return GeneralLedger{}, errors.New("invalid start date")
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil || end.Format("2006-01-02") != to || end.Before(start) {
		return GeneralLedger{}, errors.New("invalid end date")
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return GeneralLedger{}, err
	}
	defer tx.Rollback()
	account, err := db.New(tx).GetAccount(ctx, db.GetAccountParams{OrganizationID: orgID, AccountID: accountID})
	if err != nil {
		return GeneralLedger{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT t.id, t.date, t.description, e.direction, e.amount
FROM entries e JOIN transactions t ON t.id = e.transaction_id
JOIN ledger_finalizations f ON f.transaction_id = t.id
WHERE t.organization_id = ? AND e.account_id = ? AND t.date <= ?
ORDER BY t.date, t.id, e.id`, orgID, accountID, to)
	if err != nil {
		return GeneralLedger{}, err
	}
	defer rows.Close()
	report := GeneralLedger{Account: account, From: from, To: to}
	var balance big.Int
	for rows.Next() {
		var line GeneralLedgerLine
		var direction string
		var amount int64
		if err := rows.Scan(&line.TransactionID, &line.Date, &line.Description, &direction, &amount); err != nil {
			return GeneralLedger{}, err
		}
		if direction == "debit" {
			line.Debit = amount
		} else {
			line.Credit = amount
		}
		if (direction == "debit") == (account.Type == "asset" || account.Type == "expense") {
			balance.Add(&balance, big.NewInt(amount))
		} else {
			balance.Sub(&balance, big.NewInt(amount))
		}
		if !balance.IsInt64() {
			return GeneralLedger{}, errors.New("account balance exceeds supported range")
		}
		if line.Date < from {
			report.Opening = balance.Int64()
			continue
		}
		line.Balance = balance.Int64()
		report.Lines = append(report.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return GeneralLedger{}, err
	}
	if err := rows.Close(); err != nil {
		return GeneralLedger{}, err
	}
	report.Closing = balance.Int64()
	if err := tx.Commit(); err != nil {
		return GeneralLedger{}, err
	}
	return report, nil
}
