-- +goose Up
-- SQLite requires a unique key on the columns referenced by a composite foreign key.
CREATE UNIQUE INDEX customers_organization_id_id ON customers (organization_id, id);

CREATE TABLE invoices (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  customer_id     INTEGER NOT NULL,
  number          TEXT NOT NULL,
  issue_date      TEXT NOT NULL,
  due_date        TEXT NOT NULL,
  UNIQUE (organization_id, number),
  FOREIGN KEY (organization_id, customer_id) REFERENCES customers(organization_id, id)
);

-- +goose Down
DROP TABLE invoices;
DROP INDEX customers_organization_id_id;
