package tutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/mayur-tolexo/tutor/internal/content"
	"github.com/mayur-tolexo/tutor/internal/store"
)

// Hint sources, recorded on every hint for cost and quality tracking.
const (
	SourceCanned   = "canned"
	SourceCache    = "cache"
	SourceModel    = "model"
	SourceDegraded = "degraded"
)

// MaxLevel caps the ladder: level 3 is as specific as a hint gets.
const MaxLevel = 3

// Service assembles hints. LLM may be nil, in which case only canned and
// cached hints are served.
type Service struct {
	Store store.Store
	LLM   LLM
	// DailyModelBudget caps model calls per day across all students; 0 means
	// unlimited. Once hit, hints degrade to canned/cache/generic.
	DailyModelBudget int
	Now              func() time.Time
	Log              *slog.Logger
}

// Request is everything needed to produce one hint.
type Request struct {
	Attempt  store.Attempt
	Exercise *content.Exercise
	Owner    store.Owner
	Lang     string // hinglish | en
	Question string // optional free-text from the student
}

// Hint produces and records a hint for a failed attempt.
func (s *Service) Hint(ctx context.Context, req Request) (store.Hint, error) {
	now := s.now()
	f := failureOf(req.Attempt)
	level, err := s.level(ctx, req.Owner, req.Exercise.ID)
	if err != nil {
		return store.Hint{}, err
	}
	h := store.Hint{
		AttemptID:  req.Attempt.ID,
		Owner:      req.Owner,
		ExerciseID: req.Exercise.ID,
		Level:      level,
		Lang:       req.Lang,
		CreatedAt:  now,
	}

	// Canned hints answer the common mistakes without a model call. A
	// student's explicit question bypasses them since it may ask something else.
	if req.Question == "" {
		if canned, ok := MatchCanned(req.Exercise.Hints, f); ok {
			h.Source, h.Text = SourceCanned, canned.TextIn(req.Lang)
			if canned.LineHint {
				h.Line = f.ErrorLine
			}
			return h, s.Store.CreateHint(ctx, &h)
		}
	}

	key := CacheKey(req.Exercise.ID, level, req.Lang, f)
	if req.Question == "" {
		if e, err := s.Store.GetCache(ctx, key); err == nil {
			h.Source, h.Text, h.Model = SourceCache, e.Text, e.Model
			return h, s.Store.CreateHint(ctx, &h)
		}
	}

	if s.LLM == nil || !s.withinBudget(ctx, now) {
		h.Source, h.Text = SourceDegraded, genericHint(f, req.Lang)
		return h, s.Store.CreateHint(ctx, &h)
	}

	text, resp, err := s.generate(ctx, req, f, level)
	if err != nil {
		s.log().Warn("model hint failed", "err", err, "exercise", req.Exercise.ID)
		h.Source, h.Text = SourceDegraded, genericHint(f, req.Lang)
		return h, s.Store.CreateHint(ctx, &h)
	}
	h.Source, h.Text, h.Model, h.Tokens = SourceModel, text, resp.Model, resp.Tokens
	if req.Question == "" {
		// Only question-free hints are reusable: the cache key does not include
		// the question text.
		if err := s.Store.PutCache(ctx, store.CacheEntry{Key: key, Text: text, Lang: req.Lang, Model: resp.Model}); err != nil {
			s.log().Warn("hint cache put failed", "err", err)
		}
	}
	return h, s.Store.CreateHint(ctx, &h)
}

// level computes the ladder position: one more than the hints given on this
// exercise since the student last passed it, capped at MaxLevel.
func (s *Service) level(ctx context.Context, owner store.Owner, exerciseID string) (int, error) {
	var since time.Time
	prog, err := s.Store.ListProgress(ctx, owner)
	if err != nil {
		return 0, err
	}
	if p, ok := prog[exerciseID]; ok && p.PassedAt != nil {
		since = *p.PassedAt
	}
	hints, err := s.Store.HintsSince(ctx, owner, exerciseID, since)
	if err != nil {
		return 0, err
	}
	return min(len(hints)+1, MaxLevel), nil
}

// withinBudget increments today's global model-call counter and reports
// whether the call is allowed. The counter is bumped before the call so a
// burst cannot overshoot the cap.
func (s *Service) withinBudget(ctx context.Context, now time.Time) bool {
	if s.DailyModelBudget <= 0 {
		return true
	}
	n, err := s.Store.IncrUsage(ctx, now.UTC().Format("2006-01-02"), "model_calls", "global", 1)
	if err != nil {
		s.log().Warn("usage counter failed", "err", err)
		return false
	}
	return n <= s.DailyModelBudget
}

