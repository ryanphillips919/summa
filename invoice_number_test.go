package main

import "testing"

func TestNextInvoiceNumber(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		want     string
	}{
		{"first invoice", nil, "INV-001"},
		{"continues the sequence", []string{"INV-001", "INV-002", "INV-020"}, "INV-021"},
		{"uses the highest, not the latest", []string{"INV-007", "INV-003"}, "INV-008"},
		{"ignores hand-typed numbers", []string{"INV-004", "Q3-SPECIAL", "inv-099"}, "INV-005"},
		{"grows past three digits", []string{"INV-999"}, "INV-1000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextInvoiceNumber(test.existing); got != test.want {
				t.Errorf("nextInvoiceNumber(%v) = %q, want %q", test.existing, got, test.want)
			}
		})
	}
}
