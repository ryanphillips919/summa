package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"summa/internal/db"
	"summa/internal/payables"
)

type BillPageData struct {
	Organization      string
	Bill              payables.Bill
	Accounts          []db.Account
	Payments          []payables.Payment
	RequestKey, Today string
}

func (a *App) billsHandler(w http.ResponseWriter, r *http.Request) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", 500)
		return
	}
	bills, err := payables.List(r.Context(), a.sqlDB, a.orgID)
	if err != nil {
		http.Error(w, "Could not load bills.", 500)
		return
	}
	if err := BillsPage(org.Name, bills).Render(r.Context(), w); err != nil {
		fmt.Printf("render bills: %v\n", err)
	}
}

func (a *App) newBillHandler(w http.ResponseWriter, r *http.Request) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", 500)
		return
	}
	vendors, err := a.queries.ListVendors(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load vendors.", 500)
		return
	}
	accounts, err := a.queries.ListAccounts(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load accounts.", 500)
		return
	}
	key, err := GenerateSessionToken()
	if err != nil {
		http.Error(w, "Could not prepare form.", 500)
		return
	}
	if err := NewBillPage(org.Name, vendors, accounts, time.Now().Format("2006-01-02"), key).Render(r.Context(), w); err != nil {
		fmt.Printf("render new bill: %v\n", err)
	}
}

func (a *App) createBillHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not read bill.", 400)
		return
	}
	vendor, e1 := strconv.ParseInt(r.FormValue("vendor_id"), 10, 64)
	expense, e2 := strconv.ParseInt(r.FormValue("expense_account_id"), 10, 64)
	payable, e3 := strconv.ParseInt(r.FormValue("payable_account_id"), 10, 64)
	amount, e4 := parseCents(r.FormValue("amount"))
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || vendor < 1 || expense < 1 || payable < 1 || amount <= 0 {
		http.Error(w, "Choose a vendor and accounts and enter a positive amount.", 400)
		return
	}
	id, err := payables.Create(r.Context(), a.sqlDB, payables.NewBill{OrganizationID: a.orgID, VendorID: vendor, Number: r.FormValue("number"), IssueDate: r.FormValue("issue_date"), DueDate: r.FormValue("due_date"), Description: r.FormValue("description"), Amount: amount, ExpenseAccountID: expense, PayableAccountID: payable, RequestKey: r.FormValue("request_key")})
	if err != nil {
		http.Error(w, "Could not post bill: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	a.notifyLedgerChanged()
	http.Redirect(w, r, fmt.Sprintf("/bills/%d", id), http.StatusSeeOther)
}

func (a *App) billDetailHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "billID"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	bill, err := payables.Get(r.Context(), a.sqlDB, a.orgID, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Could not load bill.", 500)
		return
	}
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", 500)
		return
	}
	accounts, err := a.queries.ListAccounts(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load accounts.", 500)
		return
	}
	payments, err := payables.Payments(r.Context(), a.sqlDB, a.orgID, id)
	if err != nil {
		http.Error(w, "Could not load payments.", 500)
		return
	}
	key, err := GenerateSessionToken()
	if err != nil {
		http.Error(w, "Could not prepare form.", 500)
		return
	}
	today := time.Now().Format("2006-01-02")
	if today < bill.IssueDate {
		today = bill.IssueDate
	}
	if err := BillDetailPage(BillPageData{Organization: org.Name, Bill: bill, Accounts: accounts, Payments: payments, RequestKey: key, Today: today}).Render(r.Context(), w); err != nil {
		fmt.Printf("render bill: %v\n", err)
	}
}

func (a *App) payBillHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "billID"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not read payment.", 400)
		return
	}
	account, e1 := strconv.ParseInt(r.FormValue("payment_account_id"), 10, 64)
	amount, e2 := parseCents(r.FormValue("amount"))
	if e1 != nil || e2 != nil || account < 1 || amount <= 0 {
		http.Error(w, "Choose an account and enter a positive amount.", 400)
		return
	}
	_, err = payables.Pay(r.Context(), a.sqlDB, a.orgID, id, account, amount, r.FormValue("date"), r.FormValue("request_key"))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Could not record payment: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	a.notifyLedgerChanged()
	http.Redirect(w, r, fmt.Sprintf("/bills/%d", id), http.StatusSeeOther)
}
