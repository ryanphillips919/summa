package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

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

// NewCashDataSource picks where analytics reads cash movements from, based on
// SUMMA_ANALYTICS_SOURCE: "demo" (the default) or "database".
func NewCashDataSource(sqldb *sql.DB, orgID int64) (CashDataSource, error) {
	switch src := os.Getenv("SUMMA_ANALYTICS_SOURCE"); src {
	case "", "demo":
		return DemoCashDataSource{Path: "analytics/data/demo_cash_movements.json"}, nil
	case "database":
		return DatabaseCashDataSource{DB: sqldb, OrganizationID: orgID, ForecastDays: 30, ObservationDays: 60}, nil
	default:
		return nil, fmt.Errorf("unknown SUMMA_ANALYTICS_SOURCE %q; use demo or database", src)
	}
}
