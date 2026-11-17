-- +goose Up
-- A separate finalization marker lets a transaction and its entries be built
-- atomically, then prevents further entries from being added once posted.
CREATE TABLE ledger_finalizations (
  transaction_id INTEGER PRIMARY KEY REFERENCES transactions(id),
  finalized_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose StatementBegin
CREATE TRIGGER ledger_finalize_balanced BEFORE INSERT ON ledger_finalizations
WHEN (SELECT COUNT(*) FROM entries WHERE transaction_id = NEW.transaction_id) < 2
  OR (SELECT COALESCE(SUM(CASE WHEN direction = 'debit' THEN amount ELSE 0 END), 0)
      FROM entries WHERE transaction_id = NEW.transaction_id)
     <> (SELECT COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount ELSE 0 END), 0)
         FROM entries WHERE transaction_id = NEW.transaction_id)
BEGIN SELECT RAISE(ABORT, 'cannot finalize an unbalanced ledger transaction'); END;
-- +goose StatementEnd

-- Existing transactions must satisfy the invariant. Their posting time is
-- unknown, so do not misrepresent the migration time as the posting time.
INSERT INTO ledger_finalizations (transaction_id, finalized_at) SELECT id, NULL FROM transactions;

-- +goose StatementBegin
CREATE TRIGGER finalized_entries_no_insert BEFORE INSERT ON entries
WHEN EXISTS (SELECT 1 FROM ledger_finalizations WHERE transaction_id = NEW.transaction_id)
BEGIN SELECT RAISE(ABORT, 'cannot add entries to a finalized transaction'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ledger_finalizations_no_update BEFORE UPDATE ON ledger_finalizations
BEGIN SELECT RAISE(ABORT, 'ledger finalizations are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ledger_finalizations_no_delete BEFORE DELETE ON ledger_finalizations
BEGIN SELECT RAISE(ABORT, 'ledger finalizations are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER ledger_finalizations_no_delete;
DROP TRIGGER ledger_finalizations_no_update;
DROP TRIGGER finalized_entries_no_insert;
DROP TRIGGER ledger_finalize_balanced;
DROP TABLE ledger_finalizations;
