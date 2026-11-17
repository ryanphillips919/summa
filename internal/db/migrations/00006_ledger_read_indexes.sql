-- +goose Up
CREATE INDEX entries_account_transaction ON entries (account_id, transaction_id);
CREATE INDEX entries_transaction ON entries (transaction_id);
CREATE INDEX transactions_organization_date ON transactions (organization_id, date, id);

-- +goose Down
DROP INDEX transactions_organization_date;
DROP INDEX entries_transaction;
DROP INDEX entries_account_transaction;
