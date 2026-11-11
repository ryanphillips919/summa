package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"summa/internal/db"
	"summa/internal/ledger"
)

// JournalPageData is the read model for the real, double-entry general journal.
// It deliberately contains derived balances and posted entries, never mutable totals.
type JournalPageData struct {
	Organization string
	Transactions []JournalTransaction
	Accounts     []JournalAccount
	Form         JournalForm
	Posted       bool
}

type JournalTransaction struct {
	ID          int64
	Date        string
	Description string
	Entries     []JournalEntry
	Total       int64
}

type JournalEntry struct {
	AccountName string
	Direction   string
	Amount      int64
}

type JournalAccount struct {
	ID      int64
	Name    string
	Type    string
	Balance int64
}

type JournalForm struct {
	Open        bool
	Date        string
	Description string
	Lines       []JournalFormLine
	Error       string
}

type JournalFormLine struct {
	AccountID string
	Debit     string
	Credit    string
}

func emptyJournalForm() JournalForm {
	return JournalForm{
		Date:  time.Now().Format("2006-01-02"),
		Lines: []JournalFormLine{{}, {}},
	}
}

func (a *App) journalHandler(w http.ResponseWriter, r *http.Request) {
	form := emptyJournalForm()
	form.Open = r.URL.Query().Get("adjust") == "1"
	a.renderJournal(w, r, form, r.URL.Query().Get("posted") == "1", http.StatusOK)
}

func (a *App) journalPostHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read journal entry", http.StatusBadRequest)
		return
	}

	form := JournalForm{
		Open:        true,
		Date:        strings.TrimSpace(r.FormValue("date")),
		Description: strings.TrimSpace(r.FormValue("description")),
	}
	accountIDs, debits, credits := r.Form["account_id"], r.Form["debit"], r.Form["credit"]
	if len(accountIDs) != len(debits) || len(accountIDs) != len(credits) {
		form.Error = "Each adjustment line needs an account, debit, and credit field."
		a.renderJournal(w, r, form, false, http.StatusUnprocessableEntity)
		return
	}
	for i := range accountIDs {
		form.Lines = append(form.Lines, JournalFormLine{AccountID: accountIDs[i], Debit: debits[i], Credit: credits[i]})
	}

	date, err := time.Parse("2006-01-02", form.Date)
	if err != nil || date.Format("2006-01-02") != form.Date {
		form.Error = "Enter a valid transaction date."
		a.renderJournal(w, r, form, false, http.StatusUnprocessableEntity)
		return
	}
	if form.Description == "" {
		form.Error = "Add a short description for this adjustment."
		a.renderJournal(w, r, form, false, http.StatusUnprocessableEntity)
		return
	}

	accounts, err := a.queries.ListAccounts(r.Context(), a.orgID)
	if err != nil {
		a.journalServerError(w, err)
		return
	}
	allowed := make(map[int64]struct{}, len(accounts))
	for _, account := range accounts {
		allowed[account.ID] = struct{}{}
	}

	entries := make([]db.CreateEntryParams, 0, len(form.Lines))
	for _, line := range form.Lines {
		accountID, accountErr := strconv.ParseInt(line.AccountID, 10, 64)
		debit, debitErr := parseCents(line.Debit)
		credit, creditErr := parseCents(line.Credit)
		if accountErr != nil || accountID < 1 {
			form.Error = "Choose an account for every adjustment line."
			break
		}
		if _, exists := allowed[accountID]; !exists {
			form.Error = "One of the selected accounts is not available to this organization."
			break
		}
		if debitErr != nil || creditErr != nil || (debit > 0 && credit > 0) || (debit == 0 && credit == 0) {
			form.Error = "Enter a positive amount on exactly one side of each line."
			break
		}
		if debit > 0 {
			entries = append(entries, db.CreateEntryParams{AccountID: accountID, Direction: "debit", Amount: debit})
		} else {
			entries = append(entries, db.CreateEntryParams{AccountID: accountID, Direction: "credit", Amount: credit})
		}
	}
	if form.Error != "" {
		a.renderJournal(w, r, form, false, http.StatusUnprocessableEntity)
		return
	}
	if len(entries) < 2 {
		form.Error = "An adjustment needs at least two lines."
		a.renderJournal(w, r, form, false, http.StatusUnprocessableEntity)
		return
	}
	if _, err := ledger.Post(r.Context(), a.sqlDB, db.CreateTransactionParams{
		OrganizationID: a.orgID,
		Date:           form.Date,
		Description:    form.Description,
	}, entries); err != nil {
		form.Error = "This adjustment could not be posted: " + err.Error()
		a.renderJournal(w, r, form, false, http.StatusUnprocessableEntity)
		return
	}

	a.notifyLedgerChanged()
	http.Redirect(w, r, "/journal?posted=1", http.StatusSeeOther)
}

