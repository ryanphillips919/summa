package ledger

import (
	"errors"
	"testing"
)

func TestParseDollars(t *testing.T) {
	valid := map[string]Amount{
		"0":             0,
		"3200":          320000,
		"3,200":         320000,
		"$3,200.5":      320050,
		"  $3,200.50  ": 320050,
		"0.07":          7,
		"1000000":       100000000,
	}
	for in, want := range valid {
		got, err := ParseDollars(in)
		if err != nil || got != want {
			t.Errorf("ParseDollars(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-5", "1.234", "abc", "1.2.3", "12e3", "$", ".50", "99999999999999999999"} {
		if _, err := ParseDollars(in); !errors.Is(err, ErrInvalidTransaction) {
			t.Errorf("ParseDollars(%q) err = %v, want ErrInvalidTransaction", in, err)
		}
	}
}

func TestAmountString(t *testing.T) {
	for a, want := range map[Amount]string{
		0:           "$0.00",
		5:           "$0.05",
		320050:      "$3,200.50",
		121680000:   "$1,216,800.00",
		-500:        "-$5.00",
		100_000_000: "$1,000,000.00",
	} {
		if got := a.String(); got != want {
			t.Errorf("Amount(%d).String() = %q, want %q", int64(a), got, want)
		}
	}
}

func TestJournal(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	equity := f.account(f.org, "3000", "Equity", Equity, 0)
	first := f.post(day(1), transfer(cash, equity, dollars(100)))
	second := f.post(day(2), transfer(cash, equity, dollars(5)))
	rev, err := f.svc.Reverse(ctx, ReverseRequest{OrganizationID: f.org, ActorID: f.users[Accountant], TransactionID: second.ID, OccurredOn: day(3)})
	if err != nil {
		t.Fatal(err)
	}

	journal, err := f.svc.Journal(ctx, f.org, f.users[Viewer], 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal) != 3 || journal[0].ID != rev.ID || journal[2].ID != first.ID {
		t.Fatalf("journal order = %+v, want newest first", journal)
	}
	if journal[0].ReversesID != second.ID || journal[1].ReversedByID != rev.ID {
		t.Errorf("reversal links: %+v / %+v", journal[0], journal[1])
	}
	if l := journal[2].Lines; len(l) != 2 || l[0].AccountCode != "1000" || l[0].Direction != Debit || l[0].Amount != dollars(100) {
		t.Errorf("lines = %+v", l)
	}
	if journal[2].PostedByName != "bookkeeper" {
		t.Errorf("posted by = %q", journal[2].PostedByName)
	}

	if limited, _ := f.svc.Journal(ctx, f.org, f.users[Viewer], 1); len(limited) != 1 {
		t.Errorf("limit 1 returned %d", len(limited))
	}
	if _, err := f.svc.Journal(ctx, f.other, f.users[Viewer], 50); !errors.Is(err, ErrForbidden) {
		t.Errorf("journal of another org: err = %v, want ErrForbidden", err)
	}
	if empty, err := f.svc.Journal(ctx, f.other, f.users[Owner], 50); err != nil || len(empty) != 0 {
		t.Errorf("empty org journal = %v, %v", empty, err)
	}
}
