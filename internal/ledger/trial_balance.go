package ledger

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

// TrialBalanceRow shows the net debit or credit balance of one account.
type TrialBalanceRow struct {
	AccountID     int64
	Name, Type    string
	Debit, Credit int64
}

type TrialBalance struct {
	AsOf                    string
	Rows                    []TrialBalanceRow
	TotalDebit, TotalCredit int64
}

// BuildTrialBalance includes finalized transactions through asOf, inclusive.
// All amounts are integer cents. A database overflow or out-of-balance result
// is returned as an error instead of producing a misleading report.
func BuildTrialBalance(ctx context.Context, conn *sql.DB, orgID int64, asOf string) (TrialBalance, error) {
	date, err := time.Parse("2006-01-02", asOf)
	if err != nil || date.Format("2006-01-02") != asOf {
		return TrialBalance{}, errors.New("invalid report date")
	}
	rows, err := conn.QueryContext(ctx, `
SELECT a.id, a.name, a.type,
       COALESCE(SUM(CASE WHEN e.direction = 'debit' THEN e.amount ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN e.direction = 'credit' THEN e.amount ELSE 0 END), 0)
FROM accounts a
LEFT JOIN (
  SELECT e.account_id, e.direction, e.amount FROM entries e
  JOIN transactions t ON t.id = e.transaction_id
  JOIN ledger_finalizations f ON f.transaction_id = t.id
  WHERE t.organization_id = ? AND t.date <= ?
) e ON e.account_id = a.id
WHERE a.organization_id = ?
GROUP BY a.id, a.name, a.type ORDER BY a.name, a.id`, orgID, asOf, orgID)
	if err != nil {
		return TrialBalance{}, err
	}
	defer rows.Close()
	report := TrialBalance{AsOf: asOf}
	for rows.Next() {
		var row TrialBalanceRow
		var debit, credit int64
		if err := rows.Scan(&row.AccountID, &row.Name, &row.Type, &debit, &credit); err != nil {
			return TrialBalance{}, err
		}
		if debit >= credit {
			row.Debit = debit - credit
		} else {
			row.Credit = credit - debit
		}
		if report.TotalDebit > math.MaxInt64-row.Debit || report.TotalCredit > math.MaxInt64-row.Credit {
			return TrialBalance{}, errors.New("trial balance total exceeds supported range")
		}
		report.TotalDebit += row.Debit
		report.TotalCredit += row.Credit
		report.Rows = append(report.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return TrialBalance{}, err
	}
	if report.TotalDebit != report.TotalCredit {
		return TrialBalance{}, errors.New("trial balance is out of balance")
	}
	return report, nil
}
