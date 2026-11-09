package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// DatabaseCashDataSource translates posted Cash ledger entries into the
// snapshot consumed by the analytics engine.
type DatabaseCashDataSource struct {
	DB              *sql.DB
	OrganizationID  int64
	ForecastDays    int
	ObservationDays int
}

func (s DatabaseCashDataSource) LoadCashSnapshot(
	ctx context.Context,
) (CashSnapshot, error) {
	forecastDays := s.ForecastDays
	if forecastDays == 0 {
		forecastDays = 30
	}

	observationDays := s.ObservationDays
	if observationDays == 0 {
		observationDays = 60
	}

	latestDate, err := s.latestCashDate(ctx)
	if err != nil {
		return CashSnapshot{}, err
	}

	latest, err := time.Parse("2006-01-02", latestDate)
	if err != nil {
		return CashSnapshot{}, fmt.Errorf("parse latest cash date: %w", err)
	}

	startDate := latest.AddDate(0, 0, -(observationDays - 1)).
		Format("2006-01-02")

	openingCashBalance, err := s.openingCashBalance(ctx, startDate)
	if err != nil {
		return CashSnapshot{}, err
	}

	movements, err := s.cashMovements(ctx, startDate)
	if err != nil {
		return CashSnapshot{}, err
	}

	return CashSnapshot{
		OpeningCashBalance: openingCashBalance,
		ForecastDays:       forecastDays,
		Movements:          movements,
	}, nil
}

func (s DatabaseCashDataSource) latestCashDate(
	ctx context.Context,
) (string, error) {
	const query = `
SELECT MAX(t.date)
FROM transactions t
JOIN entries e ON e.transaction_id = t.id
JOIN accounts a ON a.id = e.account_id
WHERE t.organization_id = ?
  AND a.name = 'Cash'
`

	var latest sql.NullString
	if err := s.DB.QueryRowContext(ctx, query, s.OrganizationID).
		Scan(&latest); err != nil {
		return "", fmt.Errorf("find latest cash entry: %w", err)
	}

	if !latest.Valid {
		return "", fmt.Errorf("no Cash entries found for organization %d", s.OrganizationID)
	}

	return latest.String, nil
}

func (s DatabaseCashDataSource) openingCashBalance(
	ctx context.Context,
	startDate string,
) (float64, error) {
	const query = `
SELECT COALESCE(SUM(
	CASE e.direction
		WHEN 'debit' THEN e.amount
		ELSE -e.amount
	END
), 0)
FROM transactions t
JOIN entries e ON e.transaction_id = t.id
JOIN accounts a ON a.id = e.account_id
WHERE t.organization_id = ?
  AND a.name = 'Cash'
  AND t.date < ?
`

	var cents int64
	if err := s.DB.QueryRowContext(ctx, query, s.OrganizationID, startDate).
		Scan(&cents); err != nil {
		return 0, fmt.Errorf("calculate opening cash balance: %w", err)
	}

	return float64(cents) / 100, nil
}

func (s DatabaseCashDataSource) cashMovements(
	ctx context.Context,
	startDate string,
) ([]CashMovement, error) {
	const query = `
SELECT
	t.id,
	t.date,
	SUM(CASE e.direction
		WHEN 'debit' THEN e.amount
		ELSE -e.amount
	END) AS cash_cents,
	t.description,
	COALESCE(MIN(counterparty.name), '')
FROM transactions t
JOIN entries e ON e.transaction_id = t.id
JOIN accounts cash ON cash.id = e.account_id
LEFT JOIN entries other_entry
	ON other_entry.transaction_id = t.id
	AND other_entry.account_id != cash.id
LEFT JOIN accounts counterparty
	ON counterparty.id = other_entry.account_id
WHERE t.organization_id = ?
  AND cash.name = 'Cash'
  AND t.date >= ?
GROUP BY t.id, t.date, t.description
HAVING cash_cents != 0
ORDER BY t.date, t.id
`

	rows, err := s.DB.QueryContext(
		ctx,
		query,
		s.OrganizationID,
		startDate,
	)
	if err != nil {
		return nil, fmt.Errorf("load cash movements: %w", err)
	}
	defer rows.Close()

	var movements []CashMovement

	for rows.Next() {
		var (
			id          int64
			date        string
			cents       int64
			description string
			vendor      string
		)

		if err := rows.Scan(
			&id,
			&date,
			&cents,
			&description,
			&vendor,
		); err != nil {
			return nil, fmt.Errorf("scan cash movement: %w", err)
		}

		movements = append(movements, CashMovement{
			ID:          strconv.FormatInt(id, 10),
			Date:        date,
			Amount:      float64(cents) / 100,
			Description: description,
			Vendor:      vendor,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read cash movements: %w", err)
	}

	return movements, nil
}
