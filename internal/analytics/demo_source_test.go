package analytics

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDemoCashDataSourceLoadsFixture(t *testing.T) {
	source := DemoCashDataSource{
		Path: filepath.Join("..", "..", "analytics", "data", "demo_cash_movements.json"),
	}

	snapshot, err := source.LoadCashSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.OpeningCashBalance != 4500 {
		t.Fatalf(
			"opening cash balance = %.2f, want 4500.00",
			snapshot.OpeningCashBalance,
		)
	}

	if snapshot.ForecastDays != 30 {
		t.Fatalf(
			"forecast days = %d, want 30",
			snapshot.ForecastDays,
		)
	}

	if len(snapshot.Movements) != 43 {
		t.Fatalf(
			"cash movements = %d, want 43",
			len(snapshot.Movements),
		)
	}
}
