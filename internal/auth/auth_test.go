package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newSessions(t *testing.T) *Sessions {
	s, err := NewSessions([]byte(strings.Repeat("k", 32)), true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// cookieFrom extracts the session cookie set on a recorder.
func cookieFrom(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	return nil
}

func TestSessionRoundTrip(t *testing.T) {
	s := newSessions(t)
	rec := httptest.NewRecorder()
	s.Issue(rec, "student-1")
	c := cookieFrom(rec)
	if c == nil || !c.HttpOnly || !c.Secure {
		t.Fatalf("cookie = %+v", c)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	if got := s.StudentID(req); got != "student-1" {
		t.Errorf("StudentID = %q", got)
	}
}

func TestSessionRejectsTamperingAndExpiry(t *testing.T) {
	s := newSessions(t)
	rec := httptest.NewRecorder()
	s.Issue(rec, "student-1")
	c := cookieFrom(rec)

	tampered := *c
	tampered.Value = strings.Replace(c.Value, c.Value[:4], "AAAA", 1)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&tampered)
	if got := s.StudentID(req); got != "" {
		t.Errorf("tampered cookie accepted: %q", got)
	}

	other, _ := NewSessions([]byte(strings.Repeat("z", 32)), true)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	if got := other.StudentID(req); got != "" {
		t.Errorf("cookie signed with another key accepted: %q", got)
	}

	s.now = func() time.Time { return time.Now().Add(SessionTTL + time.Hour) }
	if got := s.StudentID(req); got != "" {
		t.Errorf("expired cookie accepted: %q", got)
	}
	if got := s.StudentID(httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Errorf("no cookie should yield empty, got %q", got)
	}
}

func TestSessionClear(t *testing.T) {
	s := newSessions(t)
	rec := httptest.NewRecorder()
	s.Clear(rec)
	if c := cookieFrom(rec); c == nil || c.MaxAge != -1 {
		t.Errorf("clear cookie = %+v", c)
	}
}

func TestNewSessionsRequiresStrongKey(t *testing.T) {
	if _, err := NewSessions([]byte("short"), true); err == nil {
		t.Error("short key accepted")
	}
}

func TestValidDeviceID(t *testing.T) {
	for id, ok := range map[string]bool{
		"3f2b6c0e-9a1d-4e7b-8c2f-1a2b3c4d5e6f": true,
		"abcdefgh":                             true,
		"short":                                false,
		"has space":                            false,
		"../../etc":                            false,
		strings.Repeat("a", 65):                false,
	} {
		if ValidDeviceID(id) != ok {
			t.Errorf("ValidDeviceID(%q) = %v", id, !ok)
		}
	}
}
