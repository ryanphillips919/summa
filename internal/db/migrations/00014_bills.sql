-- +goose Up
CREATE TABLE bills (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  vendor_id INTEGER NOT NULL REFERENCES vendors(id),
  number TEXT NOT NULL,
  issue_date TEXT NOT NULL,
  due_date TEXT NOT NULL,
  description TEXT NOT NULL,
  amount INTEGER NOT NULL CHECK (amount > 0),
  expense_account_id INTEGER NOT NULL REFERENCES accounts(id),
  payable_account_id INTEGER NOT NULL REFERENCES accounts(id),
  transaction_id INTEGER NOT NULL UNIQUE REFERENCES transactions(id),
  request_key TEXT NOT NULL,
  UNIQUE (organization_id, request_key),
  UNIQUE (organization_id, vendor_id, number)
);
CREATE TABLE bill_payments (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  bill_id INTEGER NOT NULL REFERENCES bills(id),
  date TEXT NOT NULL,
  amount INTEGER NOT NULL CHECK (amount > 0),
  payment_account_id INTEGER NOT NULL REFERENCES accounts(id),
  transaction_id INTEGER NOT NULL UNIQUE REFERENCES transactions(id),
  request_key TEXT NOT NULL,
  UNIQUE (organization_id, request_key)
);
CREATE INDEX bill_payments_bill ON bill_payments (bill_id);
-- +goose StatementBegin
CREATE TRIGGER bills_valid BEFORE INSERT ON bills
WHEN NOT EXISTS (
  SELECT 1 FROM vendors v JOIN accounts expense ON expense.id = NEW.expense_account_id
  JOIN accounts payable ON payable.id = NEW.payable_account_id
  JOIN transactions t ON t.id = NEW.transaction_id
  JOIN ledger_finalizations f ON f.transaction_id = t.id
  WHERE v.id = NEW.vendor_id AND v.organization_id = NEW.organization_id
    AND expense.organization_id = NEW.organization_id AND expense.type = 'expense'
    AND payable.organization_id = NEW.organization_id AND payable.type = 'liability'
    AND t.organization_id = NEW.organization_id AND t.date = NEW.issue_date
    AND NEW.due_date >= NEW.issue_date
    AND (SELECT COUNT(*) FROM entries WHERE transaction_id = t.id) = 2
    AND EXISTS (SELECT 1 FROM entries WHERE transaction_id = t.id AND account_id = expense.id AND direction = 'debit' AND amount = NEW.amount)
    AND EXISTS (SELECT 1 FROM entries WHERE transaction_id = t.id AND account_id = payable.id AND direction = 'credit' AND amount = NEW.amount)
)
BEGIN SELECT RAISE(ABORT, 'invalid bill posting'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER bill_payments_valid BEFORE INSERT ON bill_payments
WHEN NOT EXISTS (
  SELECT 1 FROM bills b JOIN accounts a ON a.id = NEW.payment_account_id
  JOIN transactions t ON t.id = NEW.transaction_id
  JOIN ledger_finalizations f ON f.transaction_id = t.id
  WHERE b.id = NEW.bill_id AND b.organization_id = NEW.organization_id
    AND a.organization_id = NEW.organization_id AND a.type = 'asset'
    AND t.organization_id = NEW.organization_id AND t.date = NEW.date AND NEW.date >= b.issue_date
    AND NEW.amount <= b.amount - (SELECT COALESCE(SUM(amount), 0) FROM bill_payments WHERE bill_id = b.id)
    AND (SELECT COUNT(*) FROM entries WHERE transaction_id = t.id) = 2
    AND EXISTS (SELECT 1 FROM entries WHERE transaction_id = t.id AND account_id = b.payable_account_id AND direction = 'debit' AND amount = NEW.amount)
    AND EXISTS (SELECT 1 FROM entries WHERE transaction_id = t.id AND account_id = a.id AND direction = 'credit' AND amount = NEW.amount)
)
BEGIN SELECT RAISE(ABORT, 'invalid bill payment'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER bills_no_update BEFORE UPDATE ON bills BEGIN SELECT RAISE(ABORT, 'posted bills are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER bills_no_delete BEFORE DELETE ON bills BEGIN SELECT RAISE(ABORT, 'posted bills are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER bill_payments_no_update BEFORE UPDATE ON bill_payments BEGIN SELECT RAISE(ABORT, 'bill payments are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER bill_payments_no_delete BEFORE DELETE ON bill_payments BEGIN SELECT RAISE(ABORT, 'bill payments are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER bill_payments_no_delete;
DROP TRIGGER bill_payments_no_update;
DROP TRIGGER bills_no_delete;
DROP TRIGGER bills_no_update;
DROP TRIGGER bill_payments_valid;
DROP TRIGGER bills_valid;
DROP INDEX bill_payments_bill;
DROP TABLE bill_payments;
DROP TABLE bills;
