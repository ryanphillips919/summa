-- +goose Up
CREATE TABLE customers (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  name            TEXT NOT NULL,
  email           TEXT
);

-- +goose Down
DROP TABLE customers;
