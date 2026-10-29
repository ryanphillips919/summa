package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// DemoCashDataSource reads the current demo JSON fixture.
type DemoCashDataSource struct {
	Path string
}

func (source DemoCashDataSource) LoadCashSnapshot(
	ctx context.Context,
) (CashSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return CashSnapshot{}, err
	}

	data, err := os.ReadFile(source.Path)
	if err != nil {
		return CashSnapshot{}, fmt.Errorf(
			"read demo cash movements from %q: %w",
			source.Path,
			err,
		)
	}

	var request Request
	if err := json.Unmarshal(data, &request); err != nil {
		return CashSnapshot{}, fmt.Errorf(
			"decode demo cash movements: %w",
			err,
		)
	}

	if len(request.CashMovements) == 0 {
		return CashSnapshot{}, fmt.Errorf(
			"demo cash movements contain no transactions",
		)
	}

	return CashSnapshot{
		OpeningCashBalance: request.OpeningCashBalance,
		ForecastDays:       request.ForecastDays,
		Movements:          request.CashMovements,
	}, nil
}
