package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"summa/internal/db"
	"summa/internal/ledger"
)

// seedDevData adds a small Modern Bagelry ledger to a new database. Existing
// organizations are left alone so restarting the app does not duplicate data.
func seedDevData(ctx context.Context, conn *sql.DB, q *db.Queries) (int64, error) {
	var existingID int64
	err := conn.QueryRowContext(ctx, "SELECT id FROM organizations ORDER BY id LIMIT 1").Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("find existing organization: %w", err)
	}

	org, err := q.CreateOrganization(ctx, "Modern Bagelry")
	if err != nil {
		return 0, err
	}
	accounts := map[string]int64{}
	for _, a := range []struct{ name, accountType string }{
		{"Cash", "asset"},
		{"Inventory", "asset"},
		{"Loans", "liability"},
		{"Owner Equity", "equity"},
		{"Bagel Sales", "revenue"},
		{"Facilities", "expense"},
		{"Rent", "expense"},
		{"Utilities", "expense"},
	} {
		created, err := q.CreateAccount(ctx, db.CreateAccountParams{
			OrganizationID: org.ID,
			Name:           a.name,
			Type:           a.accountType,
		})
		if err != nil {
			return 0, fmt.Errorf("seed account %s: %w", a.name, err)
		}
		accounts[a.name] = created.ID
	}

	for _, p := range []struct {
		date, description, debit, credit string
		cents                            int64
	}{
		{"2026-10-01", "Owner investment", "Cash", "Owner Equity", 100_000_000},
		{"2026-10-02", "Bank loan", "Cash", "Loans", 50_000_000},
		{"2026-10-03", "Buy inventory", "Inventory", "Cash", 25_000_000},
		{"2026-10-04", "Loan repayment", "Loans", "Cash", 3_000_000},
	} {
		_, err := ledger.Post(ctx, conn, db.CreateTransactionParams{
			OrganizationID: org.ID,
			Date:           p.date,
			Description:    p.description,
		}, []db.CreateEntryParams{
			{AccountID: accounts[p.debit], Direction: "debit", Amount: p.cents},
			{AccountID: accounts[p.credit], Direction: "credit", Amount: p.cents},
		})
		if err != nil {
			return 0, fmt.Errorf("seed transaction %q: %w", p.description, err)
		}
	}
	return org.ID, nil
}
