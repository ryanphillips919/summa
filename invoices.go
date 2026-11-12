package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"summa/internal/db"
	"summa/internal/invoicing"

	"github.com/go-chi/chi/v5"
)

type InvoiceSummary struct {
	Invoice  db.Invoice
	Customer string
	Total    int64
}

type InvoiceDetailData struct {
	Organization string
	Summary      InvoiceSummary
	Lines        []db.InvoiceLine
	Accounts     []db.Account
	Transaction  *JournalTransaction
}

func invoiceStatus(invoice db.Invoice) string {
	if invoice.PostedTransactionID.Valid {
		return "Posted"
	}
	return "Draft"
}

func (a *App) invoiceSummaries(ctx context.Context) ([]InvoiceSummary, error) {
	invoices, err := a.queries.ListInvoices(ctx, a.orgID)
	if err != nil {
		return nil, err
	}
	customers, err := a.queries.ListCustomers(ctx, a.orgID)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(customers))
	for _, customer := range customers {
		names[customer.ID] = customer.Name
	}
	var summaries []InvoiceSummary
	for _, invoice := range invoices {
		total, err := a.queries.InvoiceTotal(ctx, db.InvoiceTotalParams{OrganizationID: a.orgID, InvoiceID: invoice.ID})
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, InvoiceSummary{Invoice: invoice, Customer: names[invoice.CustomerID], Total: total})
	}
	return summaries, nil
}

func (a *App) customersHandler(w http.ResponseWriter, r *http.Request) {
	a.renderCustomers(w, r, "", "", "", http.StatusOK)
}

func (a *App) renderCustomers(w http.ResponseWriter, r *http.Request, name, email, message string, status int) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	customers, err := a.queries.ListCustomers(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load customers.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := CustomersPage(org.Name, customers, name, email, message).Render(r.Context(), w); err != nil {
		fmt.Printf("render customers: %v\n", err)
	}
}

func (a *App) createCustomerHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not read the customer form.", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	message := ""
	if name == "" || len(name) > 200 {
		message = "Enter a customer name of up to 200 characters."
	} else if email != "" {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email || len(email) > 254 {
			message = "Enter a valid email address."
		}
	}
	if message != "" {
		a.renderCustomers(w, r, name, email, message, http.StatusBadRequest)
		return
	}
	_, err := a.queries.CreateCustomer(r.Context(), db.CreateCustomerParams{
		OrganizationID: a.orgID, Name: name, Email: sql.NullString{String: email, Valid: email != ""},
	})
	if err != nil {
		http.Error(w, "Could not create customer.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/customers", http.StatusSeeOther)
}

func (a *App) invoicesHandler(w http.ResponseWriter, r *http.Request) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	invoices, err := a.invoiceSummaries(r.Context())
	if err != nil {
		http.Error(w, "Could not load invoices.", http.StatusInternalServerError)
		return
	}
	if err := InvoicesPage(org.Name, invoices).Render(r.Context(), w); err != nil {
		fmt.Printf("render invoices: %v\n", err)
	}
}

func (a *App) newInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	customers, err := a.queries.ListCustomers(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load customers.", http.StatusInternalServerError)
		return
	}
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	if err := NewInvoicePage(org.Name, customers, time.Now().Format("2006-01-02")).Render(r.Context(), w); err != nil {
		fmt.Printf("render new invoice: %v\n", err)
	}
}

