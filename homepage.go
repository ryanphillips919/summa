package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"summa/internal/analytics"
	"summa/internal/ledger"
)

// DashboardData contains values derived from the organization's ledger.
type DashboardData struct {
	Organization                 string
	Cash, Receivables, Payables  int64
	Revenue, Expenses, NetIncome int64
	Trends                       []MonthlyTrend
	CashFlow                     []CashFlowMonth
	CashFlowScale                int64
	Tasks                        []DashboardTask
	Activity                     []JournalTransaction
	Invoices                     []InvoiceSummary
}

type MonthlyTrend struct {
	Month                  string
	Revenue, Expenses, Net int64
}

// CashFlowMonth is a display-friendly aggregation of posted Cash entries.
type CashFlowMonth struct {
	Month                       string
	Inflow, Outflow             int64
	InflowHeight, OutflowHeight string
}

type DashboardTask struct {
	Label, Detail, Href, Mark string
	Complete                  bool
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

	// Database-only read; this does not run the Python forecasting engine.
	snapshot, err := a.cashDataSource.LoadCashSnapshot(ctx)
	if err != nil {
		return result, err
	}
	result.CashFlow, result.CashFlowScale = dashboardCashFlow(snapshot.Movements)

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

	draftCount := 0
	for _, invoice := range result.Invoices {
		if !invoice.Invoice.PostedTransactionID.Valid {
			draftCount++
		}
	}

	if draftCount > 0 {
		result.Tasks = append(result.Tasks, DashboardTask{
			Label:  fmt.Sprintf("Finish %d draft invoice%s", draftCount, pluralSuffix(draftCount)),
			Detail: "Add any remaining lines, then post it to the ledger.",
			Href:   "/invoices",
		})
	} else {
		result.Tasks = append(result.Tasks, DashboardTask{
			Label:    "Invoices are up to date",
			Detail:   "All invoices in this workspace are posted.",
			Href:     "/invoices",
			Mark:     "✓",
			Complete: true,
		})
	}

	result.Tasks = append(result.Tasks,
		DashboardTask{
			Label:  "Review cash outlook",
			Detail: "See the forecast and items that need attention.",
			Href:   "/analytics",
		},
		DashboardTask{
			Label:  "Record a journal adjustment",
			Detail: "Use the balanced entry form for an accounting correction.",
			Href:   "/journal?adjust=1",
		},
	)

	if len(result.Invoices) > 5 {
		result.Invoices = result.Invoices[:5]
	}

	return result, nil
}

func dashboardCashFlow(movements []analytics.CashMovement) ([]CashFlowMonth, int64) {
	if len(movements) == 0 {
		return nil, 0
	}

	var latest time.Time
	for _, movement := range movements {
		if len(movement.Date) < len("2006-01-02") {
			continue
		}
		movementDate, err := time.Parse("2006-01-02", movement.Date[:10])
		if err == nil && movementDate.After(latest) {
			latest = movementDate
		}
	}
	if latest.IsZero() {
		return nil, 0
	}

	// Show the latest four complete calendar months available in the snapshot.
	firstMonth := time.Date(
		latest.Year(),
		latest.Month(),
		1,
		0, 0, 0, 0,
		time.UTC,
	).AddDate(0, -3, 0)

	months := make([]CashFlowMonth, 4)
	byMonth := make(map[string]int, len(months))
	for index := range months {
		month := firstMonth.AddDate(0, index, 0)
		months[index].Month = month.Format("Jan")
		byMonth[month.Format("2006-01")] = index
	}

	maxAmount := int64(0)
	for _, movement := range movements {
		if len(movement.Date) < len("2006-01") {
			continue
		}
		index, found := byMonth[movement.Date[:len("2006-01")]]
		if !found {
			continue
		}

		amount := int64(math.Round(movement.Amount * 100))
		if amount >= 0 {
			months[index].Inflow += amount
			maxAmount = max(maxAmount, months[index].Inflow)
		} else {
			months[index].Outflow -= amount
			maxAmount = max(maxAmount, months[index].Outflow)
		}
	}

	for index := range months {
		months[index].InflowHeight = dashboardBarHeight(
			months[index].Inflow,
			maxAmount,
		)
		months[index].OutflowHeight = dashboardBarHeight(
			months[index].Outflow,
			maxAmount,
		)
	}

	return months, maxAmount
}

func dashboardBarHeight(amount, maxAmount int64) string {
	if amount <= 0 || maxAmount <= 0 {
		return "h-0"
	}
	percent := int((amount*100 + maxAmount - 1) / maxAmount)
	percent = min(100, ((percent+9)/10)*10)
	return fmt.Sprintf("h-%d", percent)
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
