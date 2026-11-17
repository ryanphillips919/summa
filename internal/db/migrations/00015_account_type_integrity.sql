-- +goose Up
-- Changing an account's normal sign after it has entries silently rewrites
-- historical reports, so corrections require a new account and postings.
-- +goose StatementBegin
CREATE TRIGGER ledger_account_type_immutable BEFORE UPDATE OF type ON accounts
WHEN NEW.type <> OLD.type AND EXISTS (SELECT 1 FROM entries WHERE account_id = OLD.id)
BEGIN SELECT RAISE(ABORT, 'account type cannot change after entries are recorded'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER ledger_account_type_immutable;
