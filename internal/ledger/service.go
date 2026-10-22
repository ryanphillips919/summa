package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"summa/internal/db"
)

const dateLayout = "2006-01-02"

// Service reads and writes the ledger in SQLite. Every method checks the
// actor's role in the organization before doing anything else.
type Service struct {
	conn *sql.DB
	q    *db.Queries
}

func NewService(conn *sql.DB) *Service {
	return &Service{conn: conn, q: db.New(conn)}
}

type Account struct {
	ID             int64
	OrganizationID int64
	Code           string
	Name           string
	Type           AccountType
	ParentID       int64 // 0 means a top-level account
	Active         bool
}

type CreateAccountRequest struct {
	OrganizationID int64
	ActorID        int64
	Code           string
	Name           string
	Type           AccountType
	ParentID       int64 // optional; must have the same type
}

type PostRequest struct {
	OrganizationID int64
	ActorID        int64
	OccurredOn     time.Time // accounting date; only the date part is stored
	Description    string
	CounterpartyID int64 // optional
	Entries        []Entry
}

type ReverseRequest struct {
	OrganizationID int64
	ActorID        int64
	TransactionID  int64
	OccurredOn     time.Time
	Description    string
}

type Transaction struct {
	ID             int64
	OrganizationID int64
	OccurredOn     time.Time
	Description    string
	CounterpartyID int64
	ReversesID     int64
	PostedBy       int64
	Entries        []Entry
}

// TrialBalanceLine is one account's totals as of a date. Balance is positive
// when the account has its normal balance.
type TrialBalanceLine struct {
	AccountID int64
	Code      string
	Name      string
	Type      AccountType
	Debits    Amount
	Credits   Amount
	Balance   Amount
}

type TrialBalance struct {
	Lines []TrialBalanceLine
	// Sums of debit-normal and credit-normal balances; equal in a healthy ledger.
	DebitNormal  Amount
	CreditNormal Amount
}

func (tb TrialBalance) Balanced() bool { return tb.DebitNormal == tb.CreditNormal }

func (s *Service) authorize(ctx context.Context, q *db.Queries, orgID, actorID int64, min Role) error {
	name, err := q.GetMembershipRole(ctx, db.GetMembershipRoleParams{OrganizationID: orgID, UserID: actorID})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: user %d is not a member of organization %d", ErrForbidden, actorID, orgID)
	}
	if err != nil {
		return err
	}
	role, err := parseRole(name)
	if err != nil {
		return fmt.Errorf("%w: %v", errUnexpectedStoredData, err)
	}
	if role < min {
		return fmt.Errorf("%w: %s role required, user %d is %s", ErrForbidden, min, actorID, role)
	}
	return nil
}

// inTx runs fn in a database transaction, committing only if fn succeeds.
func (s *Service) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) CreateAccount(ctx context.Context, req CreateAccountRequest) (Account, error) {
	if err := s.authorize(ctx, s.q, req.OrganizationID, req.ActorID, Accountant); err != nil {
		return Account{}, err
	}
	if !req.Type.valid() || strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Name) == "" {
		return Account{}, fmt.Errorf("%w: code, name and a valid type are required", ErrInvalidAccount)
	}
	var created db.Account
	err := s.inTx(ctx, func(q *db.Queries) error {
		if req.ParentID != 0 {
			parent, err := s.account(ctx, q, req.OrganizationID, req.ParentID)
			if err != nil {
				return err
			}
			if parent.Type != req.Type {
				return fmt.Errorf("%w: parent is %s, child is %s", ErrParentTypeMismatch, parent.Type, req.Type)
			}
		}
		var err error
		created, err = q.CreateAccount(ctx, db.CreateAccountParams{
			OrganizationID: req.OrganizationID,
			Code:           req.Code,
			Name:           req.Name,
			Type:           req.Type.String(),
			ParentID:       nullID(req.ParentID),
		})
		if isUniqueViolation(err, "accounts.organization_id, accounts.code") {
			return fmt.Errorf("%w: %s", ErrDuplicateAccount, req.Code)
		}
		return err
	})
	if err != nil {
		return Account{}, err
	}
	return toAccount(created)
}

