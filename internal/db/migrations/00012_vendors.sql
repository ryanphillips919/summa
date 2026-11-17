-- +goose Up
CREATE TABLE vendors (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  name TEXT NOT NULL,
  email TEXT
);
CREATE INDEX vendors_organization_name ON vendors (organization_id, name);

-- +goose Down
DROP TABLE vendors;
