package main

import "net/http"

// DashboardData is a presentation-specific view model. For the demo it uses
// sample data; later its values can be assembled from ledger queries and the
// analytics service without changing the template.
type DashboardData struct {
	Organization string
	UserName     string
	Period       string
	Metrics      []DashboardMetric
	CloseTasks   []CloseTask
	Reviews      []ReviewItem
	Aging        []AgingBucket
	Activity     []DashboardActivity
}

type DashboardMetric struct{ Label, Value, Change, Direction, Tone string }
type CloseTask struct{ Label, Detail, State, Mark string }
type ReviewItem struct{ Level, Title, Detail string }
type AgingBucket struct{ Label, Amount, Customers, Tone string }
type DashboardActivity struct{ Date, Kind, Description, Amount, Tone string }

func (a *App) homepageHandler(w http.ResponseWriter, r *http.Request) {
	HomePage(sampleDashboardData()).Render(r.Context(), w)
}

func sampleDashboardData() DashboardData {
	return DashboardData{
		Organization: "Acme Corporation", UserName: "User", Period: "Sep 1, 2026 – Sep 30, 2026",
		Metrics: []DashboardMetric{
			{Label: "Cash position", Value: "$125,430", Change: "+12% vs. last month", Direction: "↑", Tone: "positive"},
			{Label: "Accounts receivable", Value: "$42,800", Change: "8% lower than last month", Direction: "↓", Tone: "positive"},
			{Label: "Accounts payable", Value: "$28,650", Change: "15% lower than last month", Direction: "↓", Tone: "positive"},
			{Label: "Net income", Value: "$36,210", Change: "+18% vs. last month", Direction: "↑", Tone: "positive"},
		},
		CloseTasks: []CloseTask{
			{Label: "Reconcile operating checking", Detail: "2 transactions remaining", State: "in-progress", Mark: ""},
			{Label: "Review outstanding invoices", Detail: "5 invoices need follow-up", State: "in-progress", Mark: ""},
			{Label: "Post recurring journal entries", Detail: "Completed Sep 23", State: "complete", Mark: "✓"},
			{Label: "Lock September books", Detail: "Available after checklist is complete", State: "locked", Mark: ""},
		},
		Reviews: []ReviewItem{
			{Level: "High", Title: "Possible duplicate vendor bill", Detail: "Office Warehouse · $149.99"},
			{Level: "Review", Title: "Unusually large travel expense", Detail: "Travel Hub · $3,200.00"},
			{Level: "Info", Title: "Three bank transactions unmatched", Detail: "Operating Checking"},
		},
		Aging: []AgingBucket{
			{Label: "Current", Amount: "$21,400", Customers: "12 customers", Tone: "current"},
			{Label: "1–30 days", Amount: "$14,200", Customers: "6 customers", Tone: "soon"},
			{Label: "31–60 days", Amount: "$5,800", Customers: "3 customers", Tone: "late"},
			{Label: "Over 60 days", Amount: "$1,400", Customers: "1 customer", Tone: "overdue"},
		},
		Activity: []DashboardActivity{
			{Date: "Sep 24", Kind: "Invoice", Description: "INV-2026-089 sent to Acme Retail Group", Amount: "$12,400.00", Tone: "invoice"},
			{Date: "Sep 24", Kind: "Payment", Description: "Customer payment from Ridgewood LLC", Amount: "$7,800.00", Tone: "payment"},
			{Date: "Sep 23", Kind: "Bill", Description: "AWS Hosting", Amount: "$75.00", Tone: "bill"},
			{Date: "Sep 22", Kind: "Journal entry", Description: "JE-2026-142 — Month-end accrual", Amount: "—", Tone: "journal"},
			{Date: "Sep 21", Kind: "Payment", Description: "Vendor payment to Stripe", Amount: "$1,200.00", Tone: "bill"},
		},
	}
}
