package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"summa/internal/db"
	"summa/internal/ledger"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"github.com/starfederation/datastar-go/datastar"
)

const (
	maxLines      = 8
	journalLimit  = 50
	ledgerSubject = "ledger.updated"
	dateLayout    = "2006-01-02"
)

var accountTypes = []ledger.AccountType{ledger.Asset, ledger.Liability, ledger.Equity, ledger.Revenue, ledger.Expense}

func (a *App) ledgerRoutes() chi.Router {
	r := chi.NewRouter()
	// the watch stream stays open for as long as the tab does, so it must not
	// sit behind the request timeout
	r.Get("/watch", a.ledgerWatchHandler)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))
		r.Get("/", a.ledgerPageHandler)
		r.Get("/view", a.ledgerViewHandler)
		r.Post("/preview", a.ledgerPreviewHandler)
		r.Post("/lines/remove/{line}", a.ledgerRemoveLineHandler)
		r.Post("/post", a.ledgerPostHandler)
		r.Post("/reverse/{id}", a.ledgerReverseHandler)
		r.Post("/accounts", a.ledgerCreateAccountHandler)
	})
	return r
}

// ledgerView is everything the playground renders for one viewer.
type ledgerView struct {
	Members   []db.ListMembersRow
	ActorID   int64
	ActorName string
	ActorRole string
	Role      ledger.Role
	AsOf      string
	Today     string
	Accounts  []ledger.Account
	Trial     ledger.TrialBalance
	Journal   []ledger.JournalTransaction
	Preview   linePreview

	// What this viewer's page is showing, from its signals, so a re-render
	// matches it exactly instead of flashing the defaults.
	Tab       string
	HelpShown map[string]bool
	LineCount int
}

func (v ledgerView) isGroup(id int64) bool {
	for _, a := range v.Accounts {
		if a.ParentID == id {
			return true
		}
	}
	return false
}

// Postable lists the accounts entries can be posted to: active, not groups.
func (v ledgerView) Postable() []ledger.Account {
	var out []ledger.Account
	for _, a := range v.Accounts {
		if a.Active && !v.isGroup(a.ID) {
			out = append(out, a)
		}
	}
	return out
}

func (v ledgerView) ParentCode(a ledger.Account) string {
	for _, p := range v.Accounts {
		if p.ID == a.ParentID {
			return p.Code
		}
	}
	return ""
}

func (v ledgerView) AccountStatus(a ledger.Account) string {
	switch {
	case !a.Active:
		return "inactive"
	case v.isGroup(a.ID):
		return "group (not postable)"
	}
	return "postable"
}

func today() string { return time.Now().Format(dateLayout) }

// loadLedgerView reads everything the page shows as actorID would see it.
// actorID 0 means the organization's first member.
func (a *App) loadLedgerView(ctx context.Context, actorID int64, asOf string) (ledgerView, error) {
	v := ledgerView{Today: today(), AsOf: asOf, Tab: "start", HelpShown: map[string]bool{}, LineCount: 2}
	for _, t := range ledgerTabs {
		v.HelpShown[t.ID] = true
	}
	if v.AsOf == "" {
		v.AsOf = v.Today
	}
	asOfDate, err := time.Parse(dateLayout, v.AsOf)
	if err != nil {
		return v, fmt.Errorf("ledger: invalid as-of date %q", v.AsOf)
	}
	if v.Members, err = a.queries.ListMembers(ctx, a.orgID); err != nil {
		return v, err
	}
	if actorID == 0 && len(v.Members) > 0 {
		actorID = v.Members[0].ID
	}
	v.ActorID = actorID
	for _, m := range v.Members {
		if m.ID == actorID {
			v.ActorName, v.ActorRole = m.Name, m.Role
			v.Role, _ = ledger.ParseRole(m.Role)
		}
	}
	if v.Accounts, err = a.ledger.Accounts(ctx, a.orgID, actorID); err != nil {
		return v, err
	}
	if v.Trial, err = a.ledger.TrialBalance(ctx, a.orgID, actorID, asOfDate); err != nil {
		return v, err
	}
	if v.Journal, err = a.ledger.Journal(ctx, a.orgID, actorID, journalLimit); err != nil {
		return v, err
	}
	return v, nil
}

