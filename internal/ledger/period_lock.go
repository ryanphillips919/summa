package ledger

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// LockThrough permanently advances an organization's closed-through date.
// This is a ledger primitive, not an authorization boundary: callers must
// enforce owner/accountant permissions before exposing it to users.
func LockThrough(ctx context.Context, conn *sql.DB, organizationID int64, date string) error {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return errors.New("invalid accounting period date")
	}
	result, err := conn.ExecContext(ctx, `INSERT INTO accounting_period_locks (organization_id, closed_through) VALUES (?, ?)
ON CONFLICT (organization_id) DO UPDATE SET closed_through = excluded.closed_through
WHERE excluded.closed_through >= accounting_period_locks.closed_through`, organizationID, date)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("accounting period lock cannot move backward")
	}
	return nil
}