func (a *App) createInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not read the invoice form.", http.StatusBadRequest)
		return
	}
	customerID, err := strconv.ParseInt(r.FormValue("customer_id"), 10, 64)
	if err != nil || customerID < 1 {
		http.Error(w, "Choose a customer.", http.StatusBadRequest)
		return
	}
	_, err = a.queries.GetCustomer(r.Context(), db.GetCustomerParams{OrganizationID: a.orgID, CustomerID: customerID})
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "That customer is not available for this organization.", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "Could not load customer.", http.StatusInternalServerError)
		return
	}
	number := strings.TrimSpace(r.FormValue("number"))
	issue, issueErr := time.Parse("2006-01-02", r.FormValue("issue_date"))
	due, dueErr := time.Parse("2006-01-02", r.FormValue("due_date"))
	if number == "" || issueErr != nil || dueErr != nil || due.Before(issue) {
		http.Error(w, "Enter an invoice number and valid dates; the due date must not be before the issue date.", http.StatusBadRequest)
		return
	}
	invoice, err := a.queries.CreateInvoice(r.Context(), db.CreateInvoiceParams{
		OrganizationID: a.orgID, CustomerID: customerID, Number: number,
		IssueDate: issue.Format("2006-01-02"), DueDate: due.Format("2006-01-02"),
	})
	if err != nil {
		http.Error(w, "Could not create invoice. Check that its number is unique.", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/invoices/%d", invoice.ID), http.StatusSeeOther)
}

func (a *App) invoiceDetailHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "invoiceID"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	invoice, err := a.queries.GetInvoice(ctx, db.GetInvoiceParams{OrganizationID: a.orgID, InvoiceID: id})
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Could not load invoice.", http.StatusInternalServerError)
		return
	}
	org, err := a.queries.GetOrganization(ctx, a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	customer, err := a.queries.GetCustomer(ctx, db.GetCustomerParams{OrganizationID: a.orgID, CustomerID: invoice.CustomerID})
	if err != nil {
		http.Error(w, "Could not load customer.", http.StatusInternalServerError)
		return
	}
	lines, err := a.queries.ListInvoiceLines(ctx, db.ListInvoiceLinesParams{OrganizationID: a.orgID, InvoiceID: invoice.ID})
	if err != nil {
		http.Error(w, "Could not load invoice lines.", http.StatusInternalServerError)
		return
	}
	total, err := a.queries.InvoiceTotal(ctx, db.InvoiceTotalParams{OrganizationID: a.orgID, InvoiceID: invoice.ID})
	if err != nil {
		http.Error(w, "Could not calculate invoice total.", http.StatusInternalServerError)
		return
	}
	accounts, err := a.queries.ListAccounts(ctx, a.orgID)
	if err != nil {
		http.Error(w, "Could not load accounts.", http.StatusInternalServerError)
		return
	}
	data := InvoiceDetailData{Organization: org.Name, Summary: InvoiceSummary{Invoice: invoice, Customer: customer.Name, Total: total}, Lines: lines, Accounts: accounts}
	if invoice.PostedTransactionID.Valid {
		transaction, err := a.queries.GetTransaction(ctx, db.GetTransactionParams{OrganizationID: a.orgID, TransactionID: invoice.PostedTransactionID.Int64})
		if err != nil {
			http.Error(w, "Could not load the linked ledger transaction.", http.StatusInternalServerError)
			return
		}
		entries, err := a.queries.ListEntries(ctx, transaction.ID)
		if err != nil {
			http.Error(w, "Could not load ledger entries.", http.StatusInternalServerError)
			return
		}
		names := make(map[int64]string, len(accounts))
		for _, account := range accounts {
			names[account.ID] = account.Name
		}
		linked := JournalTransaction{ID: transaction.ID, Date: transaction.Date, Description: transaction.Description}
		for _, entry := range entries {
			linked.Entries = append(linked.Entries, JournalEntry{AccountName: names[entry.AccountID], Direction: entry.Direction, Amount: entry.Amount})
		}
		data.Transaction = &linked
	}
	if err := InvoiceDetailPage(data).Render(ctx, w); err != nil {
		fmt.Printf("render invoice: %v\n", err)
	}
}

func (a *App) addInvoiceLineHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "invoiceID"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not read the line form.", http.StatusBadRequest)
		return
	}
	description := strings.TrimSpace(r.FormValue("description"))
	quantity, quantityErr := strconv.ParseInt(r.FormValue("quantity"), 10, 64)
	priceText := strings.TrimSpace(r.FormValue("unit_price"))
	price, priceErr := parseCents(priceText)
	if description == "" || quantityErr != nil || quantity < 1 || priceText == "" || priceErr != nil {
		http.Error(w, "Enter a description, positive whole quantity, and valid unit price.", http.StatusBadRequest)
		return
	}
	_, err = a.queries.CreateInvoiceLine(r.Context(), db.CreateInvoiceLineParams{
		OrganizationID: a.orgID, InvoiceID: id, Description: description, Quantity: quantity, UnitPriceCents: price,
	})
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "This invoice is missing or already posted.", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "Could not add the line. Check the quantity and price.", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
}

func (a *App) postInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "invoiceID"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not read the posting form.", http.StatusBadRequest)
		return
	}
	receivable, receivableErr := strconv.ParseInt(r.FormValue("receivable_account_id"), 10, 64)
	revenue, revenueErr := strconv.ParseInt(r.FormValue("revenue_account_id"), 10, 64)
	if receivableErr != nil || revenueErr != nil || receivable < 1 || revenue < 1 {
		http.Error(w, "Choose receivable and revenue accounts.", http.StatusBadRequest)
		return
	}
	_, err = invoicing.PostInvoice(r.Context(), a.sqlDB, a.orgID, id, receivable, revenue)
	if errors.Is(err, invoicing.ErrAlreadyPosted) {
		http.Error(w, "This invoice is already posted.", http.StatusConflict)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "The invoice or selected account was not found for this organization.", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Could not post invoice: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	a.notifyLedgerChanged()
	http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
}
