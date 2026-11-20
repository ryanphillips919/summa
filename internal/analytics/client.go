package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Package analytics is the boundary between Summa's Go application and its
// local Python analytics engine. Go remains responsible for authorization and
// selecting organization-scoped data before calling this package.

type CashMovement struct {
	ID          string  `json:"id,omitempty"`
	Date        string  `json:"date"`
	Amount      float64 `json:"amount"`
	Description string  `json:"description,omitempty"`
	Vendor      string  `json:"vendor,omitempty"`
	Category    string  `json:"category,omitempty"`
}

type Request struct {
	CashMovements      []CashMovement `json:"cash_movements"`
	ForecastDays       int            `json:"forecast_days,omitempty"`
	OpeningCashBalance float64        `json:"opening_cash_balance"`
}

type DailyCashFlowPoint struct {
	Date        string  `json:"date"`
	NetCashFlow float64 `json:"net_cash_flow"`
}

type ForecastPoint struct {
	Date                        string  `json:"date"`
	PredictedNetCashFlow        float64 `json:"predicted_net_cash_flow"`
	Lower95                     float64 `json:"lower_95"`
	Upper95                     float64 `json:"upper_95"`
	CumulativePredictedCashFlow float64 `json:"cumulative_predicted_cash_flow"`
}

type Anomaly struct {
	ID             string   `json:"id"`
	RelatedIDs     []string `json:"related_ids"`
	Date           string   `json:"date"`
	Amount         float64  `json:"amount"`
	Description    string   `json:"description"`
	Category       string   `json:"category"`
	ModifiedZScore *float64 `json:"modified_z_score"`
	Reason         string   `json:"reason"`
	Type           string   `json:"type"`
}

type Response struct {
	Methodology map[string]string `json:"methodology"`

	Summary struct {
		MovementCount   int      `json:"movement_count"`
		ObservedFrom    string   `json:"observed_from"`
		ObservedThrough string   `json:"observed_through"`
		DailyBaseline   float64  `json:"daily_baseline"`
		AnomalyCount    int      `json:"anomaly_count"`
		ForecastModel   string   `json:"forecast_model"`
		BaselineMAE     *float64 `json:"baseline_validation_mae"`
		SelectedMAE     *float64 `json:"selected_model_validation_mae"`
	} `json:"summary"`

	DailyCashFlow []DailyCashFlowPoint `json:"daily_cash_flow"`
	Forecast      []ForecastPoint      `json:"forecast"`
	Anomalies     []Anomaly            `json:"anomalies"`
}

type pythonCommand struct {
	program string
	args    []string
}

// ErrNoCashMovements means the organization has no posted cash activity yet.
var ErrNoCashMovements = errors.New("no cash movements")

func findPythonCommand(script string) (pythonCommand, error) {
	if configured := os.Getenv("SUMMA_ANALYTICS_PYTHON"); configured != "" {
		return pythonCommand{program: configured}, nil
	}

	venvPython := filepath.Join(filepath.Dir(filepath.Dir(script)), ".venv", "bin", "python")
	if runtime.GOOS == "windows" {
		venvPython = filepath.Join(filepath.Dir(filepath.Dir(script)), ".venv", "Scripts", "python.exe")
	}
	if path, err := exec.LookPath(venvPython); err == nil {
		return pythonCommand{program: path}, nil
	}

	var candidates []pythonCommand

	if runtime.GOOS == "windows" {
		candidates = []pythonCommand{
			{program: "py", args: []string{"-3"}},
			{program: "python3"},
			{program: "python"},
		}
	} else {
		candidates = []pythonCommand{
			{program: "python3"},
			{program: "python"},
		}
	}

	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate.program); err == nil {
			candidate.program = path
			return candidate, nil
		}
	}

	return pythonCommand{}, errors.New(
		"Python 3 was not found; install Python 3 or set SUMMA_ANALYTICS_PYTHON",
	)
}

// Run calls analytics/engine.py.
//
// Optional deployment configuration:
//
//	SUMMA_ANALYTICS_PYTHON=/usr/bin/python3
//	SUMMA_ANALYTICS_SCRIPT=/app/analytics/engine.py
func Run(parent context.Context, request Request) (Response, error) {
	var response Response

	input, err := json.Marshal(request)
	if err != nil {
		return response, fmt.Errorf("marshal analytics request: %w", err)
	}

	script := os.Getenv("SUMMA_ANALYTICS_SCRIPT")
	if script == "" {
		script = filepath.Join("analytics", "engine.py")
	}
	python, err := findPythonCommand(script)
	if err != nil {
		return response, err
	}

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	args := append(python.args, script)
	cmd := exec.CommandContext(ctx, python.program, args...)
	cmd.Stdin = bytes.NewReader(input)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		return response, fmt.Errorf("run analytics engine: %w: %s", err, stderr.String())
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return response, fmt.Errorf("decode analytics response: %w", err)
	}

	return response, nil
}

// RunDatabase invokes the engine with a database location and organization
// scope. Python loads the ledger-derived cash movements itself, so the normal
// dashboard path does not serialize a second copy of the ledger through Go.
func RunDatabase(
	parent context.Context,
	databasePath string,
	organizationID int64,
	forecastDays int,
	observationDays int,
) ([]byte, error) {
	script := os.Getenv("SUMMA_ANALYTICS_SCRIPT")
	if script == "" {
		script = filepath.Join("analytics", "engine.py")
	}
	python, err := findPythonCommand(script)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	args := append(python.args, script,
		"--database", databasePath,
		"--organization-id", strconv.FormatInt(organizationID, 10),
		"--forecast-days", strconv.Itoa(forecastDays),
		"--observation-days", strconv.Itoa(observationDays),
	)
	cmd := exec.CommandContext(ctx, python.program, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run database analytics engine: %w: %s", err, stderr.String())
	}

	var status struct {
		Empty bool `json:"empty"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("decode database analytics response: %w", err)
	}
	if status.Empty {
		return nil, ErrNoCashMovements
	}

	return output, nil
}
