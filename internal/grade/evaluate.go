package grade

import (
	"github.com/mayur-tolexo/tutor/internal/content"
	"github.com/mayur-tolexo/tutor/internal/runner"
)

// Outcomes of an attempt, in the order the student experiences them.
const (
	OutcomePassed       = "passed"
	OutcomeFailed       = "failed"
	OutcomeRuntimeError = "runtime_error"
	OutcomeSyntaxError  = "syntax_error"
	OutcomeTimeout      = "timeout"
	OutcomeInfraError   = "infra_error"
)

// SubmitSpec builds the run spec that exercises every test case, seeding each
// case's files and collecting the files it is expected to produce.
func SubmitSpec(ex *content.Exercise, code string) runner.RunSpec {
	spec := runner.RunSpec{Code: code, TimeLimitMS: ex.Tests.TimeLimitMS}
	for _, c := range ex.Tests.Cases {
		rc := runner.Case{ID: c.ID, Stdin: c.Stdin, Files: c.Files}
		for p := range c.ExpectFiles {
			rc.Collect = append(rc.Collect, p)
		}
		spec.Cases = append(spec.Cases, rc)
	}
	return spec
}

// RunSpec builds the spec for a free run with the student's own stdin. The
// first case's seed files are provided so file-handling exercises have their
// input file present.
func RunSpec(ex *content.Exercise, code, stdin string) runner.RunSpec {
	var files map[string]string
	if len(ex.Tests.Cases) > 0 {
		files = ex.Tests.Cases[0].Files
	}
	return runner.RunSpec{Code: code, TimeLimitMS: ex.Tests.TimeLimitMS, Cases: []runner.Case{{ID: "run", Stdin: stdin, Files: files}}}
}

// CaseVerdict is the graded result of one test case.
type CaseVerdict struct {
	ID       string
	Hidden   bool
	Passed   bool
	TimedOut bool
	Stdin    string
	Expected string
	Actual   string
	Stderr   string
	Error    *runner.PyError
}

// Verdict is the graded result of a submit.
type Verdict struct {
	Outcome     string
	Passed      bool
	Cases       []CaseVerdict
	Error       *runner.PyError // syntax error, or the first runtime error
	FailingTest string          // id of the first failing case
}

// Evaluate grades a harness result against the exercise's cases. A syntax
// error fails every case with no per-case detail; otherwise each case is
// compared on stdout and any expected files.
func Evaluate(ex *content.Exercise, res runner.RunResult) Verdict {
	if res.SyntaxError != nil {
		v := Verdict{Outcome: OutcomeSyntaxError, Error: res.SyntaxError}
		for _, c := range ex.Tests.Cases {
			v.Cases = append(v.Cases, CaseVerdict{ID: c.ID, Hidden: c.Hidden})
		}
		if len(ex.Tests.Cases) > 0 {
			v.FailingTest = ex.Tests.Cases[0].ID
		}
		return v
	}

	byID := map[string]runner.CaseResult{}
	for _, r := range res.Cases {
		byID[r.ID] = r
	}
	v := Verdict{Outcome: OutcomePassed, Passed: true}
	for _, c := range ex.Tests.Cases {
		r, ran := byID[c.ID]
		cv := CaseVerdict{ID: c.ID, Hidden: c.Hidden, Stdin: c.Stdin, Expected: c.Stdout, Actual: r.Stdout, Stderr: r.Stderr, TimedOut: r.TimedOut, Error: r.Error}
		switch {
		case !ran:
			// The harness skipped it; treat as infrastructure, not the student.
			cv.Passed = false
		case r.TimedOut:
			cv.Passed = false
		case r.ExitCode != 0:
			cv.Passed = false
		default:
			cv.Passed = Equal(c.Stdout, r.Stdout, c.Strict) && filesMatch(c, r)
		}
		if !cv.Passed && v.Passed {
			// First failure decides the outcome and the hint target.
			v.Passed = false
			v.FailingTest = c.ID
			switch {
			case !ran:
				v.Outcome = OutcomeInfraError
			case r.TimedOut:
				v.Outcome = OutcomeTimeout
			case r.ExitCode != 0:
				v.Outcome = OutcomeRuntimeError
				v.Error = r.Error
			default:
				v.Outcome = OutcomeFailed
			}
		}
		v.Cases = append(v.Cases, cv)
	}
	return v
}

// filesMatch checks every expected output file against what the program wrote.
func filesMatch(c content.TestCase, r runner.CaseResult) bool {
	for p, want := range c.ExpectFiles {
		got, ok := r.Files[p]
		if !ok || !Equal(want, got, c.Strict) {
			return false
		}
	}
	return true
}

// RunOutcome classifies a free run (no expected output) for the record.
func RunOutcome(res runner.RunResult) (outcome string, err *runner.PyError) {
	if res.SyntaxError != nil {
		return OutcomeSyntaxError, res.SyntaxError
	}
	if len(res.Cases) == 0 {
		return OutcomeInfraError, nil
	}
	c := res.Cases[0]
	switch {
	case c.TimedOut:
		return OutcomeTimeout, nil
	case c.ExitCode != 0:
		return OutcomeRuntimeError, c.Error
	}
	return OutcomePassed, nil
}
