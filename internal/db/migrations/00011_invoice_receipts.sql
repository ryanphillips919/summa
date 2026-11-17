-- +goose Up
CREATE TABLE invoice_receipts (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  invoice_id INTEGER NOT NULL REFERENCES invoices(id),
  transaction_id INTEGER NOT NULL UNIQUE REFERENCES transactions(id),
  deposit_account_id INTEGER NOT NULL REFERENCES accounts(id),
  date TEXT NOT NULL,
  amount INTEGER NOT NULL CHECK (amount > 0),
  request_key TEXT NOT NULL,
  UNIQUE (organization_id, request_key)
);
CREATE INDEX invoice_receipts_invoice ON invoice_receipts (invoice_id);
-- +goose StatementBegin
CREATE TRIGGER invoice_receipts_valid BEFORE INSERT ON invoice_receipts
WHEN NOT EXISTS (
  SELECT 1 FROM invoices i
  JOIN transactions t ON t.id = NEW.transaction_id AND t.organization_id = i.organization_id AND t.date = NEW.date
  JOIN ledger_finalizations f ON f.transaction_id = t.id
  JOIN accounts deposit ON deposit.id = NEW.deposit_account_id AND deposit.organization_id = i.organization_id AND deposit.type = 'asset'
  JOIN entries original ON original.transaction_id = i.posted_transaction_id AND original.direction = 'debit'
  JOIN accounts receivable ON receivable.id = original.account_id AND receivable.organization_id = i.organization_id AND receivable.type = 'asset'
  WHERE i.id = NEW.invoice_id AND i.organization_id = NEW.organization_id AND i.posted_transaction_id IS NOT NULL
    AND NEW.date >= i.issue_date AND deposit.id <> receivable.id
    AND (SELECT COUNT(*) FROM entries WHERE transaction_id = t.id) = 2
    AND EXISTS (SELECT 1 FROM entries WHERE transaction_id = t.id AND account_id = deposit.id AND direction = 'debit' AND amount = NEW.amount)
    AND EXISTS (SELECT 1 FROM entries WHERE transaction_id = t.id AND account_id = receivable.id AND direction = 'credit' AND amount = NEW.amount)
    AND NEW.amount <= (SELECT COALESCE(SUM(quantity * unit_price_cents), 0) FROM invoice_lines WHERE invoice_id = i.id)
      - (SELECT COALESCE(SUM(amount), 0) FROM invoice_receipts WHERE invoice_id = i.id)
)
BEGIN SELECT RAISE(ABORT, 'invalid invoice receipt'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER invoice_receipts_no_update BEFORE UPDATE ON invoice_receipts
BEGIN SELECT RAISE(ABORT, 'invoice receipts are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER invoice_receipts_no_delete BEFORE DELETE ON invoice_receipts
BEGIN SELECT RAISE(ABORT, 'invoice receipts are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER invoice_receipts_no_delete;
DROP TRIGGER invoice_receipts_no_update;
DROP TRIGGER invoice_receipts_valid;
DROP INDEX invoice_receipts_invoice;
DROP TABLE invoice_receipts;
