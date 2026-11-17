-- +goose Up
-- Once an invoice is linked to the ledger, changing its source document would
-- make it disagree with that posting. Corrections require a separate workflow.
-- +goose StatementBegin
CREATE TRIGGER posted_invoice_no_update BEFORE UPDATE ON invoices
WHEN OLD.posted_transaction_id IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'posted invoices are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER posted_invoice_no_delete BEFORE DELETE ON invoices
WHEN OLD.posted_transaction_id IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'posted invoices are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER posted_invoice_line_no_insert BEFORE INSERT ON invoice_lines
WHEN EXISTS (SELECT 1 FROM invoices WHERE id = NEW.invoice_id AND posted_transaction_id IS NOT NULL)
BEGIN SELECT RAISE(ABORT, 'posted invoice lines are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER posted_invoice_line_no_update BEFORE UPDATE ON invoice_lines
WHEN EXISTS (SELECT 1 FROM invoices WHERE id IN (OLD.invoice_id, NEW.invoice_id) AND posted_transaction_id IS NOT NULL)
BEGIN SELECT RAISE(ABORT, 'posted invoice lines are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER posted_invoice_line_no_delete BEFORE DELETE ON invoice_lines
WHEN EXISTS (SELECT 1 FROM invoices WHERE id = OLD.invoice_id AND posted_transaction_id IS NOT NULL)
BEGIN SELECT RAISE(ABORT, 'posted invoice lines are immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER posted_invoice_line_no_delete;
DROP TRIGGER posted_invoice_line_no_update;
DROP TRIGGER posted_invoice_line_no_insert;
DROP TRIGGER posted_invoice_no_delete;
DROP TRIGGER posted_invoice_no_update;
