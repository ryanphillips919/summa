package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"summa/internal/invoicing"
	"summa/internal/ledger"
	"summa/internal/payables"
)

func (a *App) billAgingHandler(w http.ResponseWriter, r *http.Request) {
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		asOf = time.Now().Format("2006-01-02")
	}
	date, err := time.Parse("2006-01-02", asOf)
	if err != nil || date.Format("2006-01-02") != asOf {
		http.Error(w, "Enter a valid report date.", 400)
		return
	}
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", 500)
		return
	}
	report, err := payables.BuildAging(r.Context(), a.sqlDB, a.orgID, asOf)
	if err != nil {
		fmt.Printf("bill aging: %v\n", err)
		http.Error(w, "Could not build bill aging.", 500)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="bill-aging-`+asOf+`.csv"`)
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"Bill", "Vendor", "Due date", "Current", "1-30 days", "31-60 days", "61-90 days", "Over 90 days", "Outstanding"})
		for _, row := range report.Rows {
			_ = writer.Write([]string{csvSafeText(row.Number), csvSafeText(row.Vendor), row.DueDate, centsCSV(row.Current), centsCSV(row.Days1To30), centsCSV(row.Days31To60), centsCSV(row.Days61To90), centsCSV(row.Over90), centsCSV(row.Outstanding)})
		}
		_ = writer.Write([]string{"Total", "", "", centsCSV(report.Current), centsCSV(report.Days1To30), centsCSV(report.Days31To60), centsCSV(report.Days61To90), centsCSV(report.Over90), centsCSV(report.Outstanding)})
		_ = writer.Write([]string{"Note: Direct ledger entries not recorded as bills are not included."})
		writer.Flush()
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := BillAgingPage(org.Name, report).Render(r.Context(), w); err != nil {
		fmt.Printf("render bill aging: %v\n", err)
	}
}

func (a *App) generalLedgerHandler(w http.ResponseWriter, r *http.Request) {
	today := time.Now()
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" {
		from = fmt.Sprintf("%04d-01-01", today.Year())
	}
	if to == "" {
		to = today.Format("2006-01-02")
	}
	start, startErr := time.Parse("2006-01-02", from)
	end, endErr := time.Parse("2006-01-02", to)
	if startErr != nil || endErr != nil || start.Format("2006-01-02") != from || end.Format("2006-01-02") != to || end.Before(start) {
		http.Error(w, "Enter a valid date range.", http.StatusBadRequest)
		return
	}
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	accounts, err := a.queries.ListAccounts(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load accounts.", http.StatusInternalServerError)
		return
	}
	if len(accounts) == 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = GeneralLedgerPage(org.Name, accounts, nil, from, to).Render(r.Context(), w)
		return
	}
	accountID := accounts[0].ID
	if text := r.URL.Query().Get("account_id"); text != "" {
		accountID, err = strconv.ParseInt(text, 10, 64)
		if err != nil || accountID < 1 {
			http.Error(w, "Choose a valid account.", http.StatusBadRequest)
			return
		}
	}
	report, err := ledger.BuildGeneralLedger(r.Context(), a.sqlDB, a.orgID, accountID, from, to)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fmt.Printf("general ledger: %v\n", err)
		http.Error(w, "Could not build general ledger.", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="general-ledger-`+from+`-`+to+`.csv"`)
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"Account", "Date", "Journal entry", "Description", "Debit", "Credit", "Balance"})
		_ = writer.Write([]string{csvSafeText(report.Account.Name), from, "", "Opening balance", "", "", centsCSV(report.Opening)})
		for _, line := range report.Lines {
			_ = writer.Write([]string{csvSafeText(report.Account.Name), line.Date, fmt.Sprintf("JE-%d", line.TransactionID), csvSafeText(line.Description), centsCSV(line.Debit), centsCSV(line.Credit), centsCSV(line.Balance)})
		}
		_ = writer.Write([]string{csvSafeText(report.Account.Name), to, "", "Closing balance", "", "", centsCSV(report.Closing)})
		writer.Flush()
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := GeneralLedgerPage(org.Name, accounts, &report, from, to).Render(r.Context(), w); err != nil {
		fmt.Printf("render general ledger: %v\n", err)
	}
}

