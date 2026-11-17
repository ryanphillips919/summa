package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"summa/internal/db"

	"github.com/go-chi/chi/v5"
)

func (a *App) vendorsHandler(w http.ResponseWriter, r *http.Request) {
	a.renderVendors(w, r, "", "", "", http.StatusOK)
}

func (a *App) updateVendorHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "vendorID"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	name, email, err := parseContact(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = a.queries.UpdateVendor(r.Context(), db.UpdateVendorParams{OrganizationID: a.orgID, VendorID: id, Name: name, Email: sql.NullString{String: email, Valid: email != ""}})
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Could not update vendor.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/vendors", http.StatusSeeOther)
}

func (a *App) renderVendors(w http.ResponseWriter, r *http.Request, name, email, message string, status int) {
	org, err := a.queries.GetOrganization(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load organization.", http.StatusInternalServerError)
		return
	}
	vendors, err := a.queries.ListVendors(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load vendors.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := VendorsPage(org.Name, vendors, name, email, message).Render(r.Context(), w); err != nil {
		fmt.Printf("render vendors: %v\n", err)
	}
}

func (a *App) createVendorHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not read vendor.", http.StatusBadRequest)
		return
	}
	name, email := strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("email"))
	message := ""
	if name == "" || len(name) > 200 {
		message = "Enter a vendor name of up to 200 characters."
	} else if email != "" {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email || len(email) > 254 {
			message = "Enter a valid email address."
		}
	}
	if message != "" {
		a.renderVendors(w, r, name, email, message, http.StatusBadRequest)
		return
	}
	_, err := a.queries.CreateVendor(r.Context(), db.CreateVendorParams{OrganizationID: a.orgID, Name: name, Email: sql.NullString{String: email, Valid: email != ""}})
	if err != nil {
		http.Error(w, "Could not create vendor.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/vendors", http.StatusSeeOther)
}
