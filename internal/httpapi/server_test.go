package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mayur-tolexo/tutor/internal/auth"
	"github.com/mayur-tolexo/tutor/internal/content"
	"github.com/mayur-tolexo/tutor/internal/runner"
	"github.com/mayur-tolexo/tutor/internal/store"
	"github.com/mayur-tolexo/tutor/internal/tutor"
)

// fakeRunner returns a scripted result or error and records the last spec.
type fakeRunner struct {
	res  runner.RunResult
	err  error
	last runner.RunSpec
	n    int
}

func (f *fakeRunner) Run(_ context.Context, spec runner.RunSpec) (runner.RunResult, error) {
	f.last = spec
	f.n++
	if f.err != nil {
		return runner.RunResult{}, f.err
	}
	r := f.res
	r.Duration = 120 * time.Millisecond
	return r, nil
}

// fakeGoogle accepts the token "good" for one fixed identity.
type fakeGoogle struct{}

func (fakeGoogle) Verify(_ context.Context, tok string) (auth.GoogleIdentity, error) {
	if tok != "good" {
		return auth.GoogleIdentity{}, errors.New("bad token")
	}
	return auth.GoogleIdentity{Sub: "sub1", Email: "s@example.com", Name: "Student One"}, nil
}

type env struct {
	srv    *Server
	h      http.Handler
	runner *fakeRunner
	store  *store.Memory
	now    time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	lib, err := content.Load(fstest.MapFS{
		"cbse-11/track.yaml":            {Data: []byte("title: Class 11\nunits:\n  - id: u1\n    title: Basics\n    exercises: [cbse-11/double]\n    must_pass: [cbse-11/double]\n")},
		"cbse-11/u1/double/exercise.md": {Data: []byte("---\nid: cbse-11/double\ntitle: Double it\nconcepts: [input-int]\nsyllabus: \"11.2\"\ndifficulty: 1\n---\nRead n, print 2n.\n")},
		"cbse-11/u1/double/starter.py":  {Data: []byte("n = int(input())\n")},
		"cbse-11/u1/double/solution.py": {Data: []byte("n = int(input())\nprint(n * 2)\n")},
		"cbse-11/u1/double/tests.yaml":  {Data: []byte("cases:\n  - id: two\n    stdin: \"2\\n\"\n    stdout: \"4\\n\"\n  - id: hid\n    stdin: \"5\\n\"\n    stdout: \"10\\n\"\n    hidden: true\n")},
		"cbse-11/u1/double/hints.yaml":  {Data: []byte("hints:\n  - id: name\n    match: {exception: NameError}\n    text: {hinglish: \"naam?\", en: \"name?\"}\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := auth.NewSessions([]byte(strings.Repeat("k", 32)), false)
	st := store.NewMemory()
	fr := &fakeRunner{}
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	srv := &Server{
		Library: lib, Runner: fr, Store: st, Sessions: sessions, Google: fakeGoogle{}, GoogleClientID: "cid",
		Tutor:  &tutor.Service{Store: st, Now: func() time.Time { return now }},
		Limits: DefaultLimits, Now: func() time.Time { return now },
		Static: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}, "assets/app.js": {Data: []byte("js")}},
	}
	return &env{srv: srv, h: srv.Handler(), runner: fr, store: st, now: now}
}

// call performs a JSON request as device dev with optional cookies.
func (e *env) call(t *testing.T, method, path string, body any, dev string, cookies ...*http.Cookie) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if dev != "" {
		req.Header.Set("X-Device-Id", dev)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

const dev = "11111111-2222-3333-4444-555555555555"

func TestContentEndpoints(t *testing.T) {
	e := newEnv(t)
	rec, out := e.call(t, http.MethodGet, "/v1/tracks", nil, "")
	if rec.Code != 200 {
		t.Fatalf("tracks status %d", rec.Code)
	}
	tracks := out["tracks"].([]any)
	ex := tracks[0].(map[string]any)["units"].([]any)[0].(map[string]any)["exercises"].([]any)[0].(map[string]any)
	if ex["id"] != "cbse-11/double" || ex["must_pass"] != true {
		t.Errorf("track exercise = %v", ex)
	}

	rec, out = e.call(t, http.MethodGet, "/v1/exercises/cbse-11/double", nil, "")
	if rec.Code != 200 || out["starter"] != "n = int(input())\n" {
		t.Fatalf("exercise = %d %v", rec.Code, out)
	}
	if cases := out["visible_cases"].([]any); len(cases) != 1 {
		t.Errorf("hidden case leaked: %v", cases)
	}
	if _, ok := out["solution"]; ok {
		t.Error("solution leaked")
	}
	if rec, _ := e.call(t, http.MethodGet, "/v1/exercises/nope", nil, ""); rec.Code != 404 {
		t.Errorf("missing exercise status %d", rec.Code)
	}
	if _, out := e.call(t, http.MethodGet, "/v1/config", nil, ""); out["google_client_id"] != "cid" {
		t.Errorf("config = %v", out)
	}
}

func TestIdentityRequired(t *testing.T) {
	e := newEnv(t)
	rec, out := e.call(t, http.MethodGet, "/v1/me", nil, "")
	if rec.Code != 400 || out["error"].(map[string]any)["code"] != "invalid" {
		t.Errorf("no device id: %d %v", rec.Code, out)
	}
	rec, out = e.call(t, http.MethodGet, "/v1/me", nil, dev)
	if rec.Code != 200 || out["signed_in"] != false || out["lang_pref"] != "hinglish" || out["device_id"] != dev {
		t.Errorf("me = %d %v", rec.Code, out)
	}
	rec, out = e.call(t, http.MethodPut, "/v1/me", map[string]string{"lang_pref": "en"}, dev)
	if rec.Code != 200 || out["lang_pref"] != "en" {
		t.Errorf("update me = %d %v", rec.Code, out)
	}
	if rec, _ := e.call(t, http.MethodPut, "/v1/me", map[string]string{"lang_pref": "fr"}, dev); rec.Code != 400 {
		t.Errorf("bad lang status %d", rec.Code)
	}
}

func TestSubmitPassRecordsProgress(t *testing.T) {
	e := newEnv(t)
	e.runner.res = runner.RunResult{Cases: []runner.CaseResult{{ID: "two", Stdout: "4\n"}, {ID: "hid", Stdout: "10\n"}}}
	rec, out := e.call(t, http.MethodPost, "/v1/attempts", map[string]string{"exercise_id": "cbse-11/double", "code": "print(int(input())*2)", "mode": "submit", "idempotency_key": "k1"}, dev)
	if rec.Code != 200 || out["outcome"] != "passed" || out["passed"] != true || out["attempt_id"] == "" {
		t.Fatalf("submit = %d %v", rec.Code, out)
	}
	if len(e.runner.last.Cases) != 2 {
		t.Errorf("submit should run every case, got %d", len(e.runner.last.Cases))
	}
	_, prog := e.call(t, http.MethodGet, "/v1/progress", nil, dev)
	if prog["exercises"].(map[string]any)["cbse-11/double"].(map[string]any)["status"] != "passed" {
		t.Errorf("progress = %v", prog)
	}

	// Same idempotency key replays the stored response without a second run.
	rec, again := e.call(t, http.MethodPost, "/v1/attempts", map[string]string{"exercise_id": "cbse-11/double", "code": "different", "mode": "submit", "idempotency_key": "k1"}, dev)
	if rec.Code != 200 || again["attempt_id"] != out["attempt_id"] || e.runner.n != 1 {
		t.Errorf("idempotent replay = %v runs=%d", again, e.runner.n)
	}
}

func TestSubmitFailRevealsOnlyFirstVisibleCase(t *testing.T) {
	e := newEnv(t)
	e.runner.res = runner.RunResult{Cases: []runner.CaseResult{{ID: "two", Stdout: "3\n"}, {ID: "hid", Stdout: "9\n"}}}
	rec, out := e.call(t, http.MethodPost, "/v1/attempts", map[string]string{"exercise_id": "cbse-11/double", "code": "x", "mode": "submit"}, dev)
	if rec.Code != 200 || out["outcome"] != "failed" {
		t.Fatalf("submit = %d %v", rec.Code, out)
	}
	cases := out["cases"].([]any)
	first, second := cases[0].(map[string]any), cases[1].(map[string]any)
	if first["expected"] != "4\n" || first["actual"] != "3\n" || first["stdin"] != "2\n" {
		t.Errorf("first failing case not revealed: %v", first)
	}
	if _, ok := second["expected"]; ok || second["hidden"] != true {
		t.Errorf("hidden case leaked: %v", second)
	}
	_, prog := e.call(t, http.MethodGet, "/v1/progress", nil, dev)
	if prog["exercises"].(map[string]any)["cbse-11/double"].(map[string]any)["status"] != "attempted" {
		t.Errorf("progress = %v", prog)
	}
}

func TestRunModeReturnsRawOutput(t *testing.T) {
	e := newEnv(t)
	e.runner.res = runner.RunResult{Cases: []runner.CaseResult{{ID: "run", Stdout: "hello\n", Stderr: "", ExitCode: 0}}}
	rec, out := e.call(t, http.MethodPost, "/v1/attempts", map[string]string{"exercise_id": "cbse-11/double", "code": "print('hello')", "mode": "run", "stdin": "7\n"}, dev)
	if rec.Code != 200 || out["run"].(map[string]any)["stdout"] != "hello\n" || out["outcome"] != "passed" {
		t.Fatalf("run = %d %v", rec.Code, out)
	}
	if e.runner.last.Cases[0].Stdin != "7\n" || len(e.runner.last.Cases) != 1 {
		t.Errorf("run spec = %+v", e.runner.last)
	}
	// A run never touches progress.
	_, prog := e.call(t, http.MethodGet, "/v1/progress", nil, dev)
	if len(prog["exercises"].(map[string]any)) != 0 {
		t.Errorf("run recorded progress: %v", prog)
	}
}

func TestAttemptValidationAndInfraErrors(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		body map[string]string
		code int
	}{
		{map[string]string{"exercise_id": "cbse-11/double", "code": "x", "mode": "fly"}, 400},
		{map[string]string{"exercise_id": "nope", "code": "x", "mode": "run"}, 404},
		{map[string]string{"exercise_id": "cbse-11/double", "code": strings.Repeat("x", 33<<10), "mode": "run"}, 400},
	}
	for _, c := range cases {
		if rec, _ := e.call(t, http.MethodPost, "/v1/attempts", c.body, dev); rec.Code != c.code {
			t.Errorf("%v: status %d, want %d", c.body["mode"], rec.Code, c.code)
		}
	}
	e.runner.err = runner.ErrBusy
	rec, out := e.call(t, http.MethodPost, "/v1/attempts", map[string]string{"exercise_id": "cbse-11/double", "code": "x", "mode": "run"}, dev)
	if rec.Code != 503 || out["error"].(map[string]any)["code"] != "pool_busy" || rec.Header().Get("Retry-After") == "" {
		t.Errorf("busy = %d %v", rec.Code, out)
	}
	e.runner.err = errors.New("boom")
	rec, out = e.call(t, http.MethodPost, "/v1/attempts", map[string]string{"exercise_id": "cbse-11/double", "code": "x", "mode": "run"}, dev)
	if rec.Code != 502 || out["error"].(map[string]any)["code"] != "infra_error" {
		t.Errorf("infra = %d %v", rec.Code, out)
	}
}

func TestRateLimits(t *testing.T) {
	e := newEnv(t)
	e.srv.Limits.RunsPerMinute = 2
	e.runner.res = runner.RunResult{Cases: []runner.CaseResult{{ID: "run"}}}
	body := map[string]string{"exercise_id": "cbse-11/double", "code": "x", "mode": "run"}
	for i := 0; i < 2; i++ {
		if rec, _ := e.call(t, http.MethodPost, "/v1/attempts", body, dev); rec.Code != 200 {
			t.Fatalf("run %d status %d", i, rec.Code)
		}
	}
	rec, out := e.call(t, http.MethodPost, "/v1/attempts", body, dev)
	if rec.Code != 429 || out["error"].(map[string]any)["retry_after_seconds"] == nil {
		t.Errorf("third run = %d %v", rec.Code, out)
	}

	// Daily limit is enforced through the store counter.
	e2 := newEnv(t)
	e2.srv.Limits.RunsPerDayAnon = 1
	e2.runner.res = e.runner.res
	e2.call(t, http.MethodPost, "/v1/attempts", body, dev)
	if rec, _ := e2.call(t, http.MethodPost, "/v1/attempts", body, dev); rec.Code != 429 {
		t.Errorf("daily limit not enforced: %d", rec.Code)
	}
}

func TestHintFlowAndOwnership(t *testing.T) {
	e := newEnv(t)
	e.runner.res = runner.RunResult{Cases: []runner.CaseResult{{ID: "two", ExitCode: 1, Error: &runner.PyError{Type: "NameError", Message: "name 'x' is not defined", Line: 1}}, {ID: "hid", ExitCode: 1}}}
	_, att := e.call(t, http.MethodPost, "/v1/attempts", map[string]string{"exercise_id": "cbse-11/double", "code": "print(x)", "mode": "submit"}, dev)
	if att["outcome"] != "runtime_error" {
		t.Fatalf("attempt = %v", att)
	}
	rec, out := e.call(t, http.MethodPost, "/v1/hints", map[string]string{"attempt_id": att["attempt_id"].(string)}, dev)
	if rec.Code != 200 || out["source"] != "canned" || out["text"] != "naam?" || out["level"].(float64) != 1 {
		t.Fatalf("hint = %d %v", rec.Code, out)
	}
	// Another device cannot ask about this attempt.
	rec, _ = e.call(t, http.MethodPost, "/v1/hints", map[string]string{"attempt_id": att["attempt_id"].(string)}, "99999999-9999-9999-9999-999999999999")
	if rec.Code != 404 {
		t.Errorf("foreign attempt status %d", rec.Code)
	}
	if rec, _ := e.call(t, http.MethodPost, "/v1/hints", map[string]string{"attempt_id": "missing"}, dev); rec.Code != 404 {
		t.Errorf("missing attempt status %d", rec.Code)
	}
	if rec, _ := e.call(t, http.MethodPost, "/v1/hints", map[string]string{}, dev); rec.Code != 400 {
		t.Errorf("empty attempt id status %d", rec.Code)
	}
}

func TestGoogleSignInMergesProgressAndSessions(t *testing.T) {
	e := newEnv(t)
	e.runner.res = runner.RunResult{Cases: []runner.CaseResult{{ID: "two", Stdout: "4\n"}, {ID: "hid", Stdout: "10\n"}}}
	e.call(t, http.MethodPost, "/v1/attempts", map[string]string{"exercise_id": "cbse-11/double", "code": "ok", "mode": "submit"}, dev)

	if rec, _ := e.call(t, http.MethodPost, "/v1/auth/google", map[string]string{"id_token": "bad"}, dev); rec.Code != 401 {
		t.Errorf("bad token status %d", rec.Code)
	}
	rec, out := e.call(t, http.MethodPost, "/v1/auth/google", map[string]string{"id_token": "good"}, dev)
	if rec.Code != 200 || out["signed_in"] != true || out["display_name"] != "Student One" {
		t.Fatalf("sign-in = %d %v", rec.Code, out)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}

	// A brand new device with the cookie sees the merged progress.
	dev2 := "22222222-2222-2222-2222-222222222222"
	_, me := e.call(t, http.MethodGet, "/v1/me", nil, dev2, cookie)
	if me["signed_in"] != true {
		t.Errorf("me on second device = %v", me)
	}
	_, prog := e.call(t, http.MethodGet, "/v1/progress", nil, dev2, cookie)
	if prog["exercises"].(map[string]any)["cbse-11/double"].(map[string]any)["status"] != "passed" {
		t.Errorf("merged progress = %v", prog)
	}

	// Language preference now lives on the student and survives devices.
	e.call(t, http.MethodPut, "/v1/me", map[string]string{"lang_pref": "en"}, dev2, cookie)
	_, me = e.call(t, http.MethodGet, "/v1/me", nil, dev, cookie)
	if me["lang_pref"] != "en" {
		t.Errorf("student lang not shared: %v", me)
	}

	rec, _ = e.call(t, http.MethodPost, "/v1/auth/logout", nil, dev, cookie)
	if rec.Code != 204 {
		t.Errorf("logout status %d", rec.Code)
	}
	_, me = e.call(t, http.MethodGet, "/v1/me", nil, dev)
	if me["signed_in"] != false {
		t.Errorf("still signed in without cookie: %v", me)
	}
}

func TestStaticSPAFallback(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/ex/cbse-11/double", "/index.html"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "tutor_pool_busy_total") {
		t.Errorf("metrics: %d", rec.Code)
	}
}
