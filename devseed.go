package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"summa/internal/db"
	"summa/internal/ledger"
)

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
