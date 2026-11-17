package ledger

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
)

type BalanceSheetRow struct {
	Name, Type string
	Amount     int64
}

type BalanceSheet struct {
	AsOf                                                                      string
	Rows                                                                      []BalanceSheetRow
	Assets, Liabilities, Equity, AccumulatedIncome, TotalLiabilitiesAndEquity int64
}

// BuildBalanceSheet includes the cumulative balance of revenue and expense
// accounts in equity. This also works when closing entries have transferred
// prior years' earnings into an equity account.
func BuildBalanceSheet(ctx context.Context, conn *sql.DB, orgID int64, asOf string) (BalanceSheet, error) {
	trial, err := BuildTrialBalance(ctx, conn, orgID, asOf)
	if err != nil {
		return BalanceSheet{}, err
	}
	report := BalanceSheet{AsOf: asOf}
	var assets, liabilities, equity, income big.Int
	for _, row := range trial.Rows {
		var amount int64
		switch row.Type {
		case "asset":
			amount = row.Debit - row.Credit
			assets.Add(&assets, big.NewInt(amount))
		case "liability":
			amount = row.Credit - row.Debit
			liabilities.Add(&liabilities, big.NewInt(amount))
		case "equity":
			amount = row.Credit - row.Debit
			equity.Add(&equity, big.NewInt(amount))
		case "revenue", "expense":
			income.Add(&income, big.NewInt(row.Credit-row.Debit))
			continue
		default:
			return BalanceSheet{}, errors.New("unsupported account type")
		}
		report.Rows = append(report.Rows, BalanceSheetRow{Name: row.Name, Type: row.Type, Amount: amount})
	}
	var total big.Int
	total.Add(&liabilities, &equity)
	total.Add(&total, &income)
	if !assets.IsInt64() || !liabilities.IsInt64() || !equity.IsInt64() || !income.IsInt64() || !total.IsInt64() {
		return BalanceSheet{}, errors.New("balance sheet total exceeds supported range")
	}
	if assets.Cmp(&total) != 0 {
		return BalanceSheet{}, errors.New("balance sheet is out of balance")
	}
	report.Assets, report.Liabilities, report.Equity = assets.Int64(), liabilities.Int64(), equity.Int64()
	report.AccumulatedIncome, report.TotalLiabilitiesAndEquity = income.Int64(), total.Int64()
	return report, nil
}
