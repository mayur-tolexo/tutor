package grade

import (
	"testing"

	"github.com/mayur-tolexo/tutor/internal/content"
	"github.com/mayur-tolexo/tutor/internal/runner"
)

func exercise() *content.Exercise {
	return &content.Exercise{ID: "t/e", Tests: content.Tests{TimeLimitMS: 1500, Cases: []content.TestCase{
		{ID: "a", Stdin: "1\n", Stdout: "2\n"},
		{ID: "b", Stdin: "2\n", Stdout: "4\n", Hidden: true, Files: map[string]string{"in.txt": "x"}, ExpectFiles: map[string]string{"out.txt": "X"}},
	}}}
}

func TestSubmitSpecAndRunSpec(t *testing.T) {
	ex := exercise()
	s := SubmitSpec(ex, "code")
	if s.TimeLimitMS != 1500 || len(s.Cases) != 2 || s.Cases[1].Files["in.txt"] != "x" || s.Cases[1].Collect[0] != "out.txt" {
		t.Errorf("SubmitSpec = %+v", s)
	}
	r := RunSpec(ex, "code", "7\n")
	if len(r.Cases) != 1 || r.Cases[0].Stdin != "7\n" || r.Cases[0].ID != "run" {
		t.Errorf("RunSpec = %+v", r)
	}
}

func TestEvaluateOutcomes(t *testing.T) {
	ex := exercise()
	ok := runner.RunResult{Cases: []runner.CaseResult{
		{ID: "a", Stdout: "2\n"},
		{ID: "b", Stdout: "4\n", Files: map[string]string{"out.txt": "X\n"}},
	}}
	if v := Evaluate(ex, ok); !v.Passed || v.Outcome != OutcomePassed || v.FailingTest != "" {
		t.Errorf("all pass = %+v", v)
	}

	wrongFile := ok
	wrongFile.Cases = []runner.CaseResult{ok.Cases[0], {ID: "b", Stdout: "4\n", Files: map[string]string{"out.txt": "nope"}}}
	if v := Evaluate(ex, wrongFile); v.Passed || v.Outcome != OutcomeFailed || v.FailingTest != "b" || !v.Cases[1].Hidden {
		t.Errorf("wrong file = %+v", v)
	}

	crash := runner.RunResult{Cases: []runner.CaseResult{
		{ID: "a", ExitCode: 1, Error: &runner.PyError{Type: "ZeroDivisionError", Line: 2}},
		{ID: "b", TimedOut: true},
	}}
	v := Evaluate(ex, crash)
	if v.Outcome != OutcomeRuntimeError || v.Error == nil || v.Error.Type != "ZeroDivisionError" || v.FailingTest != "a" || !v.Cases[1].TimedOut {
		t.Errorf("crash = %+v", v)
	}

	timeout := runner.RunResult{Cases: []runner.CaseResult{{ID: "a", TimedOut: true}, {ID: "b", Stdout: "4\n"}}}
	if v := Evaluate(ex, timeout); v.Outcome != OutcomeTimeout || v.FailingTest != "a" {
		t.Errorf("timeout = %+v", v)
	}

	syntax := runner.RunResult{SyntaxError: &runner.PyError{Type: "SyntaxError", Line: 1}}
	if v := Evaluate(ex, syntax); v.Outcome != OutcomeSyntaxError || len(v.Cases) != 2 || v.FailingTest != "a" || v.Error == nil {
		t.Errorf("syntax = %+v", v)
	}

	missing := runner.RunResult{Cases: []runner.CaseResult{{ID: "a", Stdout: "2\n"}}}
	if v := Evaluate(ex, missing); v.Outcome != OutcomeInfraError || v.FailingTest != "b" {
		t.Errorf("missing case = %+v", v)
	}
}

func TestRunOutcome(t *testing.T) {
	if o, _ := RunOutcome(runner.RunResult{Cases: []runner.CaseResult{{ID: "run", Stdout: "hi"}}}); o != OutcomePassed {
		t.Errorf("ok run = %s", o)
	}
	if o, e := RunOutcome(runner.RunResult{Cases: []runner.CaseResult{{ID: "run", ExitCode: 1, Error: &runner.PyError{Type: "NameError"}}}}); o != OutcomeRuntimeError || e == nil {
		t.Errorf("crash run = %s %v", o, e)
	}
	if o, _ := RunOutcome(runner.RunResult{Cases: []runner.CaseResult{{ID: "run", TimedOut: true}}}); o != OutcomeTimeout {
		t.Errorf("timeout run = %s", o)
	}
	if o, _ := RunOutcome(runner.RunResult{}); o != OutcomeInfraError {
		t.Errorf("empty run = %s", o)
	}
}
