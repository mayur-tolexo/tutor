// Package grade compares a program's output with the expected output.
package grade

import "strings"

// Equal reports whether got matches want. In tolerant mode trailing
// whitespace on each line and trailing blank lines are ignored, so a student
// is not failed for `print()` spacing the statement did not specify; strict
// mode requires byte equality.
func Equal(want, got string, strict bool) bool {
	if strict {
		return want == got
	}
	return Normalize(want) == Normalize(got)
}

// Normalize applies the tolerant comparison rules: CRLF→LF, trailing spaces
// stripped per line, trailing blank lines dropped.
func Normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	// Drop trailing empty lines so a missing or extra final newline is neutral.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