// Post records a balanced transaction. Either the transaction and all of its
// entries are stored, or nothing is.
func (s *Service) Post(ctx context.Context, req PostRequest) (Transaction, error) {
	if err := s.authorize(ctx, s.q, req.OrganizationID, req.ActorID, Bookkeeper); err != nil {
		return Transaction{}, err
	}
	if req.OccurredOn.IsZero() {
		return Transaction{}, fmt.Errorf("%w: missing date", ErrInvalidTransaction)
	}
	if err := validateEntries(req.Entries); err != nil {
		return Transaction{}, err
	}
	var posted Transaction
	err := s.inTx(ctx, func(q *db.Queries) error {
		for _, e := range req.Entries {
			if err := s.checkPostable(ctx, q, req.OrganizationID, e.AccountID); err != nil {
				return err
			}
		}
		var err error
		posted, err = s.insert(ctx, q, req.OrganizationID, req.ActorID, req.OccurredOn, req.Description, req.CounterpartyID, 0, req.Entries)
		return err
	})
	if err != nil {
		return Transaction{}, err
	}
	return posted, nil
}

// Reverse posts a new transaction that undoes an earlier one by swapping the
// direction of every entry. The original is left untouched, and a transaction
// can only be reversed once.
func (s *Service) Reverse(ctx context.Context, req ReverseRequest) (Transaction, error) {
	if err := s.authorize(ctx, s.q, req.OrganizationID, req.ActorID, Accountant); err != nil {
		return Transaction{}, err
	}
	if req.OccurredOn.IsZero() {
		return Transaction{}, fmt.Errorf("%w: missing date", ErrInvalidTransaction)
	}
	var reversal Transaction
	err := s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.GetTransaction(ctx, db.GetTransactionParams{OrganizationID: req.OrganizationID, ID: req.TransactionID})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %d", ErrUnknownTransaction, req.TransactionID)
		}
		if err != nil {
			return err
		}
		stored, err := q.ListTransactionEntries(ctx, db.ListTransactionEntriesParams{OrganizationID: req.OrganizationID, TransactionID: req.TransactionID})
		if err != nil {
			return err
		}
		entries := make([]Entry, len(stored))
		for i, e := range stored {
			entry, err := toEntry(e)
			if err != nil {
				return err
			}
			entry.Direction = entry.Direction.opposite()
			entries[i] = entry
		}
		// Reversals skip the active/group checks: undoing a mistake must work
		// even if an account was deactivated or split since it was posted.
		reversal, err = s.insert(ctx, q, req.OrganizationID, req.ActorID, req.OccurredOn, req.Description, 0, req.TransactionID, entries)
		return err
	})
	if err != nil {
		return Transaction{}, err
	}
	return reversal, nil
}

// insert writes a transaction header and its entries, then re-reads the
// stored totals as a final check before the caller commits.
func (s *Service) insert(ctx context.Context, q *db.Queries, orgID, actorID int64, occurredOn time.Time, description string, counterpartyID, reversesID int64, entries []Entry) (Transaction, error) {
	header, err := q.InsertTransaction(ctx, db.InsertTransactionParams{
		OrganizationID: orgID,
		OccurredOn:     occurredOn.Format(dateLayout),
		Description:    description,
		CounterpartyID: nullID(counterpartyID),
		ReversesID:     nullID(reversesID),
		PostedBy:       actorID,
	})
	switch {
	case isUniqueViolation(err, "transactions.reverses_id"):
		return Transaction{}, fmt.Errorf("%w: %d", ErrAlreadyReversed, reversesID)
	case isForeignKeyViolation(err):
		return Transaction{}, fmt.Errorf("%w: unknown counterparty", ErrInvalidTransaction)
	case err != nil:
		return Transaction{}, err
	}
	for _, e := range entries {
		if err := q.InsertEntry(ctx, db.InsertEntryParams{
			OrganizationID: orgID,
			TransactionID:  header.ID,
			AccountID:      e.AccountID,
			Direction:      e.Direction.String(),
			Amount:         int64(e.Amount),
			Memo:           e.Memo,
		}); err != nil {
			return Transaction{}, err
		}
	}
	totals, err := q.TransactionTotals(ctx, header.ID)
	if err != nil {
		return Transaction{}, err
	}
	if totals.EntryCount != int64(len(entries)) || totals.Debits != totals.Credits {
		return Transaction{}, fmt.Errorf("%w: %+v", errInvariantViolated, totals)
	}
	tx, err := toTransaction(header)
	if err != nil {
		return Transaction{}, err
	}
	tx.Entries = append([]Entry(nil), entries...)
	return tx, nil
}

