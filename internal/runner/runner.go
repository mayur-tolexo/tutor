// Package runner executes a student's Python program against test cases in an
// isolated environment and returns structured per-case results. The embedded
// Python harness does the per-case work so every implementation only needs to
// stage files and run one command.
package runner

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Harness is the Python driver written into every sandbox alongside main.py.
//
//go:embed harness.py
var Harness string

// HarnessFile and SpecFile are the file names the harness expects in cwd.
const (
	HarnessFile = "harness.py"
	SpecFile    = "spec.json"
	MainFile    = "main.py"
)

// KnownFlags is the closed set of mistake signals the harness can emit;
// content validation rejects hints keyed on anything else.
var KnownFlags = map[string]bool{
	"no_input_call":           true,
	"no_print_call":           true,
	"no_function_def":         true,
	"input_not_converted":     true,
	"uses_float_division":     true,
	"print_instead_of_return": true,
	"bare_except":             true,
	"while_true_no_break":     true,
	"loop_variable_unused":    true,
	"uses_eval":               true,
	"string_int_concat":       true,
}

// Runner executes one RunSpec. Implementations must be safe for concurrent use.
type Runner interface {
	Run(ctx context.Context, spec RunSpec) (RunResult, error)
}

// RunSpec is one execution request: the student's code and the cases to feed it.
type RunSpec struct {
	Code        string
	Cases       []Case
	TimeLimitMS int // per case
}

// Case is one input to run the program with. Files are seeded into the
// working directory; Collect lists files to read back afterwards.
type Case struct {
	ID      string            `json:"id"`
	Stdin   string            `json:"stdin"`
	Files   map[string]string `json:"files,omitempty"`
	Collect []string          `json:"collect,omitempty"`
}

// harnessSpec is the JSON the harness reads from spec.json.
type harnessSpec struct {
	TimeLimitMS int    `json:"time_limit_ms"`
	Cases       []Case `json:"cases"`
}

// RunResult is the harness output plus wall-clock duration.
type RunResult struct {
	SyntaxError *PyError      `json:"syntax_error"`
	Flags       []string      `json:"flags"`
	Cases       []CaseResult  `json:"cases"`
	Duration    time.Duration `json:"-"`
}

// CaseResult is the observed behaviour of the program for one case.
type CaseResult struct {
	ID       string            `json:"id"`
	Stdout   string            `json:"stdout"`
	Stderr   string            `json:"stderr"`
	ExitCode int               `json:"exit_code"`
	TimedOut bool              `json:"timed_out"`
	Files    map[string]string `json:"files"`
	Error    *PyError          `json:"error"`
}

// PyError is the final exception of a traceback, or a syntax error.
type PyError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
}

// ErrBusy signals that no execution capacity is available right now; callers
// should surface a retry rather than an error.
var ErrBusy = errors.New("runner: no capacity")

// TotalBudget is the wall clock allowed for one harness run: every case's
// limit plus interpreter start-up and file staging.
func (s RunSpec) TotalBudget() time.Duration {
	perCase := time.Duration(s.TimeLimitMS) * time.Millisecond
	if perCase == 0 {
		perCase = 2 * time.Second
	}
	n := len(s.Cases)
	if n == 0 {
		n = 1
	}
	return time.Duration(n)*perCase + 3*time.Second
}

// specJSON serialises the case list for the harness.
func (s RunSpec) specJSON() ([]byte, error) {
	limit := s.TimeLimitMS
	if limit == 0 {
		limit = 2000
	}
	cases := s.Cases
	if cases == nil {
		cases = []Case{}
	}
	return json.Marshal(harnessSpec{TimeLimitMS: limit, Cases: cases})
}

// parseResult decodes the harness's stdout, guarding against a harness that
// died before printing (which would otherwise look like an empty pass).
func parseResult(stdout []byte, stderr string) (RunResult, error) {
	var r RunResult
	if len(stdout) == 0 {
		return r, fmt.Errorf("harness produced no output: %s", trunc(stderr, 500))
	}
	if err := json.Unmarshal(stdout, &r); err != nil {
		return r, fmt.Errorf("harness output: %w (%s)", err, trunc(string(stdout), 200))
	}
	if r.Flags == nil {
		r.Flags = []string{}
	}
	return r, nil
}

// trunc shortens s for inclusion in an error message.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
