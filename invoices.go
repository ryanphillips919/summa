package main

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"summa/internal/db"
	"summa/views"

	"github.com/go-chi/chi/v5"
)

func (a *App) invoiceRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.invoiceHandler)
	r.Post("/", a.invoiceCreateHandler)
	return r
}

func (a *App) invoiceHandler(w http.ResponseWriter, r *http.Request) {
	invoices, err := a.queries.ListInvoicesWithCustomers(r.Context(), a.orgID)
	if err != nil {
		log.Printf("load invoices: %v", err)
		http.Error(w, "Could not load invoices.", http.StatusInternalServerError)
		return
	}
	customers, err := a.queries.ListCustomers(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load customers.", http.StatusInternalServerError)
		return
	}
	views.InvoicesPage(invoices, customers).Render(r.Context(), w)
}

func (a *App) invoiceCreateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid invoice.", http.StatusBadRequest)
		return
	}
	customerID, err := strconv.ParseInt(r.FormValue("customer_id"), 10, 64)
	number := strings.TrimSpace(r.FormValue("number"))
	issueDate := r.FormValue("issue_date")
	dueDate := r.FormValue("due_date")
	_, issueErr := time.Parse("2006-01-02", issueDate)
	_, dueErr := time.Parse("2006-01-02", dueDate)
	if err != nil || customerID <= 0 || number == "" || issueErr != nil || dueErr != nil {
		http.Error(w, "Enter a customer, invoice number, and valid dates.", http.StatusBadRequest)
		return
	}
	_, err = a.queries.CreateInvoice(r.Context(), db.CreateInvoiceParams{
		OrganizationID: a.orgID,
		CustomerID:     customerID,
		Number:         number,
		IssueDate:      issueDate,
		DueDate:        dueDate,
	})
	if err != nil {
		http.Error(w, "Could not create invoice. Check the customer and invoice number.", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/invoices/", http.StatusSeeOther)
}
