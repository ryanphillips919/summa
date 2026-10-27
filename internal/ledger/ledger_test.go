package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"summa/internal/db"
)

var (
	ctx  = context.Background()
	day0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

func day(n int) time.Time { return day0.AddDate(0, 0, n) }

func dollars(n int64) Amount { return Amount(n * 100) }

// fixture is one organization with a member of every role, plus a second
// organization the owner also belongs to.
type fixture struct {
	t     *testing.T
	conn  *sql.DB
	svc   *Service
	org   int64
	other int64
	users map[Role]int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	q := db.New(conn)
	f := &fixture{t: t, conn: conn, svc: NewService(conn), users: map[Role]int64{}}

	org, err := q.CreateOrganization(ctx, "Modern Bagelry")
	if err != nil {
		t.Fatal(err)
	}
	other, err := q.CreateOrganization(ctx, "Other Co")
	if err != nil {
		t.Fatal(err)
	}
	f.org, f.other = org.ID, other.ID
	for _, r := range []Role{Viewer, Bookkeeper, Accountant, Owner} {
		u, err := q.CreateUser(ctx, db.CreateUserParams{Email: r.String() + "@example.com", Name: r.String()})
		if err != nil {
			t.Fatal(err)
		}
		if err := q.AddMembership(ctx, db.AddMembershipParams{OrganizationID: f.org, UserID: u.ID, Role: r.String()}); err != nil {
			t.Fatal(err)
		}
		f.users[r] = u.ID
	}
	if err := q.AddMembership(ctx, db.AddMembershipParams{OrganizationID: f.other, UserID: f.users[Owner], Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) account(org int64, code, name string, typ AccountType, parent int64) int64 {
	f.t.Helper()
	a, err := f.svc.CreateAccount(ctx, CreateAccountRequest{
		OrganizationID: org, ActorID: f.users[Owner], Code: code, Name: name, Type: typ, ParentID: parent,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return a.ID
}

func transfer(debit, credit int64, amt Amount) []Entry {
	return []Entry{
		{AccountID: debit, Direction: Debit, Amount: amt},
		{AccountID: credit, Direction: Credit, Amount: amt},
	}
}

func (f *fixture) post(on time.Time, entries []Entry) Transaction {
	f.t.Helper()
	tx, err := f.svc.Post(ctx, PostRequest{OrganizationID: f.org, ActorID: f.users[Bookkeeper], OccurredOn: on, Entries: entries})
	if err != nil {
		f.t.Fatalf("post: %v", err)
	}
	return tx
}

func (f *fixture) assertBalance(account int64, asOf time.Time, want Amount) {
	f.t.Helper()
	got, err := f.svc.Balance(ctx, f.org, f.users[Viewer], account, asOf)
	if err != nil {
		f.t.Fatal(err)
	}
	if got != want {
		f.t.Errorf("account %d balance as of %s = %d, want %d", account, asOf.Format(dateLayout), got, want)
	}
}

func (f *fixture) transactionCount() int {
	f.t.Helper()
	var n int
	if err := f.conn.QueryRow("SELECT count(*) FROM transactions").Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// Modern Bagelry, from Modern Treasury's "Accounting for Developers, Part I".
func TestModernBagelry(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	inventory := f.account(f.org, "1200", "Inventory", Asset, 0)
	loans := f.account(f.org, "2000", "Loans", Liability, 0)
	equity := f.account(f.org, "3000", "Equity", Equity, 0)

	f.post(day(1), transfer(cash, equity, dollars(1_000_000)))
	f.post(day(2), transfer(cash, loans, dollars(500_000)))
	f.post(day(3), transfer(inventory, cash, dollars(250_000)))
	f.post(day(4), transfer(loans, cash, dollars(30_000)))

	end := day(10)
	f.assertBalance(cash, end, dollars(1_220_000))
	f.assertBalance(inventory, end, dollars(250_000))
	f.assertBalance(equity, end, dollars(1_000_000))
	f.assertBalance(loans, end, dollars(470_000))

	tb, err := f.svc.TrialBalance(ctx, f.org, f.users[Viewer], end)
	if err != nil {
		t.Fatal(err)
	}
	if !tb.Balanced() || tb.DebitNormal != dollars(1_470_000) {
		t.Errorf("trial balance = %d / %d, want balanced at 1,470,000.00", tb.DebitNormal, tb.CreditNormal)
	}
	if len(tb.Lines) != 4 || tb.Lines[0].Code != "1000" || tb.Lines[0].Balance != dollars(1_220_000) {
		t.Errorf("trial balance lines = %+v", tb.Lines)
	}
}

func TestDirectionEffectByAccountType(t *testing.T) {
	tests := []struct {
		typ       AccountType
		dir       Direction
		increases bool
	}{
		{Asset, Debit, true}, {Asset, Credit, false},
		{Expense, Debit, true}, {Expense, Credit, false},
		{Liability, Debit, false}, {Liability, Credit, true},
		{Equity, Debit, false}, {Equity, Credit, true},
		{Revenue, Debit, false}, {Revenue, Credit, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.typ, tt.dir), func(t *testing.T) {
			f := newFixture(t)
			x := f.account(f.org, "1", "X", tt.typ, 0)
			other := f.account(f.org, "2", "Other", Asset, 0)
			f.post(day(1), []Entry{
				{AccountID: x, Direction: tt.dir, Amount: 500},
				{AccountID: other, Direction: tt.dir.opposite(), Amount: 500},
			})
			want := Amount(-500)
			if tt.increases {
				want = 500
			}
			f.assertBalance(x, day(1), want)
		})
	}
}

func TestPostRejectsInvalidTransactions(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	equity := f.account(f.org, "3000", "Equity", Equity, 0)
	otherCash := f.account(f.other, "1000", "Cash", Asset, 0)
	otherVendor, err := db.New(f.conn).CreateUser(ctx, db.CreateUserParams{Email: "x@example.com", Name: "x"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		req  PostRequest
		want error
	}{
		{"unbalanced", PostRequest{OccurredOn: day(1), Entries: []Entry{
			{AccountID: cash, Direction: Debit, Amount: 100},
			{AccountID: equity, Direction: Credit, Amount: 99},
		}}, ErrUnbalanced},
		{"single entry", PostRequest{OccurredOn: day(1), Entries: []Entry{
			{AccountID: cash, Direction: Debit, Amount: 100},
		}}, ErrInvalidTransaction},
		{"zero amount", PostRequest{OccurredOn: day(1), Entries: transfer(cash, equity, 0)}, ErrInvalidTransaction},
		{"negative amount", PostRequest{OccurredOn: day(1), Entries: transfer(cash, equity, -5)}, ErrInvalidTransaction},
		{"bad direction", PostRequest{OccurredOn: day(1), Entries: []Entry{
			{AccountID: cash, Direction: 0, Amount: 100},
			{AccountID: equity, Direction: Credit, Amount: 100},
		}}, ErrInvalidTransaction},
		{"missing date", PostRequest{Entries: transfer(cash, equity, 100)}, ErrInvalidTransaction},
		{"unknown account", PostRequest{OccurredOn: day(1), Entries: transfer(cash, 9999, 100)}, ErrUnknownAccount},
		{"another organization's account", PostRequest{OccurredOn: day(1), Entries: transfer(cash, otherCash, 100)}, ErrUnknownAccount},
		{"unknown counterparty", PostRequest{OccurredOn: day(1), CounterpartyID: otherVendor.ID + 100, Entries: transfer(cash, equity, 100)}, ErrInvalidTransaction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.req.OrganizationID, tt.req.ActorID = f.org, f.users[Bookkeeper]
			if _, err := f.svc.Post(ctx, tt.req); !errors.Is(err, tt.want) {
				t.Fatalf("Post err = %v, want %v", err, tt.want)
			}
			if n := f.transactionCount(); n != 0 {
				t.Errorf("rejected transaction was recorded (%d transactions)", n)
			}
		})
	}
	f.assertBalance(cash, day(100), 0)
}

func TestPostRejectsInactiveAndGroupAccounts(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	payroll := f.account(f.org, "6000", "Payroll", Expense, 0)
	salaries := f.account(f.org, "6100", "Salaries", Expense, payroll)
	old := f.account(f.org, "1900", "Old Bank", Asset, 0)
	if _, err := f.conn.Exec("UPDATE accounts SET is_active = 0 WHERE id = ?", old); err != nil {
		t.Fatal(err)
	}

	req := func(debit int64) PostRequest {
		return PostRequest{OrganizationID: f.org, ActorID: f.users[Bookkeeper], OccurredOn: day(1), Entries: transfer(debit, cash, 100)}
	}
	if _, err := f.svc.Post(ctx, req(payroll)); !errors.Is(err, ErrGroupAccount) {
		t.Errorf("posting to group account: err = %v, want ErrGroupAccount", err)
	}
	if _, err := f.svc.Post(ctx, req(old)); !errors.Is(err, ErrInactiveAccount) {
		t.Errorf("posting to inactive account: err = %v, want ErrInactiveAccount", err)
	}
	if _, err := f.svc.Post(ctx, req(salaries)); err != nil {
		t.Errorf("posting to child account: %v", err)
	}
}

func TestCreateAccountValidation(t *testing.T) {
	f := newFixture(t)
	payroll := f.account(f.org, "6000", "Payroll", Expense, 0)
	otherParent := f.account(f.other, "6000", "Payroll", Expense, 0)

	tests := []struct {
		name string
		req  CreateAccountRequest
		want error
	}{
		{"missing code", CreateAccountRequest{Name: "X", Type: Asset}, ErrInvalidAccount},
		{"missing name", CreateAccountRequest{Code: "1", Type: Asset}, ErrInvalidAccount},
		{"missing type", CreateAccountRequest{Code: "1", Name: "X"}, ErrInvalidAccount},
		{"duplicate code", CreateAccountRequest{Code: "6000", Name: "Dup", Type: Expense}, ErrDuplicateAccount},
		{"parent type mismatch", CreateAccountRequest{Code: "6100", Name: "X", Type: Revenue, ParentID: payroll}, ErrParentTypeMismatch},
		{"unknown parent", CreateAccountRequest{Code: "6100", Name: "X", Type: Expense, ParentID: 9999}, ErrUnknownAccount},
		{"parent in another organization", CreateAccountRequest{Code: "6100", Name: "X", Type: Expense, ParentID: otherParent}, ErrUnknownAccount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.req.OrganizationID, tt.req.ActorID = f.org, f.users[Owner]
			if _, err := f.svc.CreateAccount(ctx, tt.req); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}

	// The same code is fine in a different organization.
	f.account(f.other, "1000", "Cash", Asset, 0)
	f.account(f.org, "1000", "Cash", Asset, 0)
}

func TestRolePermissions(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	equity := f.account(f.org, "3000", "Equity", Equity, 0)
	posted := f.post(day(1), transfer(cash, equity, 100))
	outsider := f.users[Owner] // owner of f.org, but we check against an org they're not in below
	notMemberOrg, err := db.New(f.conn).CreateOrganization(ctx, "Stranger Inc")
	if err != nil {
		t.Fatal(err)
	}

	post := func(r Role) error {
		_, err := f.svc.Post(ctx, PostRequest{OrganizationID: f.org, ActorID: f.users[r], OccurredOn: day(2), Entries: transfer(cash, equity, 1)})
		return err
	}
	reverse := func(r Role, id int64) error {
		_, err := f.svc.Reverse(ctx, ReverseRequest{OrganizationID: f.org, ActorID: f.users[r], TransactionID: id, OccurredOn: day(3)})
		return err
	}
	createAccount := func(r Role, code string) error {
		_, err := f.svc.CreateAccount(ctx, CreateAccountRequest{OrganizationID: f.org, ActorID: f.users[r], Code: code, Name: code, Type: Asset})
		return err
	}

	if err := post(Viewer); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer post: err = %v, want ErrForbidden", err)
	}
	if err := post(Bookkeeper); err != nil {
		t.Errorf("bookkeeper post: %v", err)
	}
	if err := reverse(Bookkeeper, posted.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("bookkeeper reverse: err = %v, want ErrForbidden", err)
	}
	if err := reverse(Accountant, posted.ID); err != nil {
		t.Errorf("accountant reverse: %v", err)
	}
	if err := createAccount(Bookkeeper, "1100"); !errors.Is(err, ErrForbidden) {
		t.Errorf("bookkeeper create account: err = %v, want ErrForbidden", err)
	}
	if err := createAccount(Accountant, "1100"); err != nil {
		t.Errorf("accountant create account: %v", err)
	}
	if _, err := f.svc.TrialBalance(ctx, f.org, f.users[Viewer], day(10)); err != nil {
		t.Errorf("viewer trial balance: %v", err)
	}
	if _, err := f.svc.TrialBalance(ctx, notMemberOrg.ID, outsider, day(10)); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member trial balance: err = %v, want ErrForbidden", err)
	}
}

func TestBalanceAsOf(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	equity := f.account(f.org, "3000", "Equity", Equity, 0)
	loans := f.account(f.org, "2000", "Loans", Liability, 0)
	// Posted out of order: back-dated entries count from their accounting date.
	f.post(day(5), transfer(cash, loans, dollars(50)))
	f.post(day(1), transfer(cash, equity, dollars(100)))

	f.assertBalance(cash, day(0), 0)
	f.assertBalance(cash, day(1), dollars(100))
	f.assertBalance(cash, day(4), dollars(100))
	f.assertBalance(cash, day(5), dollars(150))
}

func TestReverse(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	equity := f.account(f.org, "3000", "Equity", Equity, 0)
	oops := f.post(day(1), transfer(cash, equity, dollars(999)))

	reverse := func(id int64) (Transaction, error) {
		return f.svc.Reverse(ctx, ReverseRequest{OrganizationID: f.org, ActorID: f.users[Accountant], TransactionID: id, OccurredOn: day(2), Description: "entered in error"})
	}
	rev, err := reverse(oops.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.ReversesID != oops.ID || rev.Entries[0].Direction != Credit || rev.Entries[1].Direction != Debit {
		t.Errorf("unexpected reversal: %+v", rev)
	}

	// History is preserved: the mistake is still visible on day 1.
	f.assertBalance(cash, day(1), dollars(999))
	f.assertBalance(cash, day(2), 0)
	f.assertBalance(equity, day(2), 0)
	if n := f.transactionCount(); n != 2 {
		t.Errorf("transactions = %d, want 2 (original + reversal)", n)
	}

	if _, err := reverse(oops.ID); !errors.Is(err, ErrAlreadyReversed) {
		t.Errorf("second reversal: err = %v, want ErrAlreadyReversed", err)
	}
	if _, err := reverse(9999); !errors.Is(err, ErrUnknownTransaction) {
		t.Errorf("unknown reversal: err = %v, want ErrUnknownTransaction", err)
	}
}

func TestReverseWorksAfterAccountDeactivated(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	equity := f.account(f.org, "3000", "Equity", Equity, 0)
	posted := f.post(day(1), transfer(cash, equity, 100))
	if _, err := f.conn.Exec("UPDATE accounts SET is_active = 0 WHERE id = ?", cash); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Reverse(ctx, ReverseRequest{OrganizationID: f.org, ActorID: f.users[Accountant], TransactionID: posted.ID, OccurredOn: day(2)}); err != nil {
		t.Fatalf("reverse: %v", err)
	}
}

func TestReverseCannotReachAnotherOrganization(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.other, "1000", "Cash", Asset, 0)
	equity := f.account(f.other, "3000", "Equity", Equity, 0)
	theirs, err := f.svc.Post(ctx, PostRequest{OrganizationID: f.other, ActorID: f.users[Owner], OccurredOn: day(1), Entries: transfer(cash, equity, 100)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.Reverse(ctx, ReverseRequest{OrganizationID: f.org, ActorID: f.users[Accountant], TransactionID: theirs.ID, OccurredOn: day(2)})
	if !errors.Is(err, ErrUnknownTransaction) {
		t.Errorf("err = %v, want ErrUnknownTransaction", err)
	}
}

func TestConcurrentPostsStayBalanced(t *testing.T) {
	f := newFixture(t)
	cash := f.account(f.org, "1000", "Cash", Asset, 0)
	equity := f.account(f.org, "3000", "Equity", Equity, 0)

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.Post(ctx, PostRequest{OrganizationID: f.org, ActorID: f.users[Bookkeeper], OccurredOn: day(1), Entries: transfer(cash, equity, 100)})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent post failed: %v", err)
		}
	}
	f.assertBalance(cash, day(1), 100*n)
	tb, err := f.svc.TrialBalance(ctx, f.org, f.users[Viewer], day(1))
	if err != nil {
		t.Fatal(err)
	}
	if !tb.Balanced() {
		t.Errorf("trial balance %d / %d not balanced", tb.DebitNormal, tb.CreditNormal)
	}
}
