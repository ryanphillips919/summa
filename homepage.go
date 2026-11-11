package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"summa/internal/ledger"
)

// DashboardData contains values derived from the organization's ledger.
type DashboardData struct {
	Organization                 string
	Cash, Receivables, Payables  int64
	Revenue, Expenses, NetIncome int64
	Trends                       []MonthlyTrend
	Activity                     []JournalTransaction
	Invoices                     []InvoiceSummary
}

type MonthlyTrend struct {
	Month                  string
	Revenue, Expenses, Net int64
}

func (a *App) homepageHandler(w http.ResponseWriter, r *http.Request) {
	data, err := a.loadDashboard(r.Context(), time.Time{})
	if err != nil {
		http.Error(w, "could not load dashboard", http.StatusInternalServerError)
		return
	}
	if err := HomePage(data).Render(r.Context(), w); err != nil {
		fmt.Printf("render dashboard: %v\n", err)
	}
}

func (a *App) loadDashboard(ctx context.Context, asOf time.Time) (DashboardData, error) {
	var result DashboardData
	org, err := a.queries.GetOrganization(ctx, a.orgID)
	if err != nil {
		return result, err
	}
	result.Organization = org.Name
	totals, err := a.queries.AccountTotals(ctx, a.orgID)
	if err != nil {
		return result, err
	}
	for _, total := range totals {
		balance := ledger.Balance(total.Type, total.Debits, total.Credits)
		switch total.Name {
		case "Cash":
			result.Cash += balance
		case "Accounts Receivable":
			result.Receivables += balance
		case "Accounts Payable":
			result.Payables += balance
		}
		switch total.Type {
		case "revenue":
			result.Revenue += balance
		case "expense":
			result.Expenses += balance
		}
	}
	result.NetIncome = result.Revenue - result.Expenses

	document, err := a.monthlyDocument(ctx, asOf)
	if err != nil {
		return result, err
	}
	for i, date := range document.Dates {
		trend := MonthlyTrend{Month: date.Date.Format("Jan 2006")}
		for _, row := range document.Rows {
			if row.Account.Type == "revenue" {
				trend.Revenue += row.Data[i]
			} else if row.Account.Type == "expense" {
				trend.Expenses -= row.Data[i]
			}
		}
		trend.Net = trend.Revenue - trend.Expenses
		result.Trends = append(result.Trends, trend)
	}
	result.Activity, err = a.journalTransactions(ctx)
	if err != nil {
		return result, err
	}
	if len(result.Activity) > 5 {
		result.Activity = result.Activity[:5]
	}
	result.Invoices, err = a.invoiceSummaries(ctx)
	if err != nil {
		return result, err
	}
	if len(result.Invoices) > 5 {
		result.Invoices = result.Invoices[:5]
	}
	return result, nil
}
