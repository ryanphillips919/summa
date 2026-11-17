package ledger

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"time"
)

type IncomeStatementRow struct {
	Name, Type string
	Amount     int64
}

type IncomeStatement struct {
	From, To                     string
	Rows                         []IncomeStatementRow
	Revenue, Expenses, NetIncome int64
}

// BuildIncomeStatement reports period activity, with revenue and expense
// accounts in their normal signs. Contra balances can therefore be negative.
func BuildIncomeStatement(ctx context.Context, conn *sql.DB, orgID int64, from, to string) (IncomeStatement, error) {
	start, err := time.Parse("2006-01-02", from)
	if err != nil || start.Format("2006-01-02") != from {
		return IncomeStatement{}, errors.New("invalid start date")
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil || end.Format("2006-01-02") != to || end.Before(start) {
		return IncomeStatement{}, errors.New("invalid end date")
	}
	rows, err := conn.QueryContext(ctx, `
SELECT a.name, a.type,
       COALESCE(SUM(CASE WHEN e.direction = 'debit' THEN e.amount ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN e.direction = 'credit' THEN e.amount ELSE 0 END), 0)
FROM accounts a
LEFT JOIN (
  SELECT e.account_id, e.direction, e.amount FROM entries e
  JOIN transactions t ON t.id = e.transaction_id
  JOIN ledger_finalizations f ON f.transaction_id = t.id
  WHERE t.organization_id = ? AND t.date >= ? AND t.date <= ?
) e ON e.account_id = a.id
WHERE a.organization_id = ? AND a.type IN ('revenue', 'expense')
GROUP BY a.id, a.name, a.type ORDER BY a.type DESC, a.name, a.id`, orgID, from, to, orgID)
	if err != nil {
		return IncomeStatement{}, err
	}
	defer rows.Close()
	report := IncomeStatement{From: from, To: to}
	var revenue, expenses big.Int
	for rows.Next() {
		var row IncomeStatementRow
		var debit, credit int64
		if err := rows.Scan(&row.Name, &row.Type, &debit, &credit); err != nil {
			return IncomeStatement{}, err
		}
		if row.Type == "revenue" {
			row.Amount = credit - debit
			revenue.Add(&revenue, big.NewInt(row.Amount))
		} else {
			row.Amount = debit - credit
			expenses.Add(&expenses, big.NewInt(row.Amount))
		}
		report.Rows = append(report.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return IncomeStatement{}, err
	}
	var income big.Int
	income.Sub(&revenue, &expenses)
	if !revenue.IsInt64() || !expenses.IsInt64() || !income.IsInt64() {
		return IncomeStatement{}, errors.New("income statement total exceeds supported range")
	}
	report.Revenue, report.Expenses, report.NetIncome = revenue.Int64(), expenses.Int64(), income.Int64()
	return report, nil
}