// generate asks the model, rejecting a reply that leaks the solution; one
// stricter retry is allowed before giving up.
func (s *Service) generate(ctx context.Context, req Request, f Failure, level int) (string, ChatResponse, error) {
	system := systemPrompt(req.Lang, level)
	user := userPrompt(req, f)
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := s.LLM.Complete(ctx, ChatRequest{System: system, User: user, MaxTokens: 220, Temperature: 0.3})
		if err != nil {
			return "", ChatResponse{}, err
		}
		if !LeaksSolution(resp.Text, req.Exercise.Solution) {
			return resp.Text, resp, nil
		}
		last = errors.New("model reply contained the solution")
		system += "\n\nSTRICT: your previous reply contained too much code. Reply in prose only, with no code block."
	}
	return "", ChatResponse{}, last
}

// now returns the injected clock or wall time.
func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// log returns the configured logger or the default.
func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// failureOf projects the stored attempt onto the matcher's view of it.
func failureOf(a store.Attempt) Failure {
	return Failure{
		Outcome:      a.Outcome,
		ErrorType:    a.ErrorType,
		ErrorMessage: a.ErrorMessage,
		ErrorLine:    a.ErrorLine,
		FailingTest:  a.FailingTest,
		Flags:        a.Flags,
	}
}

// systemPrompt sets the tutor persona. The ladder level controls how specific
// the nudge may be; even level 3 never hands over a working solution.
func systemPrompt(lang string, level int) string {
	var b strings.Builder
	b.WriteString("You are a patient Python tutor for an Indian Class 11 student preparing for the CBSE Computer Science practical. ")
	if lang == "hinglish" {
		b.WriteString("Reply in Hinglish: casual Hindi written in Latin script mixed with English technical words, the way a friendly senior explains. Keep Python keywords, error names and identifiers in English. ")
	} else {
		b.WriteString("Reply in simple, friendly English. ")
	}
	b.WriteString("Rules: at most 80 words. Never write the full solution or a corrected version of the program. ")
	switch level {
	case 1:
		b.WriteString("Level 1: ask one guiding question or point at the concept the student should re-check. No code.")
	case 2:
		b.WriteString("Level 2: name the specific line or expression that is wrong and explain why in one or two sentences. You may quote at most one line of the student's own code. No corrected code.")
	default:
		b.WriteString("Level 3: explain exactly what change is needed in words (for example 'convert the input to int before comparing'). You may show a tiny generic pattern of at most one line that is not the student's program. Never the whole fix.")
	}
	return b.String()
}

// userPrompt packs the exercise, code, and observed failure into one message.
func userPrompt(req Request, f Failure) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Exercise: %s\n\n%s\n\n", req.Exercise.Title, req.Exercise.Statement)
	fmt.Fprintf(&b, "Student's code:\n```python\n%s\n```\n\n", strings.TrimRight(req.Attempt.Code, "\n"))
	switch f.Outcome {
	case "syntax_error":
		fmt.Fprintf(&b, "Result: SyntaxError at line %d: %s\n", f.ErrorLine, f.ErrorMessage)
	case "runtime_error":
		fmt.Fprintf(&b, "Result: %s at line %d: %s\n", f.ErrorType, f.ErrorLine, f.ErrorMessage)
	case "timeout":
		b.WriteString("Result: the program did not finish within the time limit (probably an infinite loop or waiting for input that never comes).\n")
	default:
		b.WriteString("Result: wrong output.\n")
	}
	if c := firstFailingCase(req.Exercise, f.FailingTest); c != nil {
		fmt.Fprintf(&b, "Failing test input:\n%s\nExpected output:\n%s\n", indent(c.Stdin), indent(c.Stdout))
		if actual := actualOutput(req.Attempt, c.ID); actual != "" {
			fmt.Fprintf(&b, "Student's output:\n%s\n", indent(actual))
		}
	}
	if len(f.Flags) > 0 {
		fmt.Fprintf(&b, "Automated observations about the code: %s\n", strings.Join(f.Flags, ", "))
	}
	if req.Question != "" {
		fmt.Fprintf(&b, "\nStudent asks: %s\n", strings.TrimSpace(req.Question))
	}
	return b.String()
}

// firstFailingCase returns the exercise case with the given id if it is
// visible; hidden cases are never described to the model or the student.
func firstFailingCase(ex *content.Exercise, id string) *content.TestCase {
	for i := range ex.Tests.Cases {
		c := &ex.Tests.Cases[i]
		if c.ID == id && !c.Hidden {
			return c
		}
	}
	return nil
}

// actualOutput pulls the student's stdout for a case out of the stored result.
func actualOutput(a store.Attempt, caseID string) string {
	var r struct {
		Cases []struct {
			ID     string `json:"id"`
			Stdout string `json:"stdout"`
		} `json:"cases_raw"`
	}
	if err := json.Unmarshal(a.Result, &r); err != nil {
		return ""
	}
	for _, c := range r.Cases {
		if c.ID == caseID {
			return c.Stdout
		}
	}
	return ""
}