func lineSignal(i int, field string) string { return fmt.Sprintf("l%d_%s", i, field) }

// formSignals are the post form's starting values; they're also sent back
// after a successful post to clear the form. While $mirror is true, typing in
// the first amount copies it to the second line, so a simple two-line
// transaction only needs one amount.
func formSignals() map[string]any {
	m := map[string]any{"description": "", "line_count": 2, "mirror": true}
	for i := range maxLines {
		m[lineSignal(i, "account")] = ""
		m[lineSignal(i, "dir")] = defaultDirection(i)
		m[lineSignal(i, "amount")] = ""
		m[lineSignal(i, "bad")] = false
	}
	return m
}

func defaultDirection(line int) string {
	if line%2 == 1 {
		return "credit"
	}
	return "debit"
}

// lineFields are the per-line signals, in the order lines are shifted.
var lineFields = []string{"account", "dir", "amount"}

func accountFormSignals() map[string]any {
	return map[string]any{"acct_code": "", "acct_name": "", "acct_type": "expense", "acct_parent": ""}
}

type ledgerTab struct{ ID, Label string }

var ledgerTabs = []ledgerTab{
	{"start", "Start here"},
	{"post", "Post"},
	{"journal", "Journal"},
	{"trial", "Trial balance"},
	{"accounts", "Accounts"},
}

func helpSignal(tab string) string { return "help_" + tab }

func initialSignals(v ledgerView) map[string]any {
	m := formSignals()
	m["tab"] = v.Tab
	for _, t := range ledgerTabs {
		m[helpSignal(t.ID)] = v.HelpShown[t.ID]
	}
	for k, val := range accountFormSignals() {
		m[k] = val
	}
	m["actor"] = strconv.FormatInt(v.ActorID, 10)
	m["as_of"] = v.AsOf
	m["date"] = v.Today
	m["ledger_rev"] = 0
	return m
}

type signals map[string]any

func readSignals(r *http.Request) (signals, error) {
	s := signals{}
	err := datastar.ReadSignals(r, &s)
	return s, err
}

// str reads a signal as text; bound inputs arrive as strings, numbers as
// float64, and values set in Go (see with) as int.
func (s signals) str(key string) string {
	switch v := s[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	}
	return ""
}

func (s signals) id(key string) int64 {
	n, _ := strconv.ParseInt(s.str(key), 10, 64)
	return n
}

// userMessage turns an error into text for the page. Ledger errors explain
// themselves; anything else is logged and kept vague.
func userMessage(err error) string {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, "ledger: "); ok {
		return strings.ToUpper(rest[:1]) + rest[1:]
	}
	log.Printf("ledger playground: %v", err)
	return "Something went wrong; see the server log."
}

// renderLive re-renders this viewer's live section after an action, with the
// post form's line effects worked out from s.
func (a *App) renderLive(ctx context.Context, sse *datastar.ServerSentEventGenerator, s signals) {
	v, err := a.loadLedgerView(ctx, s.id("actor"), s.str("as_of"))
	if err != nil {
		_ = sse.PatchElementTempl(LedgerFlash(false, userMessage(err)))
		return
	}
	v.Preview = previewLines(s, v.Accounts)
	v.applyPageState(s)
	_ = sse.MarshalAndPatchSignals(v.Preview.previewSignals())
	_ = sse.PatchElementTempl(ActorBadge(v))
	_ = sse.PatchElementTempl(LedgerLive(v))
}

// applyPageState copies the viewer's tab, instruction boxes and line count
// from their signals.
func (v *ledgerView) applyPageState(s signals) {
	if tab := s.str("tab"); tab != "" {
		v.Tab = tab
	}
	for _, t := range ledgerTabs {
		if shown, ok := s[helpSignal(t.ID)].(bool); ok {
			v.HelpShown[t.ID] = shown
		}
	}
	v.LineCount = min(max(int(s.id("line_count")), 1), maxLines)
}

