package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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
	for _, a := range []struct {
		code, name, parent string
		typ                ledger.AccountType
	}{
		{"1000", "Cash", "", ledger.Asset},
		{"1200", "Inventory", "", ledger.Asset},
		{"2000", "Loans", "", ledger.Liability},
		{"3000", "Owner Equity", "", ledger.Equity},
		{"4000", "Bagel Sales", "", ledger.Revenue},
		{"5000", "Facilities", "", ledger.Expense},
		{"5100", "Rent", "5000", ledger.Expense},
		{"5200", "Utilities", "5000", ledger.Expense},
	} {
		created, err := svc.CreateAccount(ctx, ledger.CreateAccountRequest{
			OrganizationID: org.ID, ActorID: users["owner"],
			Code: a.code, Name: a.name, Type: a.typ, ParentID: accounts[a.parent],
		})
		if err != nil {
			return 0, fmt.Errorf("seed account %s: %w", a.code, err)
		}
		accounts[a.code] = created.ID
	}

	for _, p := range []struct {
		day           int
		desc          string
		debit, credit string
		cents         ledger.Amount
	}{
		{1, "Owner investment", "1000", "3000", 1_000_000_00},
		{2, "Bank loan", "1000", "2000", 500_000_00},
		{3, "Buy inventory", "1200", "1000", 250_000_00},
		{4, "Loan repayment", "2000", "1000", 30_000_00},
	} {
		if _, err := svc.Post(ctx, ledger.PostRequest{
			OrganizationID: org.ID,
			ActorID:        users["bookkeeper"],
			OccurredOn:     time.Date(2026, time.October, p.day, 0, 0, 0, 0, time.UTC),
			Description:    p.desc,
			Entries: []ledger.Entry{
				{AccountID: accounts[p.debit], Direction: ledger.Debit, Amount: p.cents},
				{AccountID: accounts[p.credit], Direction: ledger.Credit, Amount: p.cents},
			},
		}); err != nil {
			return 0, fmt.Errorf("seed transaction %q: %w", p.desc, err)
		}
	}
	return org.ID, nil
}