func (a *App) renderJournal(w http.ResponseWriter, r *http.Request, form JournalForm, posted bool, status int) {
	data, err := a.journalData(r, form, posted)
	if err != nil {
		a.journalServerError(w, err)
		return
	}
	w.WriteHeader(status)
	if err := JournalPage(data).Render(r.Context(), w); err != nil {
		fmt.Printf("render journal: %v\n", err)
	}
}

func (a *App) journalData(r *http.Request, form JournalForm, posted bool) (JournalPageData, error) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		return JournalPageData{}, err
	}
	transactions, err := a.journalTransactions(r.Context())
	if err != nil {
		return JournalPageData{}, err
	}
	totals, err := a.queries.AccountTotals(r.Context(), a.orgID)
	if err != nil {
		return JournalPageData{}, err
	}
	view := JournalPageData{Organization: org.Name, Form: form, Posted: posted, Transactions: transactions}
	for _, total := range totals {
		view.Accounts = append(view.Accounts, JournalAccount{
			ID: total.ID, Name: total.Name, Type: total.Type,
			Balance: ledger.Balance(total.Type, total.Debits, total.Credits),
		})
	}

	return view, nil
}

// journalTransactions is shared by the journal, timeline, and dashboard.
func (a *App) journalTransactions(ctx context.Context) ([]JournalTransaction, error) {
	accounts, err := a.queries.ListAccounts(ctx, a.orgID)
	if err != nil {
		return nil, err
	}
	accountByID := make(map[int64]db.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	transactions, err := a.queries.ListTransactions(ctx, a.orgID)
	if err != nil {
		return nil, err
	}
	var result []JournalTransaction
	for _, transaction := range transactions {
		entries, err := a.queries.ListEntries(ctx, transaction.ID)
		if err != nil {
			return nil, err
		}
		journalTransaction := JournalTransaction{ID: transaction.ID, Date: transaction.Date, Description: transaction.Description}
		for _, entry := range entries {
			account := accountByID[entry.AccountID]
			journalTransaction.Entries = append(journalTransaction.Entries, JournalEntry{AccountName: account.Name, Direction: entry.Direction, Amount: entry.Amount})
			if entry.Direction == "debit" {
				journalTransaction.Total += entry.Amount
			}
		}
		result = append(result, journalTransaction)
	}
	return result, nil
}

func (a *App) journalServerError(w http.ResponseWriter, err error) {
	fmt.Printf("load journal: %v\n", err)
	http.Error(w, "could not load the general journal", http.StatusInternalServerError)
}

var moneyPattern = regexp.MustCompile(`^\d+(?:\.\d{1,2})?$`)

func parseCents(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if !moneyPattern.MatchString(value) {
		return 0, fmt.Errorf("invalid amount")
	}
	parts := strings.SplitN(value, ".", 2)
	dollars, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || dollars > (1<<63-1)/100 {
		return 0, fmt.Errorf("amount is too large")
	}
	cents := int64(0)
	if len(parts) == 2 {
		fraction := parts[1] + "0"
		cents, err = strconv.ParseInt(fraction[:2], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid amount")
		}
	}
	if dollars > (1<<63-1-cents)/100 {
		return 0, fmt.Errorf("amount is too large")
	}
	return dollars*100 + cents, nil
}

func formatDollars(cents int64) string {
	sign := ""
	amount := uint64(cents)
	if cents < 0 {
		sign = "-"
		amount = uint64(-(cents + 1)) + 1
	}
	dollars := strconv.FormatUint(amount/100, 10)
	for i := len(dollars) - 3; i > 0; i -= 3 {
		dollars = dollars[:i] + "," + dollars[i:]
	}
	return fmt.Sprintf("%s$%s.%02d", sign, dollars, amount%100)
}
