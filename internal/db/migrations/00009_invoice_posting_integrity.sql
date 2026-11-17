-- +goose Up
-- Do not migrate a posted invoice that points outside its organization or to
-- an unfinalized/missing ledger transaction.
CREATE TEMP TABLE invoice_posting_check (valid INTEGER NOT NULL CHECK (valid = 1));
INSERT INTO invoice_posting_check (valid)
SELECT 0 FROM invoices i
WHERE i.posted_transaction_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM transactions t JOIN ledger_finalizations f ON f.transaction_id = t.id
  WHERE t.id = i.posted_transaction_id AND t.organization_id = i.organization_id
) LIMIT 1;
DROP TABLE invoice_posting_check;

-- +goose StatementBegin
CREATE TRIGGER invoice_posting_valid_insert BEFORE INSERT ON invoices
WHEN NEW.posted_transaction_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM transactions t JOIN ledger_finalizations f ON f.transaction_id = t.id
  WHERE t.id = NEW.posted_transaction_id AND t.organization_id = NEW.organization_id
)
BEGIN SELECT RAISE(ABORT, 'invoice posting must reference a finalized transaction in its organization'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER invoice_posting_valid_update BEFORE UPDATE OF posted_transaction_id ON invoices
WHEN NEW.posted_transaction_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM transactions t JOIN ledger_finalizations f ON f.transaction_id = t.id
  WHERE t.id = NEW.posted_transaction_id AND t.organization_id = NEW.organization_id
)
BEGIN SELECT RAISE(ABORT, 'invoice posting must reference a finalized transaction in its organization'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER invoice_posting_valid_update;
DROP TRIGGER invoice_posting_valid_insert;
