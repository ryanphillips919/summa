-- +goose Up
CREATE TABLE organizations (
  id   INTEGER PRIMARY KEY,
  name TEXT NOT NULL
);

CREATE TABLE users (
  id    INTEGER PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  name  TEXT NOT NULL
);

-- ── The ledger ─────────────────────────────────────────────────────────
-- Double entry in three tables:
--   accounts      the buckets money moves between (Cash, Rent, Sales, ...)
--   transactions  one business event ("Paid October rent")
--   entries       the lines of a transaction: which account, debit or
--                 credit, and how much. Every transaction has 2 or more
--                 entries, and its debits must equal its credits (checked
--                 in Go before saving).

-- An account belongs to one organization. Its type decides whether a debit
-- makes it go up or down: debits increase assets and expenses, credits
-- increase liabilities, equity and revenue.
CREATE TABLE accounts (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  name            TEXT NOT NULL,
  type            TEXT NOT NULL CHECK (type IN ('asset', 'liability', 'equity', 'revenue', 'expense'))
);

-- A transaction is the header for a group of entries. date is when it
-- happened in the business ('2026-10-06'), not when it was typed in.
CREATE TABLE transactions (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  date            TEXT NOT NULL,
  description     TEXT NOT NULL
);

-- An entry is one line of a transaction. amount is in whole cents
-- ($3,200.00 is 320000) so money math is always exact, and it is always
-- positive: direction says which side it's on.
CREATE TABLE entries (
  id             INTEGER PRIMARY KEY,
  transaction_id INTEGER NOT NULL REFERENCES transactions(id),
  account_id     INTEGER NOT NULL REFERENCES accounts(id),
  direction      TEXT NOT NULL CHECK (direction IN ('debit', 'credit')),
  amount         INTEGER NOT NULL CHECK (amount > 0)
);

-- +goose Down
DROP TABLE entries;
DROP TABLE transactions;
DROP TABLE accounts;
DROP TABLE users;
DROP TABLE organizations;
