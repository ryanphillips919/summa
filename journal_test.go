package main

import "testing"

func TestParseCents(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		bad   bool
	}{
		{input: "", want: 0},
		{input: "12", want: 1200},
		{input: "12.5", want: 1250},
		{input: "12.50", want: 1250},
		{input: " 0.01 ", want: 1},
		{input: "-2", bad: true},
		{input: "1.234", bad: true},
		{input: "one", bad: true},
	}
	for _, test := range tests {
		got, err := parseCents(test.input)
		if test.bad && err == nil {
			t.Errorf("parseCents(%q) unexpectedly succeeded", test.input)
		}
		if !test.bad && (err != nil || got != test.want) {
			t.Errorf("parseCents(%q) = %d, %v; want %d, nil", test.input, got, err, test.want)
		}
	}
}
