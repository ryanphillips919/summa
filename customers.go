package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"summa/internal/db"
	"summa/views"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go/jetstream"
)

func (a *App) customerRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.customerHandler)
	r.Get("/{id}/edit", a.customerEditHandler)
	r.Post("/", a.customerCreateHandler)
	r.Post("/{id}", a.customerSaveHandler)
	r.Get("/watch", a.customerWatchHandler)
	return r
}

func (a *App) customerHandler(w http.ResponseWriter, r *http.Request) {
	customers, err := a.queries.ListCustomers(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load customers", http.StatusInternalServerError)
		return
	}
	views.CustomersPage(customers, db.Customer{}, r.URL.Query().Get("message")).Render(r.Context(), w)
}

func (a *App) customerEditHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid customer.", http.StatusBadRequest)
		return
	}
	customer, err := a.queries.GetCustomer(r.Context(), db.GetCustomerParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		http.Error(w, "Could not load customer.", http.StatusInternalServerError)
		return
	}
	customers, err := a.queries.ListCustomers(r.Context(), a.orgID)
	if err != nil {
		http.Error(w, "Could not load customers.", http.StatusInternalServerError)
		return
	}
	views.CustomersPage(customers, customer, "").Render(r.Context(), w)
}

func (a *App) customerCreateHandler(w http.ResponseWriter, r *http.Request) {
	a.customerWriteHandler(w, r, false)
}

func (a *App) customerSaveHandler(w http.ResponseWriter, r *http.Request) {
	a.customerWriteHandler(w, r, true)
}

func (a *App) customerWriteHandler(w http.ResponseWriter, r *http.Request, update bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid customer.", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	emailValue := strings.TrimSpace(r.FormValue("email"))
	email := sql.NullString{String: emailValue, Valid: emailValue != ""}
	var id int64
	var err error
	if update {
		id, err = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "Invalid customer.", http.StatusBadRequest)
			return
		}
	}
	message := ""
	if name == "" {
		message = "Name is required."
	} else if update {
		_, err = a.queries.UpdateCustomer(r.Context(), db.UpdateCustomerParams{Name: name, Email: email, OrganizationID: a.orgID, ID: id})
	} else {
		_, err = a.queries.CreateCustomer(r.Context(), db.CreateCustomerParams{OrganizationID: a.orgID, Name: name, Email: email})
	}
	if err != nil {
		message = "Could not save customer."
	}
	if message != "" {
		customers, err := a.queries.ListCustomers(r.Context(), a.orgID)
		if err != nil {
			http.Error(w, message, http.StatusInternalServerError)
			return
		}
		views.CustomersPage(customers, db.Customer{ID: id, Name: name, Email: email}, message).Render(r.Context(), w)
		return
	}
	message = "Customer saved."
	if _, err := a.kv.Put(r.Context(), "customers.update", nil); err != nil {
		message = "Customer saved, but live updates could not be sent."
	}
	http.Redirect(w, r, "/customers/?message="+url.QueryEscape(message), http.StatusSeeOther)
}

func (a *App) customerWatchHandler(w http.ResponseWriter, r *http.Request) {
	watcher, err := a.kv.Watch(r.Context(), "customers.update", jetstream.UpdatesOnly())
	if err != nil {
		http.Error(w, "Could not watch customers for updates.", http.StatusInternalServerError)
		return
	}
	defer watcher.Stop()
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is unavailable.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	for {
		customers, err := a.queries.ListCustomers(r.Context(), a.orgID)
		if err != nil {
			fmt.Fprint(w, "event: customer-error\ndata: Could not load customers.\n\n")
			flusher.Flush()
			return
		}
		var html bytes.Buffer
		if err := views.CustomerList(customers).Render(r.Context(), &html); err != nil {
			return
		}
		data, err := json.Marshal(html.String())
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return
		}
		flusher.Flush()
		select {
		case _, ok := <-watcher.Updates():
			if !ok {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
