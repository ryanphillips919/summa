-- +goose Up
-- Ambiguous account names make account selection and reporting error-prone.
-- Existing duplicates must be resolved before the migration can succeed.
CREATE UNIQUE INDEX accounts_organization_name ON accounts (organization_id, name COLLATE NOCASE);

-- +goose Down
DROP INDEX accounts_organization_name;
