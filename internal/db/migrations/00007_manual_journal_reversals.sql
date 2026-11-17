-- +goose Up
-- Only journal entries explicitly recorded through the manual adjustment
-- workflow can be reversed here; invoices require their own credit workflow.
CREATE TABLE manual_journal_transactions (
  transaction_id INTEGER PRIMARY KEY REFERENCES transactions(id)
);
CREATE TABLE manual_journal_reversals (
  original_transaction_id INTEGER PRIMARY KEY REFERENCES manual_journal_transactions(transaction_id),
  reversal_transaction_id INTEGER NOT NULL UNIQUE REFERENCES transactions(id)
);
-- +goose StatementBegin
CREATE TRIGGER manual_journal_no_update BEFORE UPDATE ON manual_journal_transactions
BEGIN SELECT RAISE(ABORT, 'journal source records are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER manual_journal_no_delete BEFORE DELETE ON manual_journal_transactions
BEGIN SELECT RAISE(ABORT, 'journal source records are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER manual_reversal_no_update BEFORE UPDATE ON manual_journal_reversals
BEGIN SELECT RAISE(ABORT, 'journal reversal records are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER manual_reversal_no_delete BEFORE DELETE ON manual_journal_reversals
BEGIN SELECT RAISE(ABORT, 'journal reversal records are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER manual_reversal_no_delete;
DROP TRIGGER manual_reversal_no_update;
DROP TRIGGER manual_journal_no_delete;
DROP TRIGGER manual_journal_no_update;
DROP TABLE manual_journal_reversals;
DROP TABLE manual_journal_transactions;
