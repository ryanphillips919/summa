# Ledger Rules

These are the accounting rules that all Summa code touching money must follow. They are the
foundation for the Double-Entry Ledger Engine (#9) and the Git-Like Timeline (#10).

Source: Modern Treasury, ["Accounting for Developers, Part I: The Fundamentals"](https://www.moderntreasury.com/journal/accounting-for-developers-part-i).
Parts II and III of the series cover more complex transactions.

## Terms

- **Account**: a separate pool of value we track a balance for (e.g. Cash, Rent Expense). Every
  account has a type.
- **Entry**: one debit or credit of an amount against a single account. Each entry belongs to exactly
  one transaction.
- **Transaction**: an atomic financial event made up of two or more entries.
- **Ledger**: the timestamped, append-only log of all transactions.

## Rules

1. **Every transaction is double-entry.** It has at least 2 entries, each against one account, and
   records both where the money came from and where it went.
2. **Transactions are atomic.** All of a transaction's entries are written together, or none are.
3. **Debits must equal credits.** This must hold within each transaction and across the whole ledger:
   the sum of debit-normal balances equals the sum of credit-normal balances. A mismatch means money was
   created or lost, so the write must be rejected.
4. **The account type decides the sign.** "Debit" and "credit" are entry flags, not verbs:

   | Account type | Normal balance | Debit | Credit |
   |---|---|---|---|
   | Asset | debit | + | − |
   | Expense | debit | + | − |
   | Liability | credit | − | + |
   | Equity | credit | − | + |
   | Revenue | credit | − | + |

   Always derive the effect from this table. Don't special-case it per feature. Two accounts in the
   same transaction can both increase or both decrease.
5. **The ledger is append-only.** Never update or delete a posted transaction. Fix mistakes by posting
   a new reversing or adjusting transaction.
6. **Balances are derived.** Balances are computed from entries, so any balance can be rebuilt as of
   any timestamp. A cached balance is only a performance optimization and must always be rebuildable
   from the log.
7. **Everything is timestamped.** This enables point-in-time views ("the books as of last Tuesday"),
   diffs between two points in time, and audits. The timeline feature is a replay of the ledger.
8. **Check the invariant continuously.** Verify rule 3 in tests and at runtime so drift is caught
   early.

## Example

A business takes out a $10,000 loan:

| Account | Type | Entry | Effect |
|---|---|---|---|
| Cash | Asset | Debit $10,000 | +$10,000 |
| Loans Payable | Liability | Credit $10,000 | +$10,000 |

Debits ($10,000) equal credits ($10,000), and both balances went up. Repaying $2,000 later posts a
new transaction (credit Cash $2,000, debit Loans Payable $2,000). It never edits the first one.

## Open decisions

The article doesn't cover these. The team needs to decide them:

- **Money representation.** Proposal: store amounts as integer minor units (cents) in `int64`, never
  floats.
- **Currency.** Is everything USD only, or does each account or entry carry a currency?
- **Where the log lives.** NATS JetStream is already an append-only stream and fits rules 5–7. Decide
  whether it's the system of record or whether a database (SQLite via sqlc/goose) is, with NATS used
  for real-time sync.

## Current state

The `/demo/` page edits P&L totals directly, which violates rule 6. That's acceptable for the
prototype, but the real UI must post entries rather than overwrite totals.
