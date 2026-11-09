package analytics

import "context"

// CashSnapshot is the financial data the analytics engine needs.
type CashSnapshot struct {
	OpeningCashBalance float64
	ForecastDays       int
	Movements          []CashMovement
}

// CashDataSource provides authorized cash-account data to analytics.
type CashDataSource interface {
	LoadCashSnapshot(ctx context.Context) (CashSnapshot, error)
}
