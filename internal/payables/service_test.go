package payables

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
	"summa/internal/db"
	"summa/internal/ledger"
)

func TestBillAndPayments(t *testing.T) {
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "payables.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(conn, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	q := db.New(conn)
	org, err := q.CreateOrganization(ctx, "First")
	if err != nil {
		t.Fatal(err)
	}
	other, err := q.CreateOrganization(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	vendor, err := q.CreateVendor(ctx, db.CreateVendorParams{OrganizationID: org.ID, Name: "Supplier"})
	if err != nil {
		t.Fatal(err)
	}
	expense, err := q.CreateAccount(ctx, db.CreateAccountParams{OrganizationID: org.ID, Name: "Rent", Type: "expense"})
	if err != nil {
		t.Fatal(err)
	}
	payable, err := q.CreateAccount(ctx, db.CreateAccountParams{OrganizationID: org.ID, Name: "Payable", Type: "liability"})
	if err != nil {
		t.Fatal(err)
	}
	cash, err := q.CreateAccount(ctx, db.CreateAccountParams{OrganizationID: org.ID, Name: "Cash", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	input := NewBill{OrganizationID: org.ID, VendorID: vendor.ID, Number: "B-1", IssueDate: "2026-01-02", DueDate: "2026-02-01", Description: "Rent", Amount: 10000, ExpenseAccountID: expense.ID, PayableAccountID: payable.ID, RequestKey: "bill-request-0000001"}
	id, err := Create(ctx, conn, input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := Create(ctx, conn, input)
	if err != nil || retry != id {
		t.Fatalf("bill retry: %d %v", retry, err)
	}
	bad := input
	bad.Amount = 1
	if _, err := Create(ctx, conn, bad); err == nil {
		t.Fatal("accepted changed retry")
	}
	bad = input
	bad.OrganizationID = other.ID
	bad.RequestKey = "bill-request-0000002"
	if _, err := Create(ctx, conn, bad); err == nil {
		t.Fatal("accepted foreign vendor")
	}
	bill, err := Get(ctx, conn, org.ID, id)
	if err != nil || bill.Paid != 0 || bill.Amount != 10000 {
		t.Fatalf("bill: %+v %v", bill, err)
	}
	if _, err := Get(ctx, conn, other.ID, id); err != sql.ErrNoRows {
		t.Fatalf("cross-org bill: %v", err)
	}
	first, err := Pay(ctx, conn, org.ID, id, cash.ID, 3000, "2026-01-05", "payment-request-0001")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := Pay(ctx, conn, org.ID, id, cash.ID, 3000, "2026-01-05", "payment-request-0001")
	if err != nil || first != repeat {
		t.Fatalf("payment retry: %d %v", repeat, err)
	}
	if _, err := Pay(ctx, conn, org.ID, id, cash.ID, 7001, "2026-01-05", "payment-request-0002"); err == nil {
		t.Fatal("accepted overpayment")
	}
	if err := ledger.LockThrough(ctx, conn, org.ID, "2026-01-31"); err != nil {
		t.Fatal(err)
	}
	if _, err := Pay(ctx, conn, org.ID, id, cash.ID, 7000, "2026-01-31", "payment-request-0002"); err == nil {
		t.Fatal("paid in locked period")
	}
	if _, err := Pay(ctx, conn, org.ID, id, cash.ID, 7000, "2026-02-01", "payment-request-0002"); err != nil {
		t.Fatal(err)
	}
	bill, err = Get(ctx, conn, org.ID, id)
	if err != nil || bill.Paid != 10000 {
		t.Fatalf("paid bill: %+v %v", bill, err)
	}
	for _, tc := range []struct {
		date string
		want int64
	}{{"2026-01-01", 0}, {"2026-01-04", 10000}, {"2026-01-05", 7000}, {"2026-02-01", 0}} {
		aging, err := BuildAging(ctx, conn, org.ID, tc.date)
		if err != nil || aging.Outstanding != tc.want {
			t.Fatalf("aging %s: %+v %v", tc.date, aging, err)
		}
	}
	transactions, err := q.ListTransactions(ctx, org.ID)
	if err != nil || len(transactions) != 3 {
		t.Fatalf("transactions: %+v %v", transactions, err)
	}
	trial, err := ledger.BuildTrialBalance(ctx, conn, org.ID, "2026-02-01")
	if err != nil || trial.TotalDebit != 10000 || trial.TotalCredit != 10000 {
		t.Fatalf("trial: %+v %v", trial, err)
	}
	if _, err := conn.Exec(`UPDATE bills SET amount=1 WHERE id=?`, id); err == nil {
		t.Fatal("changed posted bill")
	}
	if _, err := conn.Exec(`DELETE FROM bill_payments WHERE transaction_id=?`, first); err == nil {
		t.Fatal("deleted posted bill payment")
	}
	if _, err := conn.Exec(`INSERT INTO bills (organization_id,vendor_id,number,issue_date,due_date,description,amount,expense_account_id,payable_account_id,transaction_id,request_key) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, other.ID, vendor.ID, "bad", "2026-01-02", "2026-02-01", "bad", 10000, expense.ID, payable.ID, bill.TransactionID, "foreign-direct-bill"); err == nil {
		t.Fatal("database accepted foreign bill")
	}
}

func TestUpgradePreservesExistingLedger(t *testing.T) {
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "existing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, "../db/migrations", 13); err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	org, err := q.CreateOrganization(t.Context(), "Existing")
	if err != nil {
		t.Fatal(err)
	}
	cash, err := q.CreateAccount(t.Context(), db.CreateAccountParams{OrganizationID: org.ID, Name: "Cash", Type: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	sales, err := q.CreateAccount(t.Context(), db.CreateAccountParams{OrganizationID: org.ID, Name: "Sales", Type: "revenue"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ledger.Post(t.Context(), conn, db.CreateTransactionParams{OrganizationID: org.ID, Date: "2026-01-01", Description: "Sale"}, []db.CreateEntryParams{{AccountID: cash.ID, Direction: "debit", Amount: 1234}, {AccountID: sales.ID, Direction: "credit", Amount: 1234}})
	if err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(conn, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	report, err := ledger.BuildTrialBalance(t.Context(), conn, org.ID, "2026-12-31")
	if err != nil || report.TotalDebit != 1234 || report.TotalCredit != 1234 {
		t.Fatalf("ledger after upgrade: %+v %v", report, err)
	}
}
