package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"summa/internal/db"
	"summa/internal/invoicing"
	"summa/internal/ledger"
)

// seedDevData creates a fictional company only when the database is empty.
// Payments are separate ledger entries; invoices do not yet track allocations.
func seedDevData(ctx context.Context, conn *sql.DB, q *db.Queries) (int64, error) {
	var existingID int64
	err := conn.QueryRowContext(ctx, "SELECT id FROM organizations ORDER BY id LIMIT 1").Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	org, err := q.CreateOrganization(ctx, "Northstar Equipment & Supply")
	if err != nil {
		return 0, fmt.Errorf("seed organization: %w", err)
	}
	if _, err := q.CreateUser(ctx, db.CreateUserParams{Email: "owner@northstar.example", Name: "Morgan Ellis"}); err != nil {
		return 0, fmt.Errorf("seed user: %w", err)
	}

	accounts := make(map[string]int64)
	for _, item := range []struct{ name, kind string }{
		{"Cash", "asset"}, {"Accounts Receivable", "asset"}, {"Inventory", "asset"},
		{"Equipment", "asset"}, {"Accounts Payable", "liability"}, {"Loan Payable", "liability"},
		{"Owner Equity", "equity"}, {"Sales Revenue", "revenue"}, {"Cost of Goods Sold", "expense"},
		{"Rent Expense", "expense"}, {"Payroll Expense", "expense"}, {"Utilities Expense", "expense"},
		{"Subscriptions Expense", "expense"}, {"Freight Expense", "expense"},
		{"Insurance Expense", "expense"}, {"Repairs Expense", "expense"},
	} {
		account, err := q.CreateAccount(ctx, db.CreateAccountParams{OrganizationID: org.ID, Name: item.name, Type: item.kind})
		if err != nil {
			return 0, fmt.Errorf("seed account %q: %w", item.name, err)
		}
		accounts[item.name] = account.ID
	}

	var customers []db.Customer
	for i, name := range []string{
		"Summit Hospitality", "Canyon Medical", "Evergreen Schools", "Mesa Office Group",
		"Aspen Hotel Group", "Clearview Clinics", "Cedar Community College", "Redstone Facilities",
		"Pioneer Office Services", "Willow Care Center", "Silver Creek Lodge", "Westgate Manufacturing",
	} {
		customer, err := q.CreateCustomer(ctx, db.CreateCustomerParams{OrganizationID: org.ID, Name: name, Email: sql.NullString{String: fmt.Sprintf("customer%02d@example.com", i+1), Valid: true}})
		if err != nil {
			return 0, fmt.Errorf("seed customer %q: %w", name, err)
		}
		customers = append(customers, customer)
	}

	post := func(date, description, debit, credit string, cents int64) error {
		_, err := ledger.Post(ctx, conn, db.CreateTransactionParams{
			OrganizationID: org.ID, Date: date, Description: description,
		}, []db.CreateEntryParams{
			{AccountID: accounts[debit], Direction: "debit", Amount: cents},
			{AccountID: accounts[credit], Direction: "credit", Amount: cents},
		})
		if err != nil {
			return fmt.Errorf("seed transaction %q: %w", description, err)
		}
		return nil
	}

	for _, item := range []struct {
		date, description, debit, credit string
		cents                            int64
	}{
		{"2026-05-01", "Owner investment", "Cash", "Owner Equity", 25_000_000},
		{"2026-05-02", "Equipment financing", "Cash", "Loan Payable", 8_000_000},
		{"2026-05-03", "Purchase delivery equipment", "Equipment", "Cash", 4_000_000},
		{"2026-05-04", "Inventory purchase on credit", "Inventory", "Accounts Payable", 1_800_000},
		{"2026-05-20", "Payment to inventory supplier", "Accounts Payable", "Cash", 600_000},
	} {
		if err := post(item.date, item.description, item.debit, item.credit, item.cents); err != nil {
			return 0, err
		}
	}

	// Each invoice has two lines. The final two remain drafts for the demo.
	invoices := []struct {
		customer        int
		issue, due      string
		equipment       int64
		partsAndService int64
		posted          bool
	}{
		{0, "2026-05-05", "2026-06-04", 800_000, 180_000, true},
		{1, "2026-05-09", "2026-06-08", 1_050_000, 150_000, true},
		{2, "2026-05-18", "2026-06-17", 620_000, 80_000, true},
		{3, "2026-06-04", "2026-07-04", 720_000, 100_000, true},
		{4, "2026-06-12", "2026-07-12", 880_000, 120_000, true},
		{5, "2026-06-21", "2026-07-21", 560_000, 90_000, true},
		{6, "2026-07-03", "2026-08-02", 950_000, 125_000, true},
		{7, "2026-07-14", "2026-08-13", 780_000, 120_000, true},
		{8, "2026-07-23", "2026-08-22", 610_000, 90_000, true},
		{9, "2026-08-04", "2026-09-03", 690_000, 110_000, true},
		{10, "2026-08-13", "2026-09-12", 840_000, 160_000, true},
		{11, "2026-08-24", "2026-09-23", 920_000, 130_000, true},
		{2, "2026-09-03", "2026-10-03", 730_000, 120_000, true},
		{4, "2026-09-11", "2026-10-11", 860_000, 140_000, true},
		{6, "2026-09-22", "2026-10-22", 790_000, 110_000, true},
		{8, "2026-10-05", "2026-11-04", 640_000, 110_000, true},
		{10, "2026-10-13", "2026-11-12", 970_000, 130_000, true},
		{11, "2026-10-22", "2026-11-21", 890_000, 110_000, true},
		{3, "2026-10-25", "2026-11-24", 580_000, 70_000, false},
		{5, "2026-10-28", "2026-11-27", 680_000, 120_000, false},
	}
	for i, item := range invoices {
		invoice, err := q.CreateInvoice(ctx, db.CreateInvoiceParams{
			OrganizationID: org.ID, CustomerID: customers[item.customer].ID,
			Number: fmt.Sprintf("INV-%03d", i+1), IssueDate: item.issue, DueDate: item.due,
		})
		if err != nil {
			return 0, fmt.Errorf("seed invoice %d: %w", i+1, err)
		}
		for _, line := range []struct {
			description string
			cents       int64
		}{{"Commercial equipment", item.equipment}, {"Parts and maintenance service", item.partsAndService}} {
			if _, err := q.CreateInvoiceLine(ctx, db.CreateInvoiceLineParams{
				OrganizationID: org.ID, InvoiceID: invoice.ID, Description: line.description,
				Quantity: 1, UnitPriceCents: line.cents,
			}); err != nil {
				return 0, fmt.Errorf("seed invoice %s line: %w", invoice.Number, err)
			}
		}
		if item.posted {
			if _, err := invoicing.PostInvoice(ctx, conn, org.ID, invoice.ID, accounts["Accounts Receivable"], accounts["Sales Revenue"]); err != nil {
				return 0, fmt.Errorf("post seed invoice %s: %w", invoice.Number, err)
			}
		}
	}

	// Separate receipts and expenses are balanced ledger transactions, not
	// payment allocations against particular invoice records.
	if err := post("2026-05-28", "Canyon Medical customer payment", "Cash", "Accounts Receivable", 500_000); err != nil {
		return 0, err
	}
	for month := 5; month <= 10; month++ {
		for _, item := range []struct {
			day                        int
			description, debit, credit string
			cents                      int64
		}{
			{1, "Monthly rent", "Rent Expense", "Cash", 750_000},
			{3, "Monthly payroll", "Payroll Expense", "Cash", 1_800_000},
			{5, "Monthly utilities", "Utilities Expense", "Cash", 105_000},
			{7, "Monthly subscriptions", "Subscriptions Expense", "Cash", 37_500},
			{9, "Monthly freight", "Freight Expense", "Cash", 275_000},
			{11, "Monthly insurance", "Insurance Expense", "Cash", 160_000},
			{13, "Customer receipts (unallocated)", "Cash", "Accounts Receivable", int64(1_500_000 + (month-5)*100_000)},
			{15, "Inventory replenishment on credit", "Inventory", "Accounts Payable", 1_400_000},
			{17, "Payment to suppliers", "Accounts Payable", "Cash", 1_000_000},
			{19, "Cost of equipment sold", "Cost of Goods Sold", "Inventory", 1_100_000},
			{23, "Equipment maintenance", "Repairs Expense", "Cash", 80_000},
			{26, "Loan principal repayment", "Loan Payable", "Cash", 150_000},
		} {
			if err := post(fmt.Sprintf("2026-%02d-%02d", month, item.day), item.description, item.debit, item.credit, item.cents); err != nil {
				return 0, err
			}
		}
	}
	for _, item := range []struct {
		date, description string
		cents             int64
	}{
		{"2026-10-20", "Software renewal", 124_999},
		{"2026-10-20", "Software renewal", 124_999},
	} {
		if err := post(item.date, item.description, "Subscriptions Expense", "Cash", item.cents); err != nil {
			return 0, err
		}
	}
	if err := post("2026-10-24", "Major warehouse equipment repair", "Repairs Expense", "Cash", 1_850_000); err != nil {
		return 0, err
	}
	return org.ID, nil
}
