package main

import (
	"fmt"
	"strings"
	"time"

	"summa/internal/ledger"
)

// lineHint is the plain-English effect of one entry line, e.g. "Rent ↑ $3,200.00".
type lineHint struct {
	Text string
	Bad  bool
}

// linePreview is what the post form shows while someone types: each line's
// effect and the running totals, computed in exact cents on the server.
type linePreview struct {
	Lines     [maxLines]lineHint
	Debits    ledger.Amount
	Credits   ledger.Amount
	Formatted map[int]string // normalized amounts, e.g. "3200" -> "3,200.00"
}

func (p linePreview) Status() (text string, balanced bool) {
	switch {
	case p.Debits == 0 && p.Credits == 0:
		return "Enter amounts", false
	case p.Debits == p.Credits:
		return "Balanced ✓", true
	case p.Debits > p.Credits:
		return "Off by " + (p.Debits - p.Credits).String() + ": add credits", false
	default:
		return "Off by " + (p.Credits - p.Debits).String() + ": add debits", false
	}
}

func previewLines(s signals, accounts []ledger.Account) linePreview {
	p := linePreview{Formatted: map[int]string{}}
	byID := make(map[int64]ledger.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}
	count := min(max(int(s.id("line_count")), 1), maxLines)
	for i := range count {
		accountID, raw := s.id(lineSignal(i, "account")), s.str(lineSignal(i, "amount"))
		account, known := byID[accountID]
		if accountID == 0 && raw == "" {
			continue
		}
		if !known {
			p.Lines[i] = lineHint{Text: "Choose an account", Bad: raw != ""}
			continue
		}
		if raw == "" {
			p.Lines[i] = lineHint{Text: account.Name + ": enter an amount"}
			continue
		}
		amount, err := ledger.ParseDollars(raw)
		if err != nil {
			p.Lines[i] = lineHint{Text: "Not a dollar amount like 1,234.56", Bad: true}
			continue
		}
		p.Formatted[i] = strings.TrimPrefix(amount.String(), "$")
		dir, err := ledger.ParseDirection(s.str(lineSignal(i, "dir")))
		if err != nil {
			p.Lines[i] = lineHint{Text: "Choose debit or credit", Bad: true}
			continue
		}
		arrow := "↓"
		if dir == account.Type.NormalBalance() {
			arrow = "↑"
		}
		p.Lines[i] = lineHint{Text: fmt.Sprintf("%s %s %s", account.Name, arrow, amount)}
		if dir == ledger.Debit {
			p.Debits += amount
		} else {
			p.Credits += amount
		}
	}
	return p
}

// previewSignals marks which lines have a problem so the form can highlight them.
func (p linePreview) previewSignals() map[string]any {
	m := map[string]any{}
	for i, l := range p.Lines {
		m[lineSignal(i, "bad")] = l.Bad
	}
	return m
}

// quickTemplate fills in a common two-line transaction.
type quickTemplate struct {
	Label         string
	Debit, Credit ledger.Account
}

func (t quickTemplate) ClickExpr() string {
	return fmt.Sprintf("$line_count = 2; $mirror = true; $%s = '%d'; $%s = 'debit'; $%s = '%d'; $%s = 'credit'; $%s = $%s; @post('/ledger/preview')",
		lineSignal(0, "account"), t.Debit.ID, lineSignal(0, "dir"),
		lineSignal(1, "account"), t.Credit.ID, lineSignal(1, "dir"),
		lineSignal(1, "amount"), lineSignal(0, "amount"))
}

// Templates picks sensible accounts from the chart for common transactions,
// skipping any the chart can't support.
func (v ledgerView) Templates() []quickTemplate {
	postable := v.Postable()
	var cash *ledger.Account
	for i, a := range postable {
		if a.Type == ledger.Asset && strings.Contains(strings.ToLower(a.Name), "cash") {
			cash = &postable[i]
			break
		}
	}
	if cash == nil {
		return nil
	}
	first := func(t ledger.AccountType) *ledger.Account {
		for i, a := range postable {
			if a.Type == t && a.ID != cash.ID {
				return &postable[i]
			}
		}
		return nil
	}
	var out []quickTemplate
	add := func(label string, debit, credit *ledger.Account) {
		if debit != nil && credit != nil {
			out = append(out, quickTemplate{Label: label, Debit: *debit, Credit: *credit})
		}
	}
	add("Pay an expense", first(ledger.Expense), cash)
	add("Record a sale", cash, first(ledger.Revenue))
	add("Owner investment", cash, first(ledger.Equity))
	add("Take out a loan", cash, first(ledger.Liability))
	add("Repay a loan", first(ledger.Liability), cash)
	add("Buy inventory or equipment", first(ledger.Asset), cash)
	return out
}

var typeLabels = map[ledger.AccountType]string{
	ledger.Asset:     "Assets",
	ledger.Liability: "Liabilities",
	ledger.Equity:    "Equity",
	ledger.Revenue:   "Revenue",
	ledger.Expense:   "Expenses",
}

