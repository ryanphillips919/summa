# Vendor bills

The Bills page records a posted vendor bill as one expense-account debit and
one liability-account credit. The amount is entered in dollars and stored as
integer cents. Posting the bill and its ledger transaction is atomic; a
repeated submission with the same request key does not post twice.

A bill can have several partial payments. Each payment debits the original
payables account and credits the selected asset account. Payment records and
posted bills are immutable; closed-period locks apply to both bill postings and
payments. The bill-aging report uses the issue date, due date, and payment dates
to calculate outstanding balances as of the selected date.

The current workflow supports one expense account per bill. Draft bills, bill
credits, corrections, payment reversals, and allocating one payment across
multiple bills are not implemented. Direct ledger entries are not allocated
to bills, so bill aging may differ from a payables account's balance. The app
still lacks authentication and must not be used for real client data.
