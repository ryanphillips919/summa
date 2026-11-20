// Package analytics runs the local Python analytics engine.
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

var ErrNoCashMovements = errors.New("no cash movements")

type pythonCommand struct {
	program string
	args    []string
}

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

	for _, candidate := range []pythonCommand{
		{program: "python3"},
		{program: "python"},
	} {
		if runtime.GOOS == "windows" && candidate.program == "python3" {
			candidate = pythonCommand{program: "py", args: []string{"-3"}}
		}
		if path, err := exec.LookPath(candidate.program); err == nil {
			candidate.program = path
			return candidate, nil
		}
	}

	return pythonCommand{}, errors.New("Python 3 was not found; install Python 3 or set SUMMA_ANALYTICS_PYTHON")
}

// RunDatabase scopes a Python analytics run to one organization's SQLite data.
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

	var status struct{ Empty bool }
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("decode database analytics response: %w", err)
	}
	if status.Empty {
		return nil, ErrNoCashMovements
	}
	return output, nil
}
