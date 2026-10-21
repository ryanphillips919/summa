package analytics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunForecastAndAnomaly(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := os.Setenv("SUMMA_ANALYTICS_SCRIPT", filepath.Join(root, "analytics", "engine.py")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("SUMMA_ANALYTICS_SCRIPT") })

	response, err := Run(context.Background(), Request{CashMovements: []CashMovement{
		{ID: "1", Date: "2026-10-01", Amount: -100},
		{ID: "2", Date: "2026-10-02", Amount: -105},
		{ID: "3", Date: "2026-10-03", Amount: -95},
		{ID: "4", Date: "2026-10-04", Amount: -110},
		{ID: "5", Date: "2026-10-05", Amount: -100},
		{ID: "6", Date: "2026-10-06", Amount: -102},
		{ID: "7", Date: "2026-10-07", Amount: -98},
		{ID: "8", Date: "2026-10-08", Amount: -5000},
	}, ForecastDays: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Forecast) != 3 {
		t.Fatalf("forecast points = %d, want 3", len(response.Forecast))
	}
	if response.Summary.AnomalyCount != 1 || len(response.Anomalies) != 1 {
		t.Fatalf("anomalies = %#v, want one", response.Anomalies)
	}
}