func (s *Service) checkPostable(ctx context.Context, q *db.Queries, orgID, accountID int64) error {
	a, err := s.account(ctx, q, orgID, accountID)
	if err != nil {
		return err
	}
	if !a.Active {
		return fmt.Errorf("%w: %s %s", ErrInactiveAccount, a.Code, a.Name)
	}
	hasChildren, err := q.AccountHasChildren(ctx, db.AccountHasChildrenParams{OrganizationID: orgID, ParentID: nullID(accountID)})
	if err != nil {
		return err
	}
	if hasChildren {
		return fmt.Errorf("%w: %s %s", ErrGroupAccount, a.Code, a.Name)
	}
	return nil
}

func (s *Service) account(ctx context.Context, q *db.Queries, orgID, accountID int64) (Account, error) {
	row, err := q.GetAccount(ctx, db.GetAccountParams{OrganizationID: orgID, ID: accountID})
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, fmt.Errorf("%w: %d", ErrUnknownAccount, accountID)
	}
	if err != nil {
		return Account{}, err
	}
	return toAccount(row)
}

// Balance returns one account's balance from transactions dated on or before
// asOf. A positive number means the account's normal balance.
func (s *Service) Balance(ctx context.Context, orgID, actorID, accountID int64, asOf time.Time) (Amount, error) {
	if err := s.authorize(ctx, s.q, orgID, actorID, Viewer); err != nil {
		return 0, err
	}
	a, err := s.account(ctx, s.q, orgID, accountID)
	if err != nil {
		return 0, err
	}
	totals, err := s.q.AccountTotals(ctx, db.AccountTotalsParams{OrganizationID: orgID, AccountID: accountID, AsOf: asOf.Format(dateLayout)})
	if err != nil {
		return 0, err
	}
	return a.Type.signedBalance(Amount(totals.Debits), Amount(totals.Credits)), nil
}

// TrialBalance lists every account with activity as of asOf, ordered by code.
func (s *Service) TrialBalance(ctx context.Context, orgID, actorID int64, asOf time.Time) (TrialBalance, error) {
	if err := s.authorize(ctx, s.q, orgID, actorID, Viewer); err != nil {
		return TrialBalance{}, err
	}
	rows, err := s.q.TrialBalance(ctx, db.TrialBalanceParams{OrganizationID: orgID, AsOf: asOf.Format(dateLayout)})
	if err != nil {
		return TrialBalance{}, err
	}
	var tb TrialBalance
	for _, r := range rows {
		typ, err := parseAccountType(r.Type)
		if err != nil {
			return TrialBalance{}, fmt.Errorf("%w: %v", errUnexpectedStoredData, err)
		}
		line := TrialBalanceLine{
			AccountID: r.ID, Code: r.Code, Name: r.Name, Type: typ,
			Debits: Amount(r.Debits), Credits: Amount(r.Credits),
		}
		line.Balance = typ.signedBalance(line.Debits, line.Credits)
		if typ.NormalBalance() == Debit {
			tb.DebitNormal += line.Balance
		} else {
			tb.CreditNormal += line.Balance
		}
		tb.Lines = append(tb.Lines, line)
	}
	return tb, nil
}

func toAccount(row db.Account) (Account, error) {
	typ, err := parseAccountType(row.Type)
	if err != nil {
		return Account{}, fmt.Errorf("%w: %v", errUnexpectedStoredData, err)
	}
	return Account{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		Code:           row.Code,
		Name:           row.Name,
		Type:           typ,
		ParentID:       row.ParentID.Int64,
		Active:         row.IsActive == 1,
	}, nil
}

func toEntry(row db.Entry) (Entry, error) {
	dir, err := parseDirection(row.Direction)
	if err != nil {
		return Entry{}, fmt.Errorf("%w: %v", errUnexpectedStoredData, err)
	}
	return Entry{AccountID: row.AccountID, Direction: dir, Amount: Amount(row.Amount), Memo: row.Memo}, nil
}

func toTransaction(row db.Transaction) (Transaction, error) {
	occurredOn, err := time.Parse(dateLayout, row.OccurredOn)
	if err != nil {
		return Transaction{}, fmt.Errorf("%w: %v", errUnexpectedStoredData, err)
	}
	return Transaction{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		OccurredOn:     occurredOn,
		Description:    row.Description,
		CounterpartyID: row.CounterpartyID.Int64,
		ReversesID:     row.ReversesID.Int64,
		PostedBy:       row.PostedBy,
	}, nil
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// SQLite reports constraint failures only through error text, e.g.
// "UNIQUE constraint failed: transactions.reverses_id".
func isUniqueViolation(err error, columns string) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: "+columns)
}

func isForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}
