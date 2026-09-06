package grade

import "testing"

func TestEqualTolerant(t *testing.T) {
	cases := []struct {
		want, got string
		ok        bool
	}{
		{"hello\n", "hello", true},
		{"hello\n", "hello  \n\n", true},
		{"a\nb\n", "a\r\nb\r\n", true},
		{"a\nb\n", "a\n\nb\n", false},
		{"5\n", "5.0\n", false},
		{"", "\n\n", true},
	}
	for _, c := range cases {
		if got := Equal(c.want, c.got, false); got != c.ok {
			t.Errorf("Equal(%q, %q) = %v, want %v", c.want, c.got, got, c.ok)
		}
	}
}

func TestEqualStrict(t *testing.T) {
	if Equal("hello\n", "hello", true) {
		t.Error("strict should require the trailing newline")
	}
	if !Equal("hello\n", "hello\n", true) {
		t.Error("strict equal failed on identical input")
	}
}
