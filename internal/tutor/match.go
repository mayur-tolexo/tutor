// Package tutor produces hints for a failed attempt: canned hints first, then
// an exact-match cache, then a language model whose output is checked for
// leaked solutions. The ladder escalates hint specificity across repeated
// asks on the same exercise.
package tutor

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/mayur-tolexo/tutor/internal/content"
)

// Failure is what went wrong in an attempt, in the terms hints match on.
type Failure struct {
	Outcome      string // failed | runtime_error | syntax_error | timeout
	ErrorType    string
	ErrorMessage string
	ErrorLine    int
	FailingTest  string
	Flags        []string
}

// MatchCanned returns the first hint whose every set matcher field matches f.
func MatchCanned(hints []content.Hint, f Failure) (content.Hint, bool) {
	flags := map[string]bool{}
	for _, fl := range f.Flags {
		flags[fl] = true
	}
	for _, h := range hints {
		if matches(h.Match, f, flags) {
			return h, true
		}
	}
	return content.Hint{}, false
}

// matches evaluates one matcher as an AND of its non-empty fields.
func matches(m content.HintMatch, f Failure, flags map[string]bool) bool {
	if m.Outcome != "" && m.Outcome != f.Outcome {
		return false
	}
	if m.Exception != "" && m.Exception != f.ErrorType {
		return false
	}
	if m.Message != "" {
		re := m.MessageRE()
		if re == nil || !re.MatchString(f.ErrorMessage) {
			return false
		}
	}
	if m.FailingTest != "" && m.FailingTest != f.FailingTest {
		return false
	}
	if m.Flag != "" && !flags[m.Flag] {
		return false
	}
	return true
}

var (
	quotedRE = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	numberRE = regexp.MustCompile(`\d+`)
)

// normalizeMessage erases the identifiers and numbers in an error message so
// "name 'x' is not defined" and "name 'total' is not defined" share a cache key.
func normalizeMessage(msg string) string {
	msg = quotedRE.ReplaceAllString(msg, "'?'")
	msg = numberRE.ReplaceAllString(msg, "#")
	return strings.ToLower(strings.TrimSpace(msg))
}

// CacheKey identifies a model hint reusable for the same mistake on the same
// exercise at the same ladder level and language.
func CacheKey(exerciseID string, level int, lang string, f Failure) string {
	parts := []string{exerciseID, string(rune('0' + level)), lang, f.Outcome, f.ErrorType, normalizeMessage(f.ErrorMessage), f.FailingTest}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}
