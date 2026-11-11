package main

import (
	"math"
	"testing"
)

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

func TestFormatDollars(t *testing.T) {
	for _, test := range []struct {
		cents int64
		want  string
	}{
		{0, "$0.00"}, {1, "$0.01"}, {32_550, "$325.50"},
		{11_155_002, "$111,550.02"}, {-11_469_998, "-$114,699.98"},
		{math.MinInt64, "-$92,233,720,368,547,758.08"},
	} {
		if got := formatDollars(test.cents); got != test.want {
			t.Errorf("formatDollars(%d) = %q, want %q", test.cents, got, test.want)
		}
	}
}
