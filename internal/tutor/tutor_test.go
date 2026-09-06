package tutor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mayur-tolexo/tutor/internal/content"
	"github.com/mayur-tolexo/tutor/internal/store"
)

// loadExercise builds an exercise through the real loader so hint regexes are
// compiled exactly as in production.
func loadExercise(t *testing.T, hintsYAML string) *content.Exercise {
	t.Helper()
	fsys := fstest.MapFS{
		"t/track.yaml":    {Data: []byte("units:\n  - id: u\n    title: U\n    exercises: [t/e]\n")},
		"t/e/exercise.md": {Data: []byte("---\nid: t/e\ntitle: Double\ndifficulty: 1\n---\nRead n, print 2n.\n")},
		"t/e/starter.py":  {Data: []byte("")},
		"t/e/solution.py": {Data: []byte("n = int(input())\nprint(n * 2)\n")},
		"t/e/tests.yaml":  {Data: []byte("cases:\n  - id: two\n    stdin: \"2\\n\"\n    stdout: \"4\\n\"\n  - id: hid\n    stdin: \"3\\n\"\n    stdout: \"6\\n\"\n    hidden: true\n")},
	}
	if hintsYAML != "" {
		fsys["t/e/hints.yaml"] = &fstest.MapFile{Data: []byte(hintsYAML)}
	}
	lib, err := content.Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return lib.Exercise("t/e")
}

const hints = `
hints:
  - id: name
    match: {exception: NameError, message: "name '\\w+'"}
    text: {hinglish: "naam check karo", en: "check the name"}
    line_hint: true
  - id: wrong-two
    match: {failing_test: two, flag: uses_float_division}
    text: {en: "integer division?"}
  - id: wrong-output-two
    match: {outcome: failed, failing_test: two}
    text: {en: "check the output for the first case"}
`

func TestMatchCanned(t *testing.T) {
	ex := loadExercise(t, hints)
	h, ok := MatchCanned(ex.Hints, Failure{ErrorType: "NameError", ErrorMessage: "name 'x' is not defined"})
	if !ok || h.ID != "name" {
		t.Errorf("NameError match = %+v, %v", h, ok)
	}
	if _, ok := MatchCanned(ex.Hints, Failure{ErrorType: "NameError", ErrorMessage: "weird"}); ok {
		t.Error("message regex should have rejected")
	}
	// AND semantics: failing test alone is not enough without the flag.
	if _, ok := MatchCanned(ex.Hints, Failure{FailingTest: "two"}); ok {
		t.Error("partial match should fail")
	}
	h, ok = MatchCanned(ex.Hints, Failure{FailingTest: "two", Flags: []string{"uses_float_division"}})
	if !ok || h.ID != "wrong-two" {
		t.Errorf("flag match = %+v, %v", h, ok)
	}
	// An outcome-scoped hint fires for wrong output but not for a crash on the same case.
	h, ok = MatchCanned(ex.Hints, Failure{Outcome: "failed", FailingTest: "two"})
	if !ok || h.ID != "wrong-output-two" {
		t.Errorf("outcome match = %+v, %v", h, ok)
	}
	if _, ok := MatchCanned(ex.Hints, Failure{Outcome: "runtime_error", ErrorType: "ZeroDivisionError", FailingTest: "two"}); ok {
		t.Error("wrong-output hint must not fire on a runtime error")
	}
}

func TestCacheKeyNormalisesIdentifiers(t *testing.T) {
	a := CacheKey("t/e", 1, "hinglish", Failure{Outcome: "runtime_error", ErrorType: "NameError", ErrorMessage: "name 'x' is not defined"})
	b := CacheKey("t/e", 1, "hinglish", Failure{Outcome: "runtime_error", ErrorType: "NameError", ErrorMessage: "name 'total' is not defined"})
	c := CacheKey("t/e", 2, "hinglish", Failure{Outcome: "runtime_error", ErrorType: "NameError", ErrorMessage: "name 'x' is not defined"})
	if a != b {
		t.Error("identifier should not change the key")
	}
	if a == c {
		t.Error("level should change the key")
	}
}

func TestLeaksSolution(t *testing.T) {
	sol := "n = int(input())\nprint(n * 2)\n"
	cases := []struct {
		reply string
		leak  bool
	}{
		{"Input ko int mein convert karna hai. Socho: input() kya return karta hai?", false},
		{"Try `int(input())` for the first line.", false},
		{"```python\nn = int(input())\nprint(n * 2)\n```", true},
		{"Yeh karo:\n```python\na = 1\nb = 2\nc = 3\nd = 4\n```", true},
		{"Line 1: n = int(input())\nLine 2: print(n*2)", true},
	}
	for _, c := range cases {
		if got := LeaksSolution(c.reply, sol); got != c.leak {
			t.Errorf("LeaksSolution(%q) = %v, want %v", c.reply, got, c.leak)
		}
	}
}

// fakeLLM returns scripted replies in order and records prompts.
type fakeLLM struct {
	replies []string
	calls   []ChatRequest
	err     error
}

