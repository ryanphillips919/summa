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
// Bagelry example from docs/ledger-rules.md, one user per role, and a small
// chart of accounts. It returns the first organization's ID, and does nothing
// else if the database already has an organization.
//
// This stands in for sign-up and onboarding until auth (#8) exists.
func seedDevData(ctx context.Context, q *db.Queries, svc *ledger.Service) (int64, error) {
	existing, err := q.FirstOrganization(ctx)
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	org, err := q.CreateOrganization(ctx, "Modern Bagelry")
	if err != nil {
		return 0, err
	}
	users := map[string]int64{}
	for _, u := range []struct{ email, name, role string }{
		{"owner@example.com", "Sawyer (owner)", "owner"},
		{"accountant@example.com", "Ada (accountant)", "accountant"},
		{"bookkeeper@example.com", "Bea (bookkeeper)", "bookkeeper"},
		{"viewer@example.com", "Vic (viewer)", "viewer"},
	} {
		user, err := q.CreateUser(ctx, db.CreateUserParams{Email: u.email, Name: u.name})
		if err != nil {
			return 0, err
		}
		if err := q.AddMembership(ctx, db.AddMembershipParams{OrganizationID: org.ID, UserID: user.ID, Role: u.role}); err != nil {
			return 0, err
		}
		users[u.role] = user.ID
	}

	accounts := map[string]int64{}
	for _, a := range []struct{ code, name, accountType, parent string }{
		{"1000", "Cash", "asset", ""},
		{"1200", "Inventory", "asset", ""},
		{"2000", "Loans", "liability", ""},
		{"3000", "Owner Equity", "equity", ""},
		{"4000", "Bagel Sales", "revenue", ""},
		{"5000", "Facilities", "expense", ""},
		{"5100", "Rent", "expense", "5000"},
		{"5200", "Utilities", "expense", "5000"},
	} {
		parentID, hasParent := accounts[a.parent]
		created, err := svc.CreateAccount(ctx, users["owner"], db.CreateAccountParams{
			OrganizationID: org.ID,
			Code:           a.code,
			Name:           a.name,
			Type:           a.accountType,
			ParentID:       sql.NullInt64{Int64: parentID, Valid: hasParent},
		})
		if err != nil {
			return 0, fmt.Errorf("seed account %s: %w", a.code, err)
		}
		accounts[a.code] = created.ID
	}

	for _, p := range []struct {
		date, description, debit, credit string
		cents                            int64
	}{
		{"2026-10-01", "Owner investment", "1000", "3000", 1_000_000_00},
		{"2026-10-02", "Bank loan", "1000", "2000", 500_000_00},
		{"2026-10-03", "Buy inventory", "1200", "1000", 250_000_00},
		{"2026-10-04", "Loan repayment", "2000", "1000", 30_000_00},
	} {
		_, err := svc.Post(ctx, db.InsertTransactionParams{
			OrganizationID: org.ID,
			OccurredOn:     p.date,
			Description:    p.description,
			PostedBy:       users["bookkeeper"],
		}, []db.InsertEntryParams{
			{AccountID: accounts[p.debit], Direction: "debit", Amount: p.cents},
			{AccountID: accounts[p.credit], Direction: "credit", Amount: p.cents},
		})
		if err != nil {
			return 0, fmt.Errorf("seed transaction %q: %w", p.description, err)
		}
	}
	return org.ID, nil
}
