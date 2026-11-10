-- +goose Up
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
