package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Local runs the harness with the host's python3 in a temporary directory.
// It provides no isolation and exists for development, tests, and content
// validation in CI — never for student code in production.
type Local struct {
	// Python is the interpreter to invoke; defaults to "python3".
	Python string
	// sem bounds concurrent runs so a test burst cannot fork-bomb a laptop.
	sem chan struct{}
}

// NewLocal returns a Local runner allowing at most parallel concurrent runs.
func NewLocal(parallel int) *Local {
	if parallel < 1 {
		parallel = 1
	}
	return &Local{Python: "python3", sem: make(chan struct{}, parallel)}
}

// Run stages the files, executes the harness under the spec's total budget,
// and parses its JSON result.
func (l *Local) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	select {
	case l.sem <- struct{}{}:
		defer func() { <-l.sem }()
	case <-ctx.Done():
		return RunResult{}, ctx.Err()
	}

	dir, err := os.MkdirTemp("", "tutor-run-")
	if err != nil {
		return RunResult{}, err
	}
	defer os.RemoveAll(dir)

	specBytes, err := spec.specJSON()
	if err != nil {
		return RunResult{}, err
	}
	for name, data := range map[string]string{
		HarnessFile: Harness,
		MainFile:    spec.Code,
		SpecFile:    string(specBytes),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			return RunResult{}, err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, spec.TotalBudget())
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, l.python(), HarnessFile, SpecFile)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// The harness enforces per-case limits itself; hitting the outer budget
	// means the harness is wedged, which is an infrastructure fault.
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return RunResult{}, fmt.Errorf("harness exceeded total budget: %w", err)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return RunResult{}, fmt.Errorf("start %s: %w", l.python(), err)
		}
		return RunResult{}, fmt.Errorf("harness failed: %s", trunc(stderr.String(), 500))
	}
	r, err := parseResult(stdout.Bytes(), stderr.String())
	r.Duration = time.Since(start)
	return r, err
}

// python returns the configured interpreter or the default.
func (l *Local) python() string {
	if l.Python != "" {
		return l.Python
	}
	return "python3"
}
