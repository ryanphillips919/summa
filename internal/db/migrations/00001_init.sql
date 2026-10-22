-- +goose Up

-- ── Who ───────────────────────────────────────────────────────────────
CREATE TABLE organizations (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL CHECK (length(trim(name)) > 0),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE users (
  id         INTEGER PRIMARY KEY,
  email      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  name       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE memberships (
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  user_id         INTEGER NOT NULL REFERENCES users(id),
  role            TEXT NOT NULL CHECK (role IN ('owner', 'accountant', 'bookkeeper', 'viewer')),
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (organization_id, user_id)
);
CREATE INDEX memberships_by_user ON memberships(user_id);

-- ── Chart of accounts ─────────────────────────────────────────────────
CREATE TABLE accounts (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  code            TEXT NOT NULL,                 -- '1000', '5100'
  name            TEXT NOT NULL,                 -- 'Cash', 'Rent'
  type            TEXT NOT NULL CHECK (type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
  parent_id       INTEGER CHECK (parent_id <> id),
  is_active       INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (organization_id, code),
  UNIQUE (organization_id, id),
  FOREIGN KEY (organization_id, parent_id) REFERENCES accounts(organization_id, id)
);

CREATE TABLE counterparties (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  name            TEXT NOT NULL,
  kind            TEXT NOT NULL CHECK (kind IN ('vendor', 'customer', 'other')),
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (organization_id, name),
  UNIQUE (organization_id, id)
);

-- ── The ledger (append-only) ──────────────────────────────────────────
-- Rules the database can't enforce live in Go's posting function, which must
-- be the only code that inserts entries: debits == credits with 2+ entries,
-- no posting to group or inactive accounts, role checks, and child accounts
-- sharing their parent's type. See docs/ledger-rules.md.
CREATE TABLE transactions (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations(id),
  occurred_on     TEXT NOT NULL CHECK (occurred_on = date(occurred_on)),  -- accounting date 'YYYY-MM-DD'
  description     TEXT NOT NULL DEFAULT '',
  counterparty_id INTEGER,
  reverses_id     INTEGER CHECK (reverses_id <> id),
  posted_by       INTEGER NOT NULL REFERENCES users(id),
  posted_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),  -- when it was recorded
  UNIQUE (organization_id, id),
  UNIQUE (reverses_id),                          -- a transaction can only be reversed once
  FOREIGN KEY (organization_id, counterparty_id) REFERENCES counterparties(organization_id, id),
  FOREIGN KEY (organization_id, reverses_id)     REFERENCES transactions(organization_id, id)
);
CREATE INDEX transactions_by_date ON transactions(organization_id, occurred_on);

CREATE TABLE entries (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL,
  transaction_id  INTEGER NOT NULL,
  account_id      INTEGER NOT NULL,
  direction       TEXT NOT NULL CHECK (direction IN ('debit', 'credit')),
  amount          INTEGER NOT NULL CHECK (typeof(amount) = 'integer' AND amount > 0),  -- cents
  memo            TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (organization_id, transaction_id) REFERENCES transactions(organization_id, id),
  FOREIGN KEY (organization_id, account_id)     REFERENCES accounts(organization_id, id)
);
CREATE INDEX entries_by_transaction ON entries(transaction_id);
CREATE INDEX entries_by_account     ON entries(organization_id, account_id);

-- +goose StatementBegin
CREATE TRIGGER transactions_no_update BEFORE UPDATE ON transactions
BEGIN SELECT RAISE(ABORT, 'transactions are append-only: post a reversal instead'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER transactions_no_delete BEFORE DELETE ON transactions
BEGIN SELECT RAISE(ABORT, 'transactions are append-only: post a reversal instead'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER entries_no_update BEFORE UPDATE ON entries
BEGIN SELECT RAISE(ABORT, 'entries are append-only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER entries_no_delete BEFORE DELETE ON entries
BEGIN SELECT RAISE(ABORT, 'entries are append-only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER accounts_type_locked BEFORE UPDATE OF type ON accounts
WHEN NEW.type <> OLD.type AND EXISTS (SELECT 1 FROM entries WHERE account_id = OLD.id)
BEGIN SELECT RAISE(ABORT, 'cannot change the type of an account that has entries'); END;
-- +goose StatementEnd

-- ── Analytics review queue (mutable: this is workflow, not the ledger) ─
CREATE TABLE review_flags (
  id              INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL,
  transaction_id  INTEGER NOT NULL,
  kind            TEXT NOT NULL CHECK (kind IN ('amount_outlier', 'potential_duplicate')),
  reason          TEXT NOT NULL,
  score           REAL,                          -- statistic, not money
  status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'reviewed', 'dismissed')),
  reviewed_by     INTEGER REFERENCES users(id),
  reviewed_at     TEXT,
  note            TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (organization_id, transaction_id, kind),
  FOREIGN KEY (organization_id, transaction_id) REFERENCES transactions(organization_id, id),
  CHECK (status = 'open' OR (reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL))
);

-- +goose Down
DROP TABLE review_flags;
DROP TABLE entries;
DROP TABLE transactions;
DROP TABLE counterparties;
DROP TABLE accounts;
DROP TABLE memberships;
DROP TABLE users;
DROP TABLE organizations;
