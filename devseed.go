package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	"summa/internal/analytics"
	"summa/internal/db"
	"summa/internal/ledger"
)

const seedAnalyticsDemoEnv = "SUMMA_SEED_ANALYTICS_DEMO"

// seedDevData gives an empty database something to play with: the Modern
// Bagelry example from docs/ledger-rules.md, a few users, and a small chart
// of accounts. It returns the first organization's ID, and does nothing else
// if the database already has an organization.
//
// This stands in for sign-up and onboarding until auth (#8) exists.
func seedDevData(ctx context.Context, conn *sql.DB, q *db.Queries) (int64, error) {
	var existingID int64
	err := conn.QueryRowContext(ctx, "SELECT id FROM organizations ORDER BY id LIMIT 1").Scan(&existingID)
	if err == nil {
		if os.Getenv(seedAnalyticsDemoEnv) == "1" {
			if err := seedAnalyticsSanityData(ctx, conn, q, existingID); err != nil {
				return 0, err
			}
		}
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	org, err := q.CreateOrganization(ctx, "Modern Bagelry")
	if err != nil {
		return 0, err
	}
	// users aren't linked to organizations yet; that comes with auth (#8)
	for _, u := range []struct{ email, name string }{
		{"owner@example.com", "Sawyer (owner)"},
		{"accountant@example.com", "Ada (accountant)"},
		{"bookkeeper@example.com", "Bea (bookkeeper)"},
		{"viewer@example.com", "Vic (viewer)"},
	} {
		if _, err := q.CreateUser(ctx, db.CreateUserParams{Email: u.email, Name: u.name}); err != nil {
			return 0, err
		}
	}

	if os.Getenv(seedAnalyticsDemoEnv) == "1" {
		if err := seedAnalyticsSanityData(ctx, conn, q, org.ID); err != nil {
			return 0, err
		}
		return org.ID, nil
	}

	accounts := map[string]int64{} // name -> ID
	for _, a := range []struct{ name, accountType string }{
		{"Cash", "asset"},
		{"Inventory", "asset"},
		{"Loans", "liability"},
		{"Owner Equity", "equity"},
		{"Bagel Sales", "revenue"},
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
		{"2026-10-01", "Owner investment", "Cash", "Owner Equity", 1_000_000_00},
		{"2026-10-02", "Bank loan", "Cash", "Loans", 500_000_00},
		{"2026-10-03", "Buy inventory", "Inventory", "Cash", 250_000_00},
		{"2026-10-04", "Loan repayment", "Loans", "Cash", 30_000_00},
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

func seedAnalyticsSanityData(
	ctx context.Context,
	conn *sql.DB,
	q *db.Queries,
	orgID int64,
) error {
	const marker = "Analytics opening balance"

	var existing int
	if err := conn.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM transactions
		 WHERE organization_id = ? AND description = ?`,
		orgID,
		marker,
	).Scan(&existing); err != nil {
		return err
	}

	if existing > 0 {
		return nil
	}

	snapshot, err := (analytics.DemoCashDataSource{
		Path: "analytics/data/demo_cash_movements.json",
	}).LoadCashSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("load analytics seed fixture: %w", err)
	}

	createAccount := func(name, accountType string) (int64, error) {
		account, err := q.CreateAccount(ctx, db.CreateAccountParams{
			OrganizationID: orgID,
			Name:           name,
			Type:           accountType,
		})
		if err != nil {
			return 0, err
		}
		return account.ID, nil
	}

	cashID, err := createAccount("Cash", "asset")
	if err != nil {
		return fmt.Errorf("seed Cash account: %w", err)
	}

	openingEquityID, err := createAccount("Opening Balance Equity", "equity")
	if err != nil {
		return fmt.Errorf("seed opening equity account: %w", err)
	}

	vendorAccounts := map[string]int64{}

	for _, movement := range snapshot.Movements {
		key := analyticsVendorKey(movement.Vendor)
		if _, exists := vendorAccounts[key]; exists {
			continue
		}

		accountType := "expense"
		if movement.Amount > 0 {
			accountType = "revenue"
		}

		accountID, err := createAccount(
			strings.Join(strings.Fields(movement.Vendor), " "),
			accountType,
		)
		if err != nil {
			return fmt.Errorf(
				"seed account for %q: %w",
				movement.Vendor,
				err,
			)
		}

		vendorAccounts[key] = accountID
	}

	openingCents := int64(math.Round(snapshot.OpeningCashBalance * 100))

	if _, err := ledger.Post(ctx, conn, db.CreateTransactionParams{
		OrganizationID: orgID,
		Date:           "2020-01-01",
		Description:    marker,
	}, []db.CreateEntryParams{
		{
			AccountID: cashID,
			Direction: "debit",
			Amount:    openingCents,
		},
		{
			AccountID: openingEquityID,
			Direction: "credit",
			Amount:    openingCents,
		},
	}); err != nil {
		return fmt.Errorf("seed opening cash balance: %w", err)
	}

	for _, movement := range snapshot.Movements {
		amountCents := int64(math.Round(math.Abs(movement.Amount) * 100))
		vendorID := vendorAccounts[analyticsVendorKey(movement.Vendor)]

		cashDirection := "debit"
		counterpartyDirection := "credit"

		if movement.Amount < 0 {
			cashDirection = "credit"
			counterpartyDirection = "debit"
		}

		if _, err := ledger.Post(ctx, conn, db.CreateTransactionParams{
			OrganizationID: orgID,
			Date:           movement.Date,
			Description:    movement.Description,
		}, []db.CreateEntryParams{
			{
				AccountID: cashID,
				Direction: cashDirection,
				Amount:    amountCents,
			},
			{
				AccountID: vendorID,
				Direction: counterpartyDirection,
				Amount:    amountCents,
			},
		}); err != nil {
			return fmt.Errorf(
				"seed analytics movement %q: %w",
				movement.ID,
				err,
			)
		}
	}

	return nil
}

func analyticsVendorKey(vendor string) string {
	return strings.ToLower(strings.Join(strings.Fields(vendor), " "))
}
