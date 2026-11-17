package payables

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

type AgingRow struct {
	ID                                                              int64
	Number, Vendor, DueDate                                         string
	Current, Days1To30, Days31To60, Days61To90, Over90, Outstanding int64
}
type AgingReport struct {
	AsOf                                                            string
	Rows                                                            []AgingRow
	Current, Days1To30, Days31To60, Days61To90, Over90, Outstanding int64
}

// BuildAging includes bills issued by asOf and payments dated by asOf.
func BuildAging(ctx context.Context, conn *sql.DB, orgID int64, asOf string) (AgingReport, error) {
	date, err := time.Parse("2006-01-02", asOf)
	if err != nil || date.Format("2006-01-02") != asOf {
		return AgingReport{}, errors.New("invalid report date")
	}
	rows, err := conn.QueryContext(ctx, `SELECT b.id,b.number,v.name,b.due_date,b.amount,COALESCE((SELECT SUM(p.amount) FROM bill_payments p WHERE p.bill_id=b.id AND p.organization_id=b.organization_id AND p.date<=?),0)
FROM bills b JOIN vendors v ON v.id=b.vendor_id AND v.organization_id=b.organization_id
JOIN ledger_finalizations f ON f.transaction_id=b.transaction_id
WHERE b.organization_id=? AND b.issue_date<=? ORDER BY b.due_date,b.id`, asOf, orgID, asOf)
	if err != nil {
		return AgingReport{}, err
	}
	defer rows.Close()
	report := AgingReport{AsOf: asOf}
	for rows.Next() {
		var row AgingRow
		var amount, paid int64
		if err := rows.Scan(&row.ID, &row.Number, &row.Vendor, &row.DueDate, &amount, &paid); err != nil {
			return AgingReport{}, err
		}
		due, err := time.Parse("2006-01-02", row.DueDate)
		if err != nil || amount < paid {
			return AgingReport{}, errors.New("invalid bill data in aging report")
		}
		row.Outstanding = amount - paid
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
