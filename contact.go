package main

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
)

func parseContact(r *http.Request) (string, string, error) {
	if err := r.ParseForm(); err != nil {
		return "", "", errors.New("could not read contact")
	}
	name, email := strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("email"))
	if name == "" || len(name) > 200 {
		return "", "", errors.New("enter a name of up to 200 characters")
	}
	if email != "" {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email || len(email) > 254 {
			return "", "", errors.New("enter a valid email address")
		}
	}
	return name, email, nil
}