func (f *fakeLLM) Complete(_ context.Context, req ChatRequest) (ChatResponse, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return ChatResponse{}, f.err
	}
	r := f.replies[0]
	if len(f.replies) > 1 {
		f.replies = f.replies[1:]
	}
	return ChatResponse{Text: r, Model: "fake", Tokens: 42}, nil
}

func newService(st store.Store, llm LLM) *Service {
	return &Service{Store: st, LLM: llm, Now: func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }}
}

func seedAttempt(t *testing.T, st store.Store, a store.Attempt) store.Attempt {
	t.Helper()
	if a.DeviceID == "" {
		a.DeviceID = "dev"
	}
	a.ExerciseID = "t/e"
	if err := st.CreateAttempt(context.Background(), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHintPrefersCannedThenCacheThenModel(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ex := loadExercise(t, hints)
	llm := &fakeLLM{replies: []string{"Soch: input() string deta hai ya number?"}}
	svc := newService(st, llm)
	owner := store.Owner{Kind: store.OwnerDevice, ID: "dev"}

	// Canned: NameError with a matching message; line_hint carries the line.
	a := seedAttempt(t, st, store.Attempt{Outcome: "runtime_error", ErrorType: "NameError", ErrorMessage: "name 'x' is not defined", ErrorLine: 2, Code: "print(x)"})
	h, err := svc.Hint(ctx, Request{Attempt: a, Exercise: ex, Owner: owner, Lang: "hinglish"})
	if err != nil || h.Source != SourceCanned || h.Text != "naam check karo" || h.Line != 2 || h.Level != 1 {
		t.Fatalf("canned hint = %+v, %v", h, err)
	}
	if len(llm.calls) != 0 {
		t.Fatal("model must not be called for a canned hint")
	}

	// Model: TypeError has no canned hint. Level is now 2 (one hint given).
	b := seedAttempt(t, st, store.Attempt{Outcome: "runtime_error", ErrorType: "TypeError", ErrorMessage: "'>' not supported between instances of 'str' and 'int'", ErrorLine: 2, Code: "n = input()\nif n > 5:\n    print(n)", Result: []byte(`{"cases":[]}`)})
	h, err = svc.Hint(ctx, Request{Attempt: b, Exercise: ex, Owner: owner, Lang: "hinglish"})
	if err != nil || h.Source != SourceModel || h.Level != 2 || h.Model != "fake" || h.Tokens != 42 {
		t.Fatalf("model hint = %+v, %v", h, err)
	}
	if len(llm.calls) != 1 || !strings.Contains(llm.calls[0].System, "Level 2") || !strings.Contains(llm.calls[0].User, "n = input()") {
		t.Errorf("prompt = %+v", llm.calls)
	}

	// Cache: a different student with the same mistake at the same level hits the cache.
	other := store.Owner{Kind: store.OwnerDevice, ID: "dev2"}
	st.CreateHint(ctx, &store.Hint{Owner: other, ExerciseID: "t/e", Level: 1, Source: SourceCanned, Text: "x"}) // put them at level 2 too
	c := seedAttempt(t, st, store.Attempt{DeviceID: "dev2", Outcome: "runtime_error", ErrorType: "TypeError", ErrorMessage: "'>' not supported between instances of 'str' and 'int'", Code: "m = input()"})
	h, err = svc.Hint(ctx, Request{Attempt: c, Exercise: ex, Owner: other, Lang: "hinglish"})
	if err != nil || h.Source != SourceCache || h.Text != "Soch: input() string deta hai ya number?" {
		t.Fatalf("cache hint = %+v, %v", h, err)
	}
	if len(llm.calls) != 1 {
		t.Error("cache hit must not call the model")
	}
}

func TestHintLadderCapsAndResetsOnPass(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ex := loadExercise(t, "")
	svc := newService(st, &fakeLLM{replies: []string{"prose hint"}})
	owner := store.Owner{Kind: store.OwnerDevice, ID: "dev"}
	a := seedAttempt(t, st, store.Attempt{Outcome: "failed", FailingTest: "two", Code: "print(1)", Result: []byte(`{"cases_raw":[{"id":"two","stdout":"1\n"}]}`)})

	var levels []int
	for i := 0; i < 5; i++ {
		h, err := svc.Hint(ctx, Request{Attempt: a, Exercise: ex, Owner: owner, Lang: "en"})
		if err != nil {
			t.Fatal(err)
		}
		levels = append(levels, h.Level)
	}
	if want := []int{1, 2, 3, 3, 3}; !equalInts(levels, want) {
		t.Errorf("levels = %v, want %v", levels, want)
	}

	// Passing resets the ladder for later failures.
	st.RecordProgress(ctx, owner, "t/e", "passed", a.ID, svc.Now().Add(time.Minute))
	svc.Now = func() time.Time { return time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC) }
	h, _ := svc.Hint(ctx, Request{Attempt: a, Exercise: ex, Owner: owner, Lang: "en"})
	if h.Level != 1 {
		t.Errorf("level after pass = %d", h.Level)
	}
}

func TestHintQuestionSkipsCannedAndCache(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ex := loadExercise(t, hints)
	llm := &fakeLLM{replies: []string{"answer"}}
	svc := newService(st, llm)
	owner := store.Owner{Kind: store.OwnerDevice, ID: "dev"}
	a := seedAttempt(t, st, store.Attempt{Outcome: "runtime_error", ErrorType: "NameError", ErrorMessage: "name 'x' is not defined", Code: "print(x)"})
	h, err := svc.Hint(ctx, Request{Attempt: a, Exercise: ex, Owner: owner, Lang: "en", Question: "what is a variable?"})
	if err != nil || h.Source != SourceModel || !strings.Contains(llm.calls[0].User, "what is a variable?") {
		t.Fatalf("hint = %+v, %v, calls=%+v", h, err, llm.calls)
	}
	if _, err := st.GetCache(ctx, CacheKey("t/e", 1, "en", failureOf(a))); err == nil {
		t.Error("question answers must not be cached")
	}
}

func TestHintRetriesLeakThenDegrades(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ex := loadExercise(t, "")
	leak := "```python\nn = int(input())\nprint(n * 2)\n```"
	llm := &fakeLLM{replies: []string{leak, leak}}
	svc := newService(st, llm)
	owner := store.Owner{Kind: store.OwnerDevice, ID: "dev"}
	a := seedAttempt(t, st, store.Attempt{Outcome: "runtime_error", ErrorType: "TypeError", ErrorMessage: "bad", Code: "x"})
	h, err := svc.Hint(ctx, Request{Attempt: a, Exercise: ex, Owner: owner, Lang: "en"})
	if err != nil || h.Source != SourceDegraded || !strings.Contains(h.Text, "input() always returns a string") {
		t.Fatalf("hint = %+v, %v", h, err)
	}
	if len(llm.calls) != 2 || !strings.Contains(llm.calls[1].System, "STRICT") {
		t.Errorf("expected one strict retry, got %d calls", len(llm.calls))
	}
}

func TestHintDegradesWithoutModelOrOverBudget(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ex := loadExercise(t, "")
	owner := store.Owner{Kind: store.OwnerDevice, ID: "dev"}
	a := seedAttempt(t, st, store.Attempt{Outcome: "timeout", Code: "while True: pass"})

	svc := newService(st, nil)
	h, err := svc.Hint(ctx, Request{Attempt: a, Exercise: ex, Owner: owner, Lang: "hinglish"})
	if err != nil || h.Source != SourceDegraded || !strings.Contains(h.Text, "loop") {
		t.Fatalf("no-LLM hint = %+v, %v", h, err)
	}

	llm := &fakeLLM{replies: []string{"ok"}}
	svc = newService(st, llm)
	svc.DailyModelBudget = 1
	b := seedAttempt(t, st, store.Attempt{Outcome: "runtime_error", ErrorType: "ValueError", ErrorMessage: "invalid literal", Code: "int('a')"})
	if h, _ := svc.Hint(ctx, Request{Attempt: b, Exercise: ex, Owner: owner, Lang: "en"}); h.Source != SourceModel {
		t.Errorf("first call within budget = %+v", h)
	}
	c := seedAttempt(t, st, store.Attempt{Outcome: "runtime_error", ErrorType: "IndexError", ErrorMessage: "list index out of range", Code: "[][1]"})
	if h, _ := svc.Hint(ctx, Request{Attempt: c, Exercise: ex, Owner: owner, Lang: "en"}); h.Source != SourceDegraded {
		t.Errorf("second call over budget = %+v", h)
	}
	if len(llm.calls) != 1 {
		t.Errorf("model calls = %d", len(llm.calls))
	}
}

func TestHintModelErrorDegrades(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ex := loadExercise(t, "")
	svc := newService(st, &fakeLLM{err: errors.New("boom")})
	a := seedAttempt(t, st, store.Attempt{Outcome: "syntax_error", ErrorType: "SyntaxError", ErrorLine: 3, Code: "print('x'"})
	h, err := svc.Hint(ctx, Request{Attempt: a, Exercise: ex, Owner: store.Owner{Kind: store.OwnerDevice, ID: "dev"}, Lang: "en"})
	if err != nil || h.Source != SourceDegraded || !strings.Contains(h.Text, "line 3") {
		t.Fatalf("hint = %+v, %v", h, err)
	}
}

func TestUserPromptHidesHiddenCases(t *testing.T) {
	ex := loadExercise(t, "")
	a := store.Attempt{Code: "x", Result: []byte(`{"cases_raw":[{"id":"hid","stdout":"9\n"}]}`)}
	p := userPrompt(Request{Attempt: a, Exercise: ex}, Failure{Outcome: "failed", FailingTest: "hid"})
	if strings.Contains(p, "Expected output") {
		t.Error("hidden case must not be described to the model")
	}
	p = userPrompt(Request{Attempt: store.Attempt{Code: "x", Result: []byte(`{"cases_raw":[{"id":"two","stdout":"1\n"}]}`)}, Exercise: ex}, Failure{Outcome: "failed", FailingTest: "two"})
	if !strings.Contains(p, "Expected output:\n    4") || !strings.Contains(p, "Student's output:\n    1") {
		t.Errorf("visible case missing from prompt:\n%s", p)
	}
}

// equalInts compares two int slices.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