// with returns a copy of s with the given signals replaced.
func (s signals) with(changes map[string]any) signals {
	out := make(signals, len(s)+len(changes))
	for k, v := range s {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}

// ledgerPreviewHandler updates the post form's line effects and totals as
// someone types. With ?format=1 (sent when a field loses focus) it also
// normalizes amounts, e.g. "3200" -> "3,200.00".
func (a *App) ledgerPreviewHandler(w http.ResponseWriter, r *http.Request) {
	s, err := readSignals(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sse := datastar.NewSSE(w, r)
	accounts, err := a.ledger.Accounts(r.Context(), a.orgID, s.id("actor"))
	if err != nil {
		_ = sse.PatchElementTempl(LedgerFlash(false, userMessage(err)))
		return
	}
	p := previewLines(s, accounts)
	changes := p.previewSignals()
	if r.URL.Query().Get("format") == "1" {
		for i, amount := range p.Formatted {
			changes[lineSignal(i, "amount")] = amount
		}
	}
	_ = sse.MarshalAndPatchSignals(changes)
	_ = sse.PatchElementTempl(LinePreview(p))
}

// ledgerRemoveLineHandler deletes one line from the post form, shifting the
// lines below it up.
func (a *App) ledgerRemoveLineHandler(w http.ResponseWriter, r *http.Request) {
	s, err := readSignals(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	line, _ := strconv.Atoi(chi.URLParam(r, "line"))
	count := min(max(int(s.id("line_count")), 1), maxLines)
	if line < 0 || line >= count || count <= 2 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	changes := map[string]any{"line_count": count - 1, "mirror": false}
	blank := formSignals()
	for i := line; i < maxLines; i++ {
		for _, f := range lineFields {
			key := lineSignal(i, f)
			changes[key] = blank[key]
			if next, ok := s[lineSignal(i+1, f)]; ok && i+1 < maxLines {
				changes[key] = next
			}
		}
	}

	sse := datastar.NewSSE(w, r)
	s = s.with(changes)
	accounts, err := a.ledger.Accounts(r.Context(), a.orgID, s.id("actor"))
	if err != nil {
		_ = sse.PatchElementTempl(LedgerFlash(false, userMessage(err)))
		return
	}
	p := previewLines(s, accounts)
	for k, v := range p.previewSignals() {
		changes[k] = v
	}
	_ = sse.MarshalAndPatchSignals(changes)
	_ = sse.PatchElementTempl(LinePreview(p))
}

// ledgerChanged tells every open playground tab to refresh.
func (a *App) ledgerChanged() {
	if a.nc == nil {
		return
	}
	if err := a.nc.Publish(ledgerSubject, nil); err != nil {
		log.Printf("ledger playground: publish update: %v", err)
	}
}

func (a *App) ledgerPageHandler(w http.ResponseWriter, r *http.Request) {
	v, err := a.loadLedgerView(r.Context(), 0, "")
	if err != nil {
		http.Error(w, userMessage(err), http.StatusInternalServerError)
		return
	}
	_ = LedgerPage(v).Render(r.Context(), w)
}

func (a *App) ledgerViewHandler(w http.ResponseWriter, r *http.Request) {
	s, err := readSignals(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.renderLive(r.Context(), datastar.NewSSE(w, r), s)
}

func (a *App) ledgerPostHandler(w http.ResponseWriter, r *http.Request) {
	s, err := readSignals(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sse := datastar.NewSSE(w, r)
	req, err := postRequestFromSignals(s)
	if err == nil {
		req.OrganizationID = a.orgID
		var posted ledger.Transaction
		if posted, err = a.ledger.Post(r.Context(), req); err == nil {
			_ = sse.PatchElementTempl(LedgerFlash(true, fmt.Sprintf("Posted transaction #%d.", posted.ID)))
			_ = sse.MarshalAndPatchSignals(formSignals())
			s = s.with(formSignals())
			a.ledgerChanged()
		}
	}
	if err != nil {
		_ = sse.PatchElementTempl(LedgerFlash(false, userMessage(err)))
	}
	a.renderLive(r.Context(), sse, s)
}

// postRequestFromSignals reads the post form. Lines left completely blank
// are ignored; amounts are converted to cents exactly.
func postRequestFromSignals(s signals) (ledger.PostRequest, error) {
	req := ledger.PostRequest{ActorID: s.id("actor"), Description: s.str("description")}
	date, err := time.Parse(dateLayout, s.str("date"))
	if err != nil {
		return req, fmt.Errorf("ledger: choose a date for the transaction")
	}
	req.OccurredOn = date
	count := min(max(int(s.id("line_count")), 1), maxLines)
	for i := range count {
		account, amount := s.id(lineSignal(i, "account")), s.str(lineSignal(i, "amount"))
		if account == 0 && amount == "" {
			continue
		}
		if account == 0 || amount == "" {
			return req, fmt.Errorf("ledger: line %d needs both an account and an amount", i+1)
		}
		cents, err := ledger.ParseDollars(amount)
		if err != nil {
			return req, fmt.Errorf("ledger: line %d: %q is not a dollar amount like 1,234.56", i+1, amount)
		}
		dir, err := ledger.ParseDirection(s.str(lineSignal(i, "dir")))
		if err != nil {
			return req, fmt.Errorf("ledger: line %d: choose debit or credit", i+1)
		}
		req.Entries = append(req.Entries, ledger.Entry{AccountID: account, Direction: dir, Amount: cents})
	}
	return req, nil
}

func (a *App) ledgerReverseHandler(w http.ResponseWriter, r *http.Request) {
	s, err := readSignals(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sse := datastar.NewSSE(w, r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rev, err := a.ledger.Reverse(r.Context(), ledger.ReverseRequest{
		OrganizationID: a.orgID,
		ActorID:        s.id("actor"),
		TransactionID:  id,
		OccurredOn:     time.Now(),
		Description:    fmt.Sprintf("Reversal of #%d", id),
	})
	if err != nil {
		_ = sse.PatchElementTempl(LedgerFlash(false, userMessage(err)))
	} else {
		_ = sse.PatchElementTempl(LedgerFlash(true, fmt.Sprintf("Posted #%d, reversing #%d.", rev.ID, id)))
		a.ledgerChanged()
	}
	a.renderLive(r.Context(), sse, s)
}

func (a *App) ledgerCreateAccountHandler(w http.ResponseWriter, r *http.Request) {
	s, err := readSignals(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sse := datastar.NewSSE(w, r)
	typ, err := ledger.ParseAccountType(s.str("acct_type"))
	if err == nil {
		var created ledger.Account
		created, err = a.ledger.CreateAccount(r.Context(), ledger.CreateAccountRequest{
			OrganizationID: a.orgID,
			ActorID:        s.id("actor"),
			Code:           s.str("acct_code"),
			Name:           s.str("acct_name"),
			Type:           typ,
			ParentID:       s.id("acct_parent"),
		})
		if err == nil {
			_ = sse.PatchElementTempl(LedgerFlash(true, fmt.Sprintf("Added account %s %s.", created.Code, created.Name)))
			_ = sse.MarshalAndPatchSignals(accountFormSignals())
			a.ledgerChanged()
		}
	}
	if err != nil {
		_ = sse.PatchElementTempl(LedgerFlash(false, userMessage(err)))
	}
	a.renderLive(r.Context(), sse, s)
}

// ledgerWatchHandler keeps a stream open and bumps the tab's $ledger_rev
// signal whenever anyone changes the ledger. The tab then fetches /view
// with its own current actor and date, so each tab sees its own perspective.
func (a *App) ledgerWatchHandler(w http.ResponseWriter, r *http.Request) {
	if a.nc == nil {
		http.Error(w, "live updates unavailable", http.StatusServiceUnavailable)
		return
	}
	updates := make(chan *nats.Msg, 16)
	sub, err := a.nc.ChanSubscribe(ledgerSubject, updates)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer sub.Unsubscribe()
	sse := datastar.NewSSE(w, r)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-updates:
			// microseconds stay exact in a JavaScript number and always change
			if err := sse.MarshalAndPatchSignals(map[string]any{"ledger_rev": time.Now().UnixMicro()}); err != nil {
				return
			}
		}
	}
}
