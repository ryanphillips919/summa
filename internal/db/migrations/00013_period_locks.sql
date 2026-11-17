-- +goose Up
CREATE TABLE accounting_period_locks (
  organization_id INTEGER PRIMARY KEY REFERENCES organizations(id),
  closed_through TEXT NOT NULL CHECK (length(closed_through) = 10 AND date(closed_through) IS NOT NULL AND closed_through = date(closed_through))
);
CREATE TABLE accounting_period_lock_events (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  closed_through TEXT NOT NULL,
  recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- +goose StatementBegin
CREATE TRIGGER period_lock_record_insert AFTER INSERT ON accounting_period_locks
BEGIN INSERT INTO accounting_period_lock_events (organization_id, closed_through) VALUES (NEW.organization_id, NEW.closed_through); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER period_lock_record_update AFTER UPDATE ON accounting_period_locks
WHEN NEW.closed_through > OLD.closed_through
BEGIN INSERT INTO accounting_period_lock_events (organization_id, closed_through) VALUES (NEW.organization_id, NEW.closed_through); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER period_lock_no_rollback BEFORE UPDATE ON accounting_period_locks
WHEN NEW.organization_id <> OLD.organization_id OR NEW.closed_through < OLD.closed_through
BEGIN SELECT RAISE(ABORT, 'accounting period lock cannot move backward'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER period_lock_no_delete BEFORE DELETE ON accounting_period_locks
BEGIN SELECT RAISE(ABORT, 'accounting period lock cannot be deleted'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER period_lock_events_no_update BEFORE UPDATE ON accounting_period_lock_events
BEGIN SELECT RAISE(ABORT, 'accounting period lock history is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER period_lock_events_no_delete BEFORE DELETE ON accounting_period_lock_events
BEGIN SELECT RAISE(ABORT, 'accounting period lock history is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ledger_finalize_open_period BEFORE INSERT ON ledger_finalizations
WHEN EXISTS (
  SELECT 1 FROM transactions t JOIN accounting_period_locks l ON l.organization_id = t.organization_id
  WHERE t.id = NEW.transaction_id AND t.date <= l.closed_through
)
BEGIN SELECT RAISE(ABORT, 'accounting period is locked'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER ledger_finalize_open_period;
DROP TRIGGER period_lock_events_no_delete;
DROP TRIGGER period_lock_events_no_update;
DROP TRIGGER period_lock_no_delete;
DROP TRIGGER period_lock_no_rollback;
DROP TRIGGER period_lock_record_update;
DROP TRIGGER period_lock_record_insert;
DROP TABLE accounting_period_lock_events;
DROP TABLE accounting_period_locks;