// indent prefixes each line so multi-line values read as blocks in the prompt.
func indent(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return "    (empty)"
	}
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}

var fenceRE = regexp.MustCompile("(?s)```[a-zA-Z]*\n(.*?)```")

// LeaksSolution reports whether reply hands over the answer: a fenced block of
// four or more lines, or most of the solution's lines appearing inside the
// reply (whitespace-insensitive, so "Line 1: n=int(input())" still counts).
func LeaksSolution(reply, solution string) bool {
	for _, m := range fenceRE.FindAllStringSubmatch(reply, -1) {
		if countLines(m[1]) >= 4 {
			return true
		}
	}
	var sol []string
	for _, l := range strings.Split(solution, "\n") {
		// Short lines like "else:" or "pass" are too common to be evidence.
		if n := squash(l); len(n) >= 6 {
			sol = append(sol, n)
		}
	}
	if len(sol) == 0 {
		return false
	}
	flat := squash(reply)
	overlap := 0
	for _, l := range sol {
		if strings.Contains(flat, l) {
			overlap++
		}
	}
	// Quoting one statement is legitimate tutoring; reproducing most of the
	// program is not.
	return overlap >= 2 && overlap*100 >= len(sol)*60
}

// squash strips comments and all whitespace so formatting differences vanish.
func squash(l string) string {
	if i := strings.Index(l, "#"); i >= 0 {
		l = l[:i]
	}
	return strings.Join(strings.Fields(l), "")
}

// countLines counts non-blank lines.
func countLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// genericHint is the last-resort nudge when neither canned nor model hints are
// available, keyed on the broad failure class.
func genericHint(f Failure, lang string) string {
	hinglish := lang == "hinglish"
	switch {
	case f.Outcome == "syntax_error":
		if hinglish {
			return fmt.Sprintf("Line %d ke aas-paas Python ko kuch samajh nahi aaya. Colon (:), brackets aur quotes check karo — har opening ka closing hai?", f.ErrorLine)
		}
		return fmt.Sprintf("Python could not parse the code near line %d. Check colons, brackets and quotes — does every opening have a closing?", f.ErrorLine)
	case f.Outcome == "timeout":
		if hinglish {
			return "Program khatam hi nahi hua. Koi loop hai jo kabhi rukta nahi? Loop ki condition kab False hogi, ye dhyaan se dekho."
		}
		return "The program never finished. Is there a loop that never stops? Check when its condition becomes False."
	case f.ErrorType == "NameError":
		if hinglish {
			return "Ek naam use ho raha hai jo pehle define nahi hua. Spelling aur capital letters check karo, aur dekho variable ko value assign ki hai ya nahi."
		}
		return "A name is used before it is defined. Check spelling and capitalisation, and whether the variable was assigned a value first."
	case f.ErrorType == "TypeError":
		if hinglish {
			return "Do alag type ki cheezein mix ho rahi hain (jaise str aur int). input() hamesha string deta hai — kya usko int() ya float() mein convert kiya?"
		}
		return "Two different types are being mixed (like str and int). input() always returns a string — did you convert it with int() or float()?"
	case f.ErrorType == "ValueError":
		if hinglish {
			return "Ek value ko convert karne ki koshish ho rahi hai jo us format mein nahi hai. Kya input exactly wahi hai jo test deta hai?"
		}
		return "A value is being converted into a type it does not fit. Is the input exactly what the test provides?"
	case f.ErrorType == "IndexError":
		if hinglish {
			return "List ya string ke bahar ka index access ho raha hai. Yaad rakho index 0 se shuru hota hai aur last index len()-1 hai."
		}
		return "An index beyond the end of a list or string is being accessed. Remember indexes start at 0 and end at len()-1."
	case f.ErrorType == "IndentationError":
		if hinglish {
			return "Indentation galat hai. Har block (if, for, while, def) ke andar ki lines ek jaisi 4 spaces se shuru honi chahiye."
		}
		return "Indentation is off. Every line inside a block (if, for, while, def) must start with the same 4 spaces."
	case f.ErrorType != "":
		if hinglish {
			return fmt.Sprintf("Program line %d par %s ke saath crash hua. Error message ko dhyaan se padho — woh bata raha hai kya galat hai.", f.ErrorLine, f.ErrorType)
		}
		return fmt.Sprintf("The program crashed on line %d with %s. Read the error message carefully — it says what went wrong.", f.ErrorLine, f.ErrorType)
	default:
		if hinglish {
			return "Output expected se match nahi kar raha. Example input lo, kaagaz par apna program line-by-line chalao, aur dekho kahan farak aata hai. Spacing aur newlines bhi matter karte hain."
		}
		return "The output does not match. Take the example input, trace your program line by line on paper, and see where it diverges. Spacing and newlines matter too."
	}
}
