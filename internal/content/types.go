// Package content loads the exercise library from the repository's content
// tree: tracks, exercises, their test cases, and the canned hints keyed by
// mistake pattern. Content is read once at boot and treated as immutable.
package content

import "regexp"

// Track is an ordered syllabus: units, each listing exercises in the order a
// student should attempt them.
type Track struct {
	ID    string `yaml:"id"`
	Title string `yaml:"title"`
	Units []Unit `yaml:"units"`
}

// Unit groups exercises for one syllabus topic. MustPass marks the exercises
// that gate "practical ready".
type Unit struct {
	ID        string   `yaml:"id"`
	Title     string   `yaml:"title"`
	Exercises []string `yaml:"exercises"`
	MustPass  []string `yaml:"must_pass"`
}

// Exercise is one problem: statement, starter code, reference solution, test
// cases, and canned hints. ID is "<track>/<slug>" and unique library-wide.
type Exercise struct {
	ID         string
	Title      string
	Concepts   []string
	Syllabus   string
	Difficulty int
	Statement  string // Markdown body of exercise.md, front matter stripped.
	Starter    string
	Solution   string
	Tests      Tests
	Hints      []Hint
	Dir        string // Filesystem directory the exercise was loaded from.
}

// frontMatter is the YAML header of exercise.md.
type frontMatter struct {
	ID         string   `yaml:"id"`
	Title      string   `yaml:"title"`
	Concepts   []string `yaml:"concepts"`
	Syllabus   string   `yaml:"syllabus"`
	Difficulty int      `yaml:"difficulty"`
}

// Tests is the contents of tests.yaml.
type Tests struct {
	TimeLimitMS int        `yaml:"time_limit_ms"`
	Cases       []TestCase `yaml:"cases"`
}

// TestCase is one black-box stdin→stdout check. Files are seeded into the
// working directory before the run; ExpectFiles are asserted afterwards.
type TestCase struct {
	ID          string            `yaml:"id"`
	Stdin       string            `yaml:"stdin"`
	Stdout      string            `yaml:"stdout"`
	Strict      bool              `yaml:"strict"`
	Hidden      bool              `yaml:"hidden"`
	Files       map[string]string `yaml:"files"`
	ExpectFiles map[string]string `yaml:"expect_files"`
}

// hintsFile is the contents of hints.yaml.
type hintsFile struct {
	Hints []Hint `yaml:"hints"`
}

// Hint is a canned Socratic nudge served when a student's failure matches its
// pattern, so no model call is needed for the common mistakes.
type Hint struct {
	ID       string    `yaml:"id"`
	Match    HintMatch `yaml:"match"`
	Text     HintText  `yaml:"text"`
	LineHint bool      `yaml:"line_hint"`
}

// HintMatch is an AND of its non-empty fields against an attempt's failure.
// Outcome scopes a hint to failed | runtime_error | syntax_error | timeout so a
// wrong-output hint keyed on a test id does not fire when that test crashed.
type HintMatch struct {
	Outcome     string `yaml:"outcome"`
	Exception   string `yaml:"exception"`
	Message     string `yaml:"message"`
	FailingTest string `yaml:"failing_test"`
	Flag        string `yaml:"flag"`

	messageRE *regexp.Regexp
}

// HintText carries the nudge in every supported tutor language.
type HintText struct {
	Hinglish string `yaml:"hinglish"`
	En       string `yaml:"en"`
}

// Library is the fully loaded, validated content tree.
type Library struct {
	Tracks    []Track
	Exercises map[string]*Exercise
}

// Exercise returns the exercise with the given id, or nil.
func (l *Library) Exercise(id string) *Exercise {
	return l.Exercises[id]
}

// MustPass reports whether the exercise gates practical-readiness in any unit.
func (l *Library) MustPass(exerciseID string) bool {
	for _, t := range l.Tracks {
		for _, u := range t.Units {
			for _, id := range u.MustPass {
				if id == exerciseID {
					return true
				}
			}
		}
	}
	return false
}

// VisibleCases returns the test cases whose expected output may be shown to
// the student.
func (e *Exercise) VisibleCases() []TestCase {
	var out []TestCase
	for _, c := range e.Tests.Cases {
		if !c.Hidden {
			out = append(out, c)
		}
	}
	return out
}

// TextIn returns the hint in the requested language, falling back to English
// and then Hinglish so a hint is never empty for a supported language.
func (h Hint) TextIn(lang string) string {
	if lang == "hinglish" && h.Text.Hinglish != "" {
		return h.Text.Hinglish
	}
	if h.Text.En != "" {
		return h.Text.En
	}
	return h.Text.Hinglish
}

// MessageRE returns the compiled message matcher, or nil when none is set.
func (m HintMatch) MessageRE() *regexp.Regexp {
	return m.messageRE
}
