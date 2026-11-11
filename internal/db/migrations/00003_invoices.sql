-- +goose Up
-- SQLite requires a unique key on the columns referenced by a composite foreign key.
CREATE UNIQUE INDEX customers_organization_id_id ON customers (organization_id, id);

CREATE TABLE invoices (
  id                    INTEGER PRIMARY KEY,
  organization_id       INTEGER NOT NULL REFERENCES organizations(id),
  customer_id           INTEGER NOT NULL,
  number                TEXT NOT NULL,
  issue_date            TEXT NOT NULL,
  due_date              TEXT NOT NULL,
  posted_transaction_id INTEGER REFERENCES transactions(id),
  UNIQUE (organization_id, number),
  FOREIGN KEY (organization_id, customer_id) REFERENCES customers(organization_id, id)
);

CREATE UNIQUE INDEX invoices_posted_transaction_id_unique ON invoices (posted_transaction_id);

-- Quantities are whole units and prices are cents; totals are calculated from lines.
CREATE TABLE invoice_lines (
  id               INTEGER PRIMARY KEY,
  invoice_id       INTEGER NOT NULL REFERENCES invoices(id),
  description      TEXT NOT NULL,
  quantity         INTEGER NOT NULL CHECK (quantity > 0),
  unit_price_cents INTEGER NOT NULL CHECK (unit_price_cents >= 0),
  -- Keep quantity times price within SQLite's signed 64-bit integer range.
  CHECK (unit_price_cents = 0 OR quantity <= 9223372036854775807 / unit_price_cents)
);

-- +goose Down
DROP TABLE invoice_lines;
DROP TABLE invoices;
DROP INDEX customers_organization_id_id;
