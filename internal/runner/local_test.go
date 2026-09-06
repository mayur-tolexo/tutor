package runner

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// requirePython skips when no python3 is on PATH so the unit suite stays green
// on machines without an interpreter.
func requirePython(t *testing.T) *Local {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	return NewLocal(2)
}

func TestLocalRunsCasesAndParsesErrors(t *testing.T) {
	l := requirePython(t)
	code := "n = int(input())\nprint(n * 2)\nif n == 3:\n    xs = [1]\n    print(xs[5])\n"
	res, err := l.Run(context.Background(), RunSpec{
		Code:        code,
		TimeLimitMS: 2000,
		Cases:       []Case{{ID: "ok", Stdin: "2\n"}, {ID: "boom", Stdin: "3\n"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.SyntaxError != nil {
		t.Fatalf("unexpected syntax error: %+v", res.SyntaxError)
	}
	if len(res.Cases) != 2 {
		t.Fatalf("cases = %d", len(res.Cases))
	}
	if res.Cases[0].Stdout != "4\n" || res.Cases[0].ExitCode != 0 {
		t.Errorf("case ok = %+v", res.Cases[0])
	}
	e := res.Cases[1].Error
	if e == nil || e.Type != "IndexError" || e.Line != 5 || !strings.Contains(e.Message, "out of range") {
		t.Errorf("case boom error = %+v", e)
	}
	if res.Duration <= 0 {
		t.Error("duration not recorded")
	}
}

func TestLocalSyntaxErrorSkipsCases(t *testing.T) {
	l := requirePython(t)
	res, err := l.Run(context.Background(), RunSpec{Code: "print('hi'\n", Cases: []Case{{ID: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.SyntaxError == nil || res.SyntaxError.Line != 1 {
		t.Fatalf("syntax error = %+v", res.SyntaxError)
	}
	if len(res.Cases) != 0 {
		t.Errorf("cases should be skipped, got %d", len(res.Cases))
	}
}

func TestLocalTimeoutAndOutputCap(t *testing.T) {
	l := requirePython(t)
	res, err := l.Run(context.Background(), RunSpec{
		Code:        "while True:\n    print('x' * 1000)\n",
		TimeLimitMS: 300,
		Cases:       []Case{{ID: "loop"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Cases[0]
	if !c.TimedOut {
		t.Errorf("expected timeout, got %+v", c)
	}
	if len(c.Stdout) > 70*1024 {
		t.Errorf("stdout not capped: %d bytes", len(c.Stdout))
	}
	if !contains(res.Flags, "while_true_no_break") {
		t.Errorf("flags = %v", res.Flags)
	}
}

func TestLocalFilesSeededAndCollected(t *testing.T) {
	l := requirePython(t)
	code := "with open('in.txt') as f:\n    data = f.read()\nwith open('out.txt', 'w') as f:\n    f.write(data.upper())\n"
	res, err := l.Run(context.Background(), RunSpec{
		Code:  code,
		Cases: []Case{{ID: "f", Files: map[string]string{"in.txt": "abc"}, Collect: []string{"out.txt", "missing.txt"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Cases[0].Files["out.txt"]; got != "ABC" {
		t.Errorf("out.txt = %q (files=%v)", got, res.Cases[0].Files)
	}
	if _, ok := res.Cases[0].Files["missing.txt"]; ok {
		t.Error("missing file should be absent, not empty")
	}
}

func TestLocalFlags(t *testing.T) {
	l := requirePython(t)
	cases := []struct {
		code string
		want []string
	}{
		{"x = input()\nif x > 5:\n    print(x)\n", []string{"input_not_converted", "no_function_def"}},
		{"print('Age: ' + 5)\n", []string{"string_int_concat", "no_input_call"}},
		{"def f(a):\n    print(a)\nf(1)\n", []string{"print_instead_of_return"}},
		{"try:\n    pass\nexcept:\n    pass\n", []string{"bare_except", "no_print_call"}},
		{"for i in range(3):\n    print('*')\n", []string{"loop_variable_unused"}},
		{"n = eval(input())\nprint(n / 2)\n", []string{"uses_eval", "uses_float_division"}},
	}
	for _, c := range cases {
		res, err := l.Run(context.Background(), RunSpec{Code: c.code})
		if err != nil {
			t.Fatalf("%q: %v", c.code, err)
		}
		for _, f := range c.want {
			if !contains(res.Flags, f) {
				t.Errorf("%q: missing flag %s in %v", c.code, f, res.Flags)
			}
			if !KnownFlags[f] {
				t.Errorf("flag %s emitted but not in KnownFlags", f)
			}
		}
	}
}

func TestLocalRespectsContext(t *testing.T) {
	l := requirePython(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// A sleeping program makes the outer deadline fire before the harness's own
	// 5 s per-case limit, so the caller's context must win.
	_, err := l.Run(ctx, RunSpec{Code: "import time\ntime.sleep(5)\n", TimeLimitMS: 5000, Cases: []Case{{ID: "slow"}}})
	if err == nil {
		t.Fatal("expected context error")
	}
}

// contains reports whether xs holds s.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
