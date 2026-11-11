package main

import (
	"context"
	"fmt"
	"time"

	"summa/internal/ledger"
)

// monthlyDocument uses the latest transaction date when no reporting date is supplied.
func (a *App) monthlyDocument(ctx context.Context, asOf time.Time) (ledger.LedgerDocument, error) {
	if asOf.IsZero() {
		transactions, err := a.queries.ListTransactions(ctx, a.orgID)
		if err != nil {
			return ledger.LedgerDocument{}, err
		}
		asOf = time.Now()
		if len(transactions) > 0 {
			asOf, err = time.Parse("2006-01-02", transactions[0].Date)
			if err != nil {
				return ledger.LedgerDocument{}, fmt.Errorf("invalid ledger date: %w", err)
			}
		}
	}
	dates := make([]ledger.Dates, 6)
	for i := range dates {
		month := time.Date(asOf.Year(), asOf.Month()-time.Month(5-i), 1, 0, 0, 0, 0, time.UTC)
		dates[i] = ledger.Dates{Date: month.AddDate(0, 1, -1)}
	}
	return ledger.BuildDocument(ctx, a.sqlDB, a.orgID, dates)
}

func monthlyNet(document ledger.LedgerDocument, column int) int64 {
	var total int64
	for _, row := range document.Rows {
		total += row.Data[column]
	}
	return total
}