type accountGroup struct {
	Label    string
	Accounts []ledger.Account
}

// PostableByType groups the account dropdown by type.
func (v ledgerView) PostableByType() []accountGroup {
	var groups []accountGroup
	for _, t := range accountTypes {
		g := accountGroup{Label: typeLabels[t]}
		for _, a := range v.Postable() {
			if a.Type == t {
				g.Accounts = append(g.Accounts, a)
			}
		}
		if len(g.Accounts) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

type trialGroup struct {
	Label string
	Lines []ledger.TrialBalanceLine
	Total ledger.Amount
}

// TrialGroups splits the trial balance into Assets, Liabilities, Equity,
// Revenue and Expenses, each with a subtotal.
func (v ledgerView) TrialGroups() []trialGroup {
	var groups []trialGroup
	for _, t := range accountTypes {
		g := trialGroup{Label: typeLabels[t]}
		for _, l := range v.Trial.Lines {
			if l.Type == t {
				g.Lines = append(g.Lines, l)
				g.Total += l.Balance
			}
		}
		if len(g.Lines) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// accountingEquation holds Assets = Liabilities + Equity + Net income, where
// net income is revenue minus expenses. It holds exactly when the trial
// balance does.
type accountingEquation struct {
	Assets, Liabilities, Equity, NetIncome ledger.Amount
}

func (v ledgerView) Equation() accountingEquation {
	totals := map[ledger.AccountType]ledger.Amount{}
	for _, l := range v.Trial.Lines {
		totals[l.Type] += l.Balance
	}
	return accountingEquation{
		Assets:      totals[ledger.Asset],
		Liabilities: totals[ledger.Liability],
		Equity:      totals[ledger.Equity],
		NetIncome:   totals[ledger.Revenue] - totals[ledger.Expense],
	}
}

func (v ledgerView) can(min ledger.Role) bool { return v.Role >= min }

func (v ledgerView) CanPost() bool           { return v.can(ledger.RoleToPost) }
func (v ledgerView) CanReverse() bool        { return v.can(ledger.RoleToReverse) }
func (v ledgerView) CanManageAccounts() bool { return v.can(ledger.RoleToManageAccounts) }

func needs(r ledger.Role) string { return fmt.Sprintf("Needs the %s role or higher", r) }

// PermissionSummary says in plain words what the current role can do.
func (v ledgerView) PermissionSummary() string { return permissionSummary(v.Role) }

func permissionSummary(r ledger.Role) string {
	var can, cannot []string
	for _, p := range []struct {
		allowed bool
		what    string
	}{
		{true, "view the books"},
		{r >= ledger.RoleToPost, "post transactions"},
		{r >= ledger.RoleToReverse, "reverse transactions"},
		{r >= ledger.RoleToManageAccounts, "add accounts"},
	} {
		if p.allowed {
			can = append(can, p.what)
		} else {
			cannot = append(cannot, p.what)
		}
	}
	summary := "Can " + joinWords(can)
	if len(cannot) > 0 {
		summary += ", but can't " + joinWords(cannot)
	}
	return summary + "."
}

func joinWords(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// amountInputExpr runs while someone types an amount (debounced). On the
// first line it mirrors the amount to the second line while $mirror is on;
// typing in any other line turns mirroring off.
func amountInputExpr(line int) string {
	if line == 0 {
		return fmt.Sprintf("$mirror && $line_count == 2 && ($%s = $%s); @post('/ledger/preview')",
			lineSignal(1, "amount"), lineSignal(0, "amount"))
	}
	return "$mirror = false; @post('/ledger/preview')"
}

func effectClass(l lineHint) string {
	if l.Bad {
		return "bad"
	}
	return ""
}

func addAccountTitle(v ledgerView) string {
	if v.CanManageAccounts() {
		return ""
	}
	return needs(ledger.RoleToManageAccounts)
}

// roleSummary is permissionSummary for a stored role name.
func roleSummary(role string) string {
	r, err := ledger.ParseRole(role)
	if err != nil {
		return ""
	}
	return permissionSummary(r)
}

// MemberID returns the first member with the given role, or 0.
func (v ledgerView) MemberID(role string) int64 {
	for _, m := range v.Members {
		if m.Role == role {
			return m.ID
		}
	}
	return 0
}

// actAs switches the page to another member and tab.
func actAs(id int64, tab string) string {
	return fmt.Sprintf("$actor = '%d'; $tab = '%s'; @get('/ledger/view')", id, tab)
}

// shownIf renders the inline style that matches a data-show expression's
// current value, so server-rendered HTML doesn't flash hidden content.
func shownIf(shown bool) string {
	if shown {
		return ""
	}
	return "display: none"
}

func toastID() string { return fmt.Sprintf("toast-%d", time.Now().UnixNano()) }

// tabClass marks the viewer's current tab active in server-rendered HTML;
// data-class keeps it in sync after that.
func tabClass(v ledgerView, tab string) string {
	if v.Tab == tab {
		return "tab active"
	}
	return "tab"
}
