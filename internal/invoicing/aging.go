package invoicing

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

type AgingRow struct {
	InvoiceID                                                       int64
	Number, Customer, DueDate                                       string
	Current, Days1To30, Days31To60, Days61To90, Over90, Outstanding int64
}

type AgingReport struct {
	AsOf                                                            string
	Rows                                                            []AgingRow
	Current, Days1To30, Days31To60, Days61To90, Over90, Outstanding int64
}

// BuildAging uses allocations dated on or before asOf. Direct journal receipts
// that have not been allocated to an invoice cannot be inferred from the ledger.
func BuildAging(ctx context.Context, conn *sql.DB, orgID int64, asOf string) (AgingReport, error) {
	date, err := time.Parse("2006-01-02", asOf)
	if err != nil || date.Format("2006-01-02") != asOf {
		return AgingReport{}, errors.New("invalid report date")
	}
	rows, err := conn.QueryContext(ctx, `SELECT i.id, i.number, c.name, i.due_date,
COALESCE((SELECT SUM(quantity * unit_price_cents) FROM invoice_lines WHERE invoice_id = i.id), 0),
COALESCE((SELECT SUM(amount) FROM invoice_receipts WHERE invoice_id = i.id AND organization_id = i.organization_id AND date <= ?), 0)
FROM invoices i JOIN customers c ON c.id = i.customer_id AND c.organization_id = i.organization_id
JOIN ledger_finalizations f ON f.transaction_id = i.posted_transaction_id
WHERE i.organization_id = ? AND i.issue_date <= ? ORDER BY i.due_date, i.id`, asOf, orgID, asOf)
	if err != nil {
		return AgingReport{}, err
	}
	defer rows.Close()
	report := AgingReport{AsOf: asOf}
	for rows.Next() {
		var row AgingRow
		var total, allocated int64
		if err := rows.Scan(&row.InvoiceID, &row.Number, &row.Customer, &row.DueDate, &total, &allocated); err != nil {
			return AgingReport{}, err
		}
		due, err := time.Parse("2006-01-02", row.DueDate)
		if err != nil || total < allocated {
			return AgingReport{}, errors.New("invalid invoice data in aging report")
		}
		row.Outstanding = total - allocated
		if row.Outstanding == 0 {
			continue
		}
		days := int(date.Sub(due).Hours() / 24)
		switch {
		case days <= 0:
			row.Current = row.Outstanding
		case days <= 30:
			row.Days1To30 = row.Outstanding
		case days <= 60:
			row.Days31To60 = row.Outstanding
		case days <= 90:
			row.Days61To90 = row.Outstanding
		default:
			row.Over90 = row.Outstanding
		}
		if report.Outstanding > math.MaxInt64-row.Outstanding {
			return AgingReport{}, errors.New("aging total exceeds supported range")
		}
		report.Outstanding += row.Outstanding
		report.Current += row.Current
		report.Days1To30 += row.Days1To30
		report.Days31To60 += row.Days31To60
		report.Days61To90 += row.Days61To90
		report.Over90 += row.Over90
		report.Rows = append(report.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return AgingReport{}, err
	}
	return report, nil
}
