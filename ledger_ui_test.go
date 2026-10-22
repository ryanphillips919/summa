package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"summa/internal/db"
	"summa/internal/ledger"
)

type playground struct {
	t        *testing.T
	app      *App
	router   http.Handler
	users    map[string]int64 // by role
	accounts map[string]int64 // by code
}

func newPlayground(t *testing.T) *playground {
	t.Helper()
	ctx := context.Background()
	conn, err := openDatabase(ctx, filepath.Join(t.TempDir(), "summa.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	q := db.New(conn)
	svc := ledger.NewService(conn)
	orgID, err := seedDevData(ctx, q, svc)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{sqlDB: conn, queries: q, ledger: svc, orgID: orgID}
	p := &playground{t: t, app: app, router: newRouter(app), users: map[string]int64{}, accounts: map[string]int64{}}

	members, err := q.ListMembers(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		p.users[m.Role] = m.ID
	}
	accounts, err := q.ListAccounts(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		p.accounts[a.Code] = a.ID
	}
	return p
}

// do sends a Datastar-style request whose body is the page's signals.
func (p *playground) do(method, path string, sig map[string]any) string {
	p.t.Helper()
	body, err := json.Marshal(sig)
	if err != nil {
		p.t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Datastar-Request", "true")
	rec := httptest.NewRecorder()
	p.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		p.t.Fatalf("%s %s: status %d: %s", method, path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func (p *playground) id(n int64) string { return strconv.FormatInt(n, 10) }

func (p *playground) postSignals(role string, lines ...[3]string) map[string]any {
	sig := map[string]any{
		"actor":       p.id(p.users[role]),
		"as_of":       "2026-12-31",
		"date":        "2026-10-06",
		"description": "October rent",
		"line_count":  len(lines),
	}
	for i, l := range lines {
		sig[lineSignal(i, "account")] = p.id(p.accounts[l[0]])
		sig[lineSignal(i, "dir")] = l[1]
		sig[lineSignal(i, "amount")] = l[2]
	}
	return sig
}

func (p *playground) cashBalance() ledger.Amount {
	p.t.Helper()
	bal, err := p.app.ledger.Balance(context.Background(), p.app.orgID, p.users["viewer"], p.accounts["1000"], time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		p.t.Fatal(err)
	}
	return bal
}

const seededCash = ledger.Amount(1_220_000_00)

func TestLedgerPageRenders(t *testing.T) {
	p := newPlayground(t)
	req := httptest.NewRequest(http.MethodGet, "/ledger/", nil)
	rec := httptest.NewRecorder()
	p.router.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	for _, want := range []string{"Summa Ledger Playground", "5100 Rent", "Owner investment", "Bea (bookkeeper)", "Balanced"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestLedgerPostFromForm(t *testing.T) {
	p := newPlayground(t)
	body := p.do(http.MethodPost, "/ledger/post", p.postSignals("bookkeeper",
		[3]string{"5100", "debit", "$3,200.00"},
		[3]string{"1000", "credit", "3200"},
	))
	if !strings.Contains(body, "Posted transaction #5") {
		t.Errorf("response missing success message:\n%s", body)
	}
	if !strings.Contains(body, "October rent") {
		t.Errorf("response should re-render the journal with the new transaction")
	}
	if got := p.cashBalance(); got != seededCash-3_200_00 {
		t.Errorf("cash = %s, want %s", got, seededCash-3_200_00)
	}
}

func TestLedgerPostShowsLedgerErrors(t *testing.T) {
	tests := []struct {
		name, role string
		lines      [][3]string
		want       string
	}{
		{"viewer cannot post", "viewer", [][3]string{{"5100", "debit", "10"}, {"1000", "credit", "10"}}, "bookkeeper role required"},
		{"unbalanced", "bookkeeper", [][3]string{{"5100", "debit", "10"}, {"1000", "credit", "9.99"}}, "Debits do not equal credits"},
		{"group account", "bookkeeper", [][3]string{{"5000", "debit", "10"}, {"1000", "credit", "10"}}, "Cannot post to a group account"},
		{"bad amount", "bookkeeper", [][3]string{{"5100", "debit", "ten"}, {"1000", "credit", "10"}}, "is not a dollar amount"},
		{"one line", "bookkeeper", [][3]string{{"5100", "debit", "10"}}, "needs at least 2 entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayground(t)
			body := p.do(http.MethodPost, "/ledger/post", p.postSignals(tt.role, tt.lines...))
			if !strings.Contains(body, tt.want) || !strings.Contains(body, "toast error") {
				t.Errorf("response missing error %q:\n%s", tt.want, body)
			}
			if got := p.cashBalance(); got != seededCash {
				t.Errorf("rejected post changed cash to %s", got)
			}
		})
	}
}

func TestLedgerReverseFromPage(t *testing.T) {
	p := newPlayground(t)
	sig := func(role string) map[string]any {
		return map[string]any{"actor": p.id(p.users[role]), "as_of": "2026-12-31"}
	}
	// #4 is the seeded $30,000 loan repayment
	if body := p.do(http.MethodPost, "/ledger/reverse/4", sig("bookkeeper")); !strings.Contains(body, "accountant role required") {
		t.Errorf("bookkeeper reverse should be refused:\n%s", body)
	}
	if body := p.do(http.MethodPost, "/ledger/reverse/4", sig("accountant")); !strings.Contains(body, "reversing #4") {
		t.Errorf("accountant reverse should succeed:\n%s", body)
	}
	if got := p.cashBalance(); got != seededCash+30_000_00 {
		t.Errorf("cash after reversal = %s, want %s", got, seededCash+30_000_00)
	}
	if body := p.do(http.MethodPost, "/ledger/reverse/4", sig("accountant")); !strings.Contains(body, "already reversed") {
		t.Errorf("second reverse should be refused:\n%s", body)
	}
}

func TestLedgerCreateAccountFromPage(t *testing.T) {
	p := newPlayground(t)
	sig := map[string]any{
		"actor": p.id(p.users["accountant"]), "as_of": "2026-12-31",
		"acct_code": "5300", "acct_name": "Insurance", "acct_type": "expense", "acct_parent": p.id(p.accounts["5000"]),
	}
	if body := p.do(http.MethodPost, "/ledger/accounts", sig); !strings.Contains(body, "Added account 5300 Insurance") {
		t.Errorf("create account failed:\n%s", body)
	}
	sig["acct_type"] = "revenue"
	sig["acct_code"] = "5400"
	if body := p.do(http.MethodPost, "/ledger/accounts", sig); !strings.Contains(body, "type must match its parent") {
		t.Errorf("type mismatch should be refused:\n%s", body)
	}
}

func TestSeedDevDataRunsOnce(t *testing.T) {
	p := newPlayground(t)
	again, err := seedDevData(context.Background(), p.app.queries, p.app.ledger)
	if err != nil || again != p.app.orgID {
		t.Fatalf("second seed = %d, %v; want existing org %d", again, err, p.app.orgID)
	}
	if got := p.cashBalance(); got != seededCash {
		t.Errorf("cash after reseed = %s, want unchanged %s", got, seededCash)
	}
}

func TestLedgerPreviewShowsEffectsAndTotals(t *testing.T) {
	p := newPlayground(t)
	body := p.do(http.MethodPost, "/ledger/preview", p.postSignals("bookkeeper",
		[3]string{"5100", "debit", "3200"},
		[3]string{"1000", "credit", "$3,200"},
	))
	for _, want := range []string{"Rent ↑ $3,200.00", "Cash ↓ $3,200.00", "Balanced ✓"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %q:\n%s", want, body)
		}
	}

	body = p.do(http.MethodPost, "/ledger/preview", p.postSignals("bookkeeper",
		[3]string{"5100", "debit", "3200"},
		[3]string{"1000", "credit", "32"},
	))
	if !strings.Contains(body, "Off by $3,168.00: add credits") {
		t.Errorf("preview should explain the imbalance:\n%s", body)
	}
}

func TestLedgerPreviewFlagsBadLinesAndFormats(t *testing.T) {
	p := newPlayground(t)
	body := p.do(http.MethodPost, "/ledger/preview?format=1", p.postSignals("bookkeeper",
		[3]string{"5100", "debit", "3200.5"},
		[3]string{"1000", "credit", "lots"},
	))
	for _, want := range []string{`"l0_amount":"3,200.50"`, `"l1_bad":true`, `"l0_bad":false`, "Not a dollar amount"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %s:\n%s", want, body)
		}
	}
}

func TestLedgerRemoveLineShiftsLinesUp(t *testing.T) {
	p := newPlayground(t)
	body := p.do(http.MethodPost, "/ledger/lines/remove/1", p.postSignals("bookkeeper",
		[3]string{"5100", "debit", "10"},
		[3]string{"5200", "debit", "5"},
		[3]string{"1000", "credit", "10"},
	))
	for _, want := range []string{
		`"line_count":2`,
		`"l1_account":"` + p.id(p.accounts["1000"]) + `"`,
		`"l1_dir":"credit"`,
		`"l1_amount":"10"`,
		"Balanced ✓",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("remove line missing %s:\n%s", want, body)
		}
	}
}

func TestLedgerViewIsRoleAware(t *testing.T) {
	p := newPlayground(t)
	view := func(role string) string {
		req := httptest.NewRequest(http.MethodGet, "/ledger/view?datastar="+
			url.QueryEscape(`{"actor":"`+p.id(p.users[role])+`","as_of":"2026-12-31"}`), nil)
		req.Header.Set("Datastar-Request", "true")
		rec := httptest.NewRecorder()
		p.router.ServeHTTP(rec, req)
		return rec.Body.String()
	}

	viewer := view("viewer")
	for _, want := range []string{
		"Can view the books, but can&#39;t post transactions, reverse transactions and add accounts.",
		"Needs the bookkeeper role or higher to post",
		`<fieldset disabled>`,
		`title="Needs the accountant role or higher"`,
	} {
		if !strings.Contains(viewer, want) {
			t.Errorf("viewer view missing %q", want)
		}
	}

	bookkeeper := view("bookkeeper")
	if strings.Contains(bookkeeper, "<fieldset disabled>") {
		t.Error("bookkeeper should be able to use the post form")
	}
	if !strings.Contains(bookkeeper, "Pay an expense") {
		t.Error("bookkeeper should see quick-start templates")
	}

	owner := view("owner")
	if !strings.Contains(owner, "Can view the books, post transactions, reverse transactions and add accounts.") {
		t.Errorf("owner summary wrong")
	}
	if strings.Contains(owner, "disabled") {
		t.Error("owner should have nothing disabled")
	}
}

func TestLedgerTrialBalanceGroupsAndEquation(t *testing.T) {
	p := newPlayground(t)
	body := p.do(http.MethodPost, "/ledger/post", p.postSignals("bookkeeper",
		[3]string{"1000", "debit", "500"},
		[3]string{"4000", "credit", "500"},
	))
	for _, want := range []string{
		"Total Assets", "Total Liabilities", "Total Revenue",
		// cash 1,220,000 + 500 sale, inventory 250,000; net income is the sale
		"<span class=\"stat-label\">Assets</span><span class=\"stat-value\">$1,470,500.00</span>",
		"<span class=\"stat-label\">Net income</span><span class=\"stat-value\">$500.00</span>",
		"Balanced ✓ The books add up.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("trial balance missing %q", want)
		}
	}
}

func TestLedgerPageHasTabsWithInstructions(t *testing.T) {
	p := newPlayground(t)
	req := httptest.NewRequest(http.MethodGet, "/ledger/", nil)
	rec := httptest.NewRecorder()
	p.router.ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, tab := range ledgerTabs {
		if !strings.Contains(body, ">"+tab.Label+"</button>") {
			t.Errorf("missing tab button %q", tab.Label)
		}
		if !strings.Contains(body, `data-show="$tab == &#39;`+tab.ID+`&#39;"`) {
			t.Errorf("missing panel for tab %q", tab.ID)
		}
	}
	if n := strings.Count(body, "How this works"); n != len(ledgerTabs) {
		t.Errorf("instruction boxes = %d, want one per tab (%d)", n, len(ledgerTabs))
	}
	// Only the start tab is visible before Datastar loads, so the page
	// doesn't flash every tab at once.
	if n := strings.Count(body, `class="panel" data-show="$tab == &#39;start&#39;" style=""`); n != 1 {
		t.Errorf("start panel should render visible")
	}
	if n := strings.Count(body, `class="panel" data-show`); n != len(ledgerTabs) {
		t.Errorf("panels = %d, want %d", n, len(ledgerTabs))
	}
	if n := strings.Count(body, `style="display: none;"><div class="panel-head`); n != len(ledgerTabs)-1 {
		t.Errorf("hidden panels = %d, want %d", n, len(ledgerTabs)-1)
	}
}

func TestLedgerViewKeepsViewerTab(t *testing.T) {
	p := newPlayground(t)
	req := httptest.NewRequest(http.MethodGet, "/ledger/view?datastar="+
		url.QueryEscape(`{"actor":"`+p.id(p.users["owner"])+`","tab":"journal","help_journal":false}`), nil)
	req.Header.Set("Datastar-Request", "true")
	rec := httptest.NewRecorder()
	p.router.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `data-show="$tab == &#39;journal&#39;" style=""`) {
		t.Error("re-render should keep the journal tab visible")
	}
	if !strings.Contains(body, `data-show="$help_journal" style="display: none;"`) {
		t.Error("re-render should keep the journal instructions hidden")
	}
	if !strings.Contains(body, `id="actor-badge"`) {
		t.Error("re-render should update the role badge")
	}
}