func (a *App) invoiceAgingHandler(w http.ResponseWriter, r *http.Request) {
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		asOf = time.Now().Format("2006-01-02")
	}
	date, err := time.Parse("2006-01-02", asOf)
	if err != nil || date.Format("2006-01-02") != asOf {
		http.Error(w, "Enter a valid report date.", http.StatusBadRequest)
		return
	}
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	report, err := invoicing.BuildAging(r.Context(), a.sqlDB, a.orgID, asOf)
	if err != nil {
		fmt.Printf("invoice aging: %v\n", err)
		http.Error(w, "Could not build invoice aging.", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="invoice-aging-`+asOf+`.csv"`)
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"Invoice", "Customer", "Due date", "Current", "1-30 days", "31-60 days", "61-90 days", "Over 90 days", "Outstanding"})
		for _, row := range report.Rows {
			_ = writer.Write([]string{csvSafeText(row.Number), csvSafeText(row.Customer), row.DueDate, centsCSV(row.Current), centsCSV(row.Days1To30), centsCSV(row.Days31To60), centsCSV(row.Days61To90), centsCSV(row.Over90), centsCSV(row.Outstanding)})
		}
		_ = writer.Write([]string{"Total", "", "", centsCSV(report.Current), centsCSV(report.Days1To30), centsCSV(report.Days31To60), centsCSV(report.Days61To90), centsCSV(report.Over90), centsCSV(report.Outstanding)})
		_ = writer.Write([]string{"Note: Earlier unallocated ledger receipts are not included."})
		writer.Flush()
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := InvoiceAgingPage(org.Name, report).Render(r.Context(), w); err != nil {
		fmt.Printf("render invoice aging: %v\n", err)
	}
}

func (a *App) trialBalanceHandler(w http.ResponseWriter, r *http.Request) {
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		asOf = time.Now().Format("2006-01-02")
	}
	date, err := time.Parse("2006-01-02", asOf)
	if err != nil || date.Format("2006-01-02") != asOf {
		http.Error(w, "Enter a valid report date.", http.StatusBadRequest)
		return
	}
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	report, err := ledger.BuildTrialBalance(r.Context(), a.sqlDB, a.orgID, asOf)
	if err != nil {
		fmt.Printf("trial balance: %v\n", err)
		http.Error(w, "Could not build a balanced trial balance.", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="trial-balance-`+asOf+`.csv"`)
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"Account", "Type", "Debit", "Credit"})
		for _, row := range report.Rows {
			_ = writer.Write([]string{csvSafeText(row.Name), csvSafeText(row.Type), centsCSV(row.Debit), centsCSV(row.Credit)})
		}
		_ = writer.Write([]string{"Total", "", centsCSV(report.TotalDebit), centsCSV(report.TotalCredit)})
		writer.Flush()
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := TrialBalancePage(org.Name, report).Render(r.Context(), w); err != nil {
		fmt.Printf("render trial balance: %v\n", err)
	}
}

func centsCSV(cents int64) string {
	sign, amount := "", uint64(cents)
	if cents < 0 {
		sign = "-"
		amount = uint64(-(cents + 1)) + 1
	}
	return fmt.Sprintf("%s%d.%02d", sign, amount/100, amount%100)
}

