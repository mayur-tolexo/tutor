package content

import (
	"strings"
	"testing"
	"testing/fstest"
)

// validTree is a minimal well-formed content tree used as the base fixture.
func validTree() fstest.MapFS {
	return fstest.MapFS{
		"cbse-11/track.yaml": {Data: []byte(`
id: cbse-11
title: Class 11
units:
  - id: u1
    title: Basics
    exercises: [cbse-11/hello]
    must_pass: [cbse-11/hello]
`)},
		"cbse-11/01-basics/hello/exercise.md": {Data: []byte(`---
id: cbse-11/hello
title: Hello
concepts: [print]
syllabus: "11.1"
difficulty: 1
---
Print hello.
`)},
		"cbse-11/01-basics/hello/starter.py":  {Data: []byte("# write here\n")},
		"cbse-11/01-basics/hello/solution.py": {Data: []byte("print('hello')\n")},
		"cbse-11/01-basics/hello/tests.yaml": {Data: []byte(`
cases:
  - id: basic
    stdin: ""
    stdout: "hello\n"
`)},
		"cbse-11/01-basics/hello/hints.yaml": {Data: []byte(`
hints:
  - id: nameerror
    match: {exception: NameError, message: "name '(\\w+)'"}
    text: {hinglish: "Variable define kiya?", en: "Did you define it?"}
  - id: wrong
    match: {failing_test: basic}
    text: {en: "Check the output."}
`)},
	}
}

func TestLoadValid(t *testing.T) {
	lib, err := Load(validTree())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ex := lib.Exercise("cbse-11/hello")
	if ex == nil {
		t.Fatal("exercise not loaded")
	}
	if ex.Statement != "Print hello." {
		t.Errorf("statement = %q", ex.Statement)
	}
	if ex.Tests.TimeLimitMS != 2000 {
		t.Errorf("default time limit = %d", ex.Tests.TimeLimitMS)
	}
	if len(ex.Hints) != 2 || ex.Hints[0].Match.messageRE == nil {
		t.Errorf("hints not compiled: %+v", ex.Hints)
	}
	if !lib.MustPass("cbse-11/hello") {
		t.Error("must_pass not recognised")
	}
	if got := ex.Hints[1].TextIn("hinglish"); got != "Check the output." {
		t.Errorf("fallback text = %q", got)
	}
}

func TestLoadCollectsErrors(t *testing.T) {
	fsys := validTree()
	// Unknown exercise in track, bad regex, must_pass outside unit, bad difficulty.
	fsys["cbse-11/track.yaml"] = &fstest.MapFile{Data: []byte(`
units:
  - id: u1
    title: Basics
    exercises: [cbse-11/hello, cbse-11/missing]
    must_pass: [cbse-11/other]
`)}
	fsys["cbse-11/01-basics/hello/hints.yaml"] = &fstest.MapFile{Data: []byte(`
hints:
  - id: bad
    match: {message: "("}
    text: {en: x}
  - id: nomatch
    text: {en: x}
  - id: badtest
    match: {failing_test: nope}
    text: {en: x}
  - id: badoutcome
    match: {outcome: exploded}
    text: {en: x}
`)}
	_, err := Load(fsys)
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		`unknown exercise "cbse-11/missing"`,
		`must_pass "cbse-11/other"`,
		`hint "bad" message regex`,
		`hint "nomatch" has no matcher`,
		`refers to unknown test "nope"`,
		`hint "badoutcome" has unknown outcome "exploded"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing error %q in:\n%v", want, err)
		}
	}
}

func TestLoadRejectsMissingFrontMatterAndFiles(t *testing.T) {
	fsys := validTree()
	fsys["cbse-11/01-basics/hello/exercise.md"] = &fstest.MapFile{Data: []byte("no front matter")}
	delete(fsys, "cbse-11/01-basics/hello/solution.py")
	_, err := Load(fsys)
	if err == nil || !strings.Contains(err.Error(), "missing front matter") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckFlags(t *testing.T) {
	fsys := validTree()
	fsys["cbse-11/01-basics/hello/hints.yaml"] = &fstest.MapFile{Data: []byte(`
hints:
  - id: f
    match: {flag: no_such_flag}
    text: {en: x}
`)}
	lib, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if errs := lib.CheckFlags(map[string]bool{"no_print_call": true}); len(errs) != 1 {
		t.Errorf("CheckFlags errs = %v", errs)
	}
}

func TestVisibleCases(t *testing.T) {
	ex := &Exercise{Tests: Tests{Cases: []TestCase{{ID: "a"}, {ID: "b", Hidden: true}}}}
	if got := ex.VisibleCases(); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("VisibleCases = %+v", got)
	}
}
