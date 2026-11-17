-- +goose Up
-- Refuse to silently accept a pre-existing cross-organization ledger. Repair
-- any such records explicitly before retrying this migration.
CREATE TEMP TABLE ledger_integrity_check (valid INTEGER NOT NULL CHECK (valid = 1));
INSERT INTO ledger_integrity_check (valid)
SELECT 0 FROM entries e
JOIN transactions t ON t.id = e.transaction_id
JOIN accounts a ON a.id = e.account_id
WHERE t.organization_id <> a.organization_id LIMIT 1;
DROP TABLE ledger_integrity_check;

-- +goose StatementBegin
CREATE TRIGGER entries_same_organization BEFORE INSERT ON entries
WHEN NOT EXISTS (
  SELECT 1 FROM transactions t JOIN accounts a ON a.id = NEW.account_id
  WHERE t.id = NEW.transaction_id AND t.organization_id = a.organization_id
)
BEGIN
  SELECT RAISE(ABORT, 'entry account must belong to transaction organization');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ledger_entries_immutable_update BEFORE UPDATE ON entries
BEGIN SELECT RAISE(ABORT, 'ledger entries are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ledger_entries_immutable_delete BEFORE DELETE ON entries
BEGIN SELECT RAISE(ABORT, 'ledger entries are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ledger_transactions_immutable_update BEFORE UPDATE ON transactions
BEGIN SELECT RAISE(ABORT, 'ledger transactions are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ledger_transactions_immutable_delete BEFORE DELETE ON transactions
BEGIN SELECT RAISE(ABORT, 'ledger transactions are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER posted_accounts_no_transfer BEFORE UPDATE OF organization_id ON accounts
WHEN NEW.organization_id <> OLD.organization_id AND EXISTS (SELECT 1 FROM entries WHERE account_id = OLD.id)
BEGIN SELECT RAISE(ABORT, 'account with ledger entries cannot change organization'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER posted_accounts_no_transfer;
DROP TRIGGER ledger_transactions_immutable_delete;
DROP TRIGGER ledger_transactions_immutable_update;
DROP TRIGGER ledger_entries_immutable_delete;
DROP TRIGGER ledger_entries_immutable_update;
DROP TRIGGER entries_same_organization;