func (a *App) incomeStatementHandler(w http.ResponseWriter, r *http.Request) {
	today := time.Now()
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" {
		from = fmt.Sprintf("%04d-01-01", today.Year())
	}
	if to == "" {
		to = today.Format("2006-01-02")
	}
	start, startErr := time.Parse("2006-01-02", from)
	end, endErr := time.Parse("2006-01-02", to)
	if startErr != nil || endErr != nil || start.Format("2006-01-02") != from || end.Format("2006-01-02") != to || end.Before(start) {
		http.Error(w, "Enter a valid date range.", http.StatusBadRequest)
		return
	}
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	report, err := ledger.BuildIncomeStatement(r.Context(), a.sqlDB, a.orgID, from, to)
	if err != nil {
		fmt.Printf("profit and loss: %v\n", err)
		http.Error(w, "Could not build profit and loss.", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="profit-and-loss-`+from+`-`+to+`.csv"`)
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"Account", "Type", "Amount"})
		for _, row := range report.Rows {
			_ = writer.Write([]string{csvSafeText(row.Name), csvSafeText(row.Type), centsCSV(row.Amount)})
		}
		_ = writer.Write([]string{"Total revenue", "", centsCSV(report.Revenue)})
		_ = writer.Write([]string{"Total expenses", "", centsCSV(report.Expenses)})
		_ = writer.Write([]string{"Net income", "", centsCSV(report.NetIncome)})
		writer.Flush()
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := IncomeStatementPage(org.Name, report).Render(r.Context(), w); err != nil {
		fmt.Printf("render profit and loss: %v\n", err)
	}
}

func (a *App) balanceSheetHandler(w http.ResponseWriter, r *http.Request) {
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		asOf = time.Now().Format("2006-01-02")
	}
	date, err := time.Parse("2006-01-02", asOf)
	if err != nil || date.Format("2006-01-02") != asOf {
		http.Error(w, "Enter a valid report date.", http.StatusBadRequest)
		return
	}
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	report, err := ledger.BuildBalanceSheet(r.Context(), a.sqlDB, a.orgID, asOf)
	if err != nil {
		fmt.Printf("balance sheet: %v\n", err)
		http.Error(w, "Could not build a balanced balance sheet.", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="balance-sheet-`+asOf+`.csv"`)
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"Account", "Type", "Amount"})
		for _, row := range report.Rows {
			_ = writer.Write([]string{csvSafeText(row.Name), csvSafeText(row.Type), centsCSV(row.Amount)})
		}
		_ = writer.Write([]string{"Total assets", "", centsCSV(report.Assets)})
		_ = writer.Write([]string{"Total liabilities", "", centsCSV(report.Liabilities)})
		_ = writer.Write([]string{"Total equity accounts", "", centsCSV(report.Equity)})
		_ = writer.Write([]string{"Accumulated income", "", centsCSV(report.AccumulatedIncome)})
		_ = writer.Write([]string{"Total liabilities and equity", "", centsCSV(report.TotalLiabilitiesAndEquity)})
		writer.Flush()
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := BalanceSheetPage(org.Name, report).Render(r.Context(), w); err != nil {
		fmt.Printf("render balance sheet: %v\n", err)
	}
}

// Spreadsheet programs can execute formulas in imported text cells.
func csvSafeText(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") || strings.HasPrefix(s, "\n") || (len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0]))) {
		return "'" + s
	}
	return s
}

// monthlyDocument uses the latest transaction date when no reporting date is supplied.
func (a *App) monthlyDocument(ctx context.Context, asOf time.Time) (ledger.LedgerDocument, error) {
	if asOf.IsZero() {
		transactions, err := a.queries.ListTransactions(ctx, a.orgID)
		if err != nil {
			return ledger.LedgerDocument{}, err
		}
		asOf = time.Now()
		if len(transactions) > 0 {
			asOf, err = time.Parse("2006-01-02", transactions[0].Date)
			if err != nil {
				return ledger.LedgerDocument{}, fmt.Errorf("invalid ledger date: %w", err)
			}
		}
	}
	dates := make([]ledger.Dates, 6)
	for i := range dates {
		month := time.Date(asOf.Year(), asOf.Month()-time.Month(5-i), 1, 0, 0, 0, 0, time.UTC)
		dates[i] = ledger.Dates{Date: month.AddDate(0, 1, -1)}
	}
	return ledger.BuildDocument(ctx, a.sqlDB, a.orgID, dates)
}

func monthlyNet(document ledger.LedgerDocument, column int) int64 {
	var total int64
	for _, row := range document.Rows {
		total += row.Data[column]
	}
	return total
}
