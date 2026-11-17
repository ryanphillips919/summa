package main

import (
	"fmt"
	"net/http"
	"strings"

	"summa/internal/db"
	"summa/internal/ledger"
)

func (a *App) accountsHandler(w http.ResponseWriter, r *http.Request) {
	a.renderAccounts(w, r, "", "", "", http.StatusOK)
}

func (a *App) renderAccounts(w http.ResponseWriter, r *http.Request, name, accountType, message string, status int) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	totals, err := a.queries.AccountTotals(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load accounts.", http.StatusInternalServerError)
		return
	}
	accounts := make([]JournalAccount, 0, len(totals))
	for _, total := range totals {
		accounts = append(accounts, JournalAccount{ID: total.ID, Name: total.Name, Type: total.Type, Balance: ledger.Balance(total.Type, total.Debits, total.Credits)})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := AccountsPage(org.Name, accounts, name, accountType, message).Render(r.Context(), w); err != nil {
		fmt.Printf("render accounts: %v\n", err)
	}
}

func (a *App) createAccountHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not read account.", http.StatusBadRequest)
		return
	}
	name, accountType := strings.TrimSpace(r.FormValue("name")), r.FormValue("type")
	if name == "" || len(name) > 200 {
		a.renderAccounts(w, r, name, accountType, "Enter an account name of up to 200 characters.", http.StatusBadRequest)
		return
	}
	switch accountType {
	case "asset", "liability", "equity", "revenue", "expense":
	default:
		a.renderAccounts(w, r, name, accountType, "Choose an account type.", http.StatusBadRequest)
		return
	}
	_, err := a.queries.CreateAccount(r.Context(), db.CreateAccountParams{OrganizationID: a.orgID, Name: name, Type: accountType})
	if isUniqueViolation(err) {
		a.renderAccounts(w, r, name, accountType, "An account with that name already exists.", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "Could not create account.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}
