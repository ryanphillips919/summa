// Package ledger is Summa's double-entry ledger engine. It enforces the rules
// in docs/ledger-rules.md: every transaction is balanced and atomic, posted
// transactions are never changed, and balances are derived from entries.
//
// Service is the only code that should write transactions or entries.
package ledger

import (
	"errors"
	"fmt"
	"math"
)

// Amount is money in integer minor units (cents). Never use floats for money.
type Amount int64

// Direction marks an entry as a debit or a credit. It is a flag, not a verb:
// its effect on a balance depends on the account's type.
type Direction int

const (
	Debit Direction = iota + 1
	Credit
)

func (d Direction) String() string {
	switch d {
	case Debit:
		return "debit"
	case Credit:
		return "credit"
	}
	return fmt.Sprintf("Direction(%d)", int(d))
}

func (d Direction) opposite() Direction { return Debit + Credit - d }

func ParseDirection(s string) (Direction, error) {
	switch s {
	case "debit":
		return Debit, nil
	case "credit":
		return Credit, nil
	}
	return 0, fmt.Errorf("ledger: unknown direction %q", s)
}

// AccountType determines an account's normal balance.
type AccountType int

const (
	Asset AccountType = iota + 1
	Liability
	Equity
	Revenue
	Expense
)

var accountTypeNames = map[AccountType]string{
	Asset:     "asset",
	Liability: "liability",
	Equity:    "equity",
	Revenue:   "revenue",
	Expense:   "expense",
}

func (t AccountType) String() string {
	if s, ok := accountTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("AccountType(%d)", int(t))
}

func ParseAccountType(s string) (AccountType, error) {
	for t, name := range accountTypeNames {
		if name == s {
			return t, nil
		}
	}
	return 0, fmt.Errorf("ledger: unknown account type %q", s)
}

// NormalBalance returns the direction that increases accounts of this type.
// Assets and expenses (uses of funds) are debit normal; liabilities, equity
// and revenue (sources of funds) are credit normal.
func (t AccountType) NormalBalance() Direction {
	switch t {
	case Asset, Expense:
		return Debit
	default:
		return Credit
	}
}

func (t AccountType) valid() bool { return t >= Asset && t <= Expense }

// signedBalance turns raw debit and credit totals into a balance where a
// positive number means the account's normal balance.
func (t AccountType) signedBalance(debits, credits Amount) Amount {
	if t.NormalBalance() == Debit {
		return debits - credits
	}
	return credits - debits
}

// Role is a member's permission level within an organization. Each role can
// do everything the roles below it can.
type Role int

const (
	Viewer Role = iota + 1
	Bookkeeper
	Accountant
	Owner
)

var roleNames = map[Role]string{
	Viewer:     "viewer",
	Bookkeeper: "bookkeeper",
	Accountant: "accountant",
	Owner:      "owner",
}

func (r Role) String() string {
	if s, ok := roleNames[r]; ok {
		return s
	}
	return fmt.Sprintf("Role(%d)", int(r))
}

// ParseRole converts a stored role name ("viewer", "owner", ...) to a Role.
func ParseRole(s string) (Role, error) {
	for r, name := range roleNames {
		if name == s {
			return r, nil
		}
	}
	return 0, fmt.Errorf("ledger: unknown role %q", s)
}

// The minimum role for each action. Service enforces these; UIs use them to
// explain and disable actions a member can't take.
const (
	RoleToRead           = Viewer
	RoleToPost           = Bookkeeper
	RoleToReverse        = Accountant
	RoleToManageAccounts = Accountant
)

type Entry struct {
	AccountID int64
	Direction Direction
	Amount    Amount
	Memo      string
}

var (
	ErrForbidden            = errors.New("ledger: not permitted")
	ErrDuplicateAccount     = errors.New("ledger: account code already exists")
	ErrInvalidAccount       = errors.New("ledger: invalid account")
	ErrUnknownAccount       = errors.New("ledger: unknown account")
	ErrInactiveAccount      = errors.New("ledger: account is inactive")
	ErrGroupAccount         = errors.New("ledger: cannot post to a group account")
	ErrParentTypeMismatch   = errors.New("ledger: child account type must match its parent")
	ErrUnknownTransaction   = errors.New("ledger: unknown transaction")
	ErrAlreadyReversed      = errors.New("ledger: transaction already reversed")
	ErrInvalidTransaction   = errors.New("ledger: invalid transaction")
	ErrUnbalanced           = errors.New("ledger: debits do not equal credits")
	errInvariantViolated    = errors.New("ledger: stored entries do not match the posting")
	errUnexpectedStoredData = errors.New("ledger: unexpected data in database")
)

// validateEntries checks the rules that need no database: at least two
// entries, positive amounts, valid directions, and debits equal to credits.
func validateEntries(entries []Entry) error {
	if len(entries) < 2 {
		return fmt.Errorf("%w: needs at least 2 entries", ErrInvalidTransaction)
	}
	var debits, credits Amount
	for _, e := range entries {
		if e.Amount <= 0 {
			return fmt.Errorf("%w: amounts must be positive", ErrInvalidTransaction)
		}
		var total *Amount
		switch e.Direction {
		case Debit:
			total = &debits
		case Credit:
			total = &credits
		default:
			return fmt.Errorf("%w: invalid direction", ErrInvalidTransaction)
		}
		if *total > math.MaxInt64-e.Amount {
			return fmt.Errorf("%w: amount overflow", ErrInvalidTransaction)
		}
		*total += e.Amount
	}
	if debits != credits {
		return fmt.Errorf("%w: debits %d, credits %d", ErrUnbalanced, debits, credits)
	}
	return nil
}
