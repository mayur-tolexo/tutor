package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mayur-tolexo/tutor/internal/grade"
	"github.com/mayur-tolexo/tutor/internal/runner"
	"github.com/mayur-tolexo/tutor/internal/store"
	"github.com/mayur-tolexo/tutor/internal/tutor"
)

type attemptRequest struct {
	ExerciseID     string `json:"exercise_id"`
	Code           string `json:"code"`
	Mode           string `json:"mode"`
	Stdin          string `json:"stdin"`
	IdempotencyKey string `json:"idempotency_key"`
}

type runJSON struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

type caseResultJSON struct {
	ID       string  `json:"id"`
	Passed   bool    `json:"passed"`
	Hidden   bool    `json:"hidden"`
	TimedOut bool    `json:"timed_out"`
	Stdin    *string `json:"stdin,omitempty"`
	Expected *string `json:"expected,omitempty"`
	Actual   *string `json:"actual,omitempty"`
	Stderr   *string `json:"stderr,omitempty"`
}

type attemptResponse struct {
	AttemptID  string           `json:"attempt_id"`
	Outcome    string           `json:"outcome"`
	Passed     bool             `json:"passed"`
	DurationMS int              `json:"duration_ms"`
	Run        *runJSON         `json:"run,omitempty"`
	Cases      []caseResultJSON `json:"cases,omitempty"`
	Error      *runner.PyError  `json:"error,omitempty"`
}

// handleAttempt runs or submits code: gate, execute, grade, record, respond.
func (s *Server) handleAttempt(w http.ResponseWriter, r *http.Request, id identity) error {
	var req attemptRequest
	if err := decodeJSON(r, &req, int64(s.Limits.MaxCodeBytes+s.Limits.MaxStdinBytes+4096)); err != nil {
		return err
	}
	if req.Mode != "run" && req.Mode != "submit" {
		return errInvalid("mode must be run or submit")
	}
	if len(req.Code) > s.Limits.MaxCodeBytes {
		return errInvalid("code is too long")
	}
	if len(req.Stdin) > s.Limits.MaxStdinBytes {
		return errInvalid("stdin is too long")
	}
	ex := s.Library.Exercise(req.ExerciseID)
	if ex == nil {
		return errNotFound("exercise not found")
	}

	// A retried request (flaky network) returns the original result.
	if req.IdempotencyKey != "" {
		if prev, err := s.Store.AttemptByIdempotencyKey(r.Context(), id.Device.ID, req.IdempotencyKey); err == nil {
			var resp attemptResponse
			if json.Unmarshal(prev.Result, &resp) == nil {
				writeJSON(w, http.StatusOK, resp)
				return nil
			}
		}
	}

	if err := s.gateRun(r, id); err != nil {
		return err
	}

	var spec runner.RunSpec
	if req.Mode == "run" {
		spec = grade.RunSpec(ex, req.Code, req.Stdin)
	} else {
		spec = grade.SubmitSpec(ex, req.Code)
	}
	ctx, cancel := context.WithTimeout(r.Context(), spec.TotalBudget()+15*time.Second)
	defer cancel()
	res, err := s.Runner.Run(ctx, spec)
	if err != nil {
		if errors.Is(err, runner.ErrBusy) {
			s.metrics.poolBusy.Inc()
			return errPoolBusy()
		}
		s.Log.Error("run failed", "err", err, "exercise", ex.ID)
		s.metrics.attempts.WithLabelValues(req.Mode, grade.OutcomeInfraError).Inc()
		return errInfra("could not run your code right now")
	}
	s.metrics.runLatency.WithLabelValues(req.Mode).Observe(res.Duration.Seconds())

	att := store.Attempt{
		DeviceID: id.Device.ID, ExerciseID: ex.ID, Mode: req.Mode, IdempotencyKey: req.IdempotencyKey,
		Code: req.Code, Flags: res.Flags, DurationMS: int(res.Duration / time.Millisecond), CreatedAt: s.Now(),
	}
	if id.Student != nil {
		att.StudentID = id.Student.ID
	}
	resp := attemptResponse{DurationMS: att.DurationMS}
	if req.Mode == "run" {
		outcome, pyErr := grade.RunOutcome(res)
		att.Outcome = outcome
		setError(&att, pyErr)
		resp.Outcome, resp.Error = outcome, pyErr
		if len(res.Cases) > 0 {
			c := res.Cases[0]
			resp.Run = &runJSON{Stdout: c.Stdout, Stderr: c.Stderr, ExitCode: c.ExitCode, TimedOut: c.TimedOut}
		} else {
			resp.Run = &runJSON{}
		}
	} else {
		v := grade.Evaluate(ex, res)
		att.Outcome, att.Passed, att.FailingTest = v.Outcome, v.Passed, v.FailingTest
		setError(&att, v.Error)
		resp.Outcome, resp.Passed, resp.Error = v.Outcome, v.Passed, v.Error
		resp.Cases = casesJSON(v)
	}
	s.metrics.attempts.WithLabelValues(req.Mode, att.Outcome).Inc()

	// The full response plus the raw harness cases is stored so an idempotent
	// retry replays exactly what the student saw and the tutor can read outputs.
	att.ID = store.NewID()
	resp.AttemptID = att.ID
	att.Result, _ = json.Marshal(storedResult{resp, res.Cases})
	if err := s.Store.CreateAttempt(r.Context(), &att); err != nil {
		return err
	}

	if req.Mode == "submit" {
		status := "attempted"
		if att.Passed {
			status = "passed"
		}
		if err := s.Store.RecordProgress(r.Context(), id.Owner(), ex.ID, status, att.ID, att.CreatedAt); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

// storedResult is the persisted attempt result: the client response and the
// harness's per-case output.
type storedResult struct {
	attemptResponse
	HarnessCases []runner.CaseResult `json:"cases_raw"`
}

// setError copies the Python error onto the attempt for hint matching.
func setError(a *store.Attempt, e *runner.PyError) {
	if e == nil {
		return
	}
	a.ErrorType, a.ErrorMessage, a.ErrorLine = e.Type, e.Message, e.Line
}

// casesJSON renders per-case verdicts, revealing input/expected/actual only
// for the first failing visible case so the student debugs one thing at a time
// and hidden cases stay hidden.
func casesJSON(v grade.Verdict) []caseResultJSON {
	out := make([]caseResultJSON, 0, len(v.Cases))
	revealed := false
	for _, c := range v.Cases {
		cj := caseResultJSON{ID: c.ID, Passed: c.Passed, Hidden: c.Hidden, TimedOut: c.TimedOut}
		if !c.Passed && !c.Hidden && !revealed {
			revealed = true
			cj.Stdin, cj.Expected, cj.Actual = ptr(c.Stdin), ptr(c.Expected), ptr(c.Actual)
			if c.Stderr != "" {
				cj.Stderr = ptr(c.Stderr)
			}
		}
		out = append(out, cj)
	}
	return out
}

// ptr returns a pointer to s for optional JSON fields.
func ptr(s string) *string { return &s }

// gateRun applies the per-minute device and IP limits and the daily device
// limit, in that order, so the cheapest check runs first.
func (s *Server) gateRun(r *http.Request, id identity) error {
	if ok, retry := s.minute.Allow("dev:"+id.Device.ID, s.Limits.RunsPerMinute); !ok {
		s.metrics.rateLimited.WithLabelValues("runs_per_minute").Inc()
		return errRateLimited(retry)
	}
	if ok, retry := s.minute.Allow("ip:"+clientIP(r), s.Limits.IPRunsPerMinute); !ok {
		s.metrics.rateLimited.WithLabelValues("ip_runs_per_minute").Inc()
		return errRateLimited(retry)
	}
	limit := s.Limits.RunsPerDayAnon
	if id.Student != nil {
		limit = s.Limits.RunsPerDaySignedIn
	}
	n, err := s.Store.IncrUsage(r.Context(), s.day(), "runs", id.Owner().ID, 1)
	if err != nil {
		return err
	}
	if n > limit {
		s.metrics.rateLimited.WithLabelValues("runs_per_day").Inc()
		return errRateLimited(secondsToMidnight(s.Now()))
	}
	return nil
}

// secondsToMidnight is the retry delay for a daily limit.
func secondsToMidnight(now time.Time) int {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return int(next.Sub(now).Seconds()) + 1
}

type hintRequest struct {
	AttemptID string `json:"attempt_id"`
	Question  string `json:"question"`
}

type hintResponse struct {
	HintID string `json:"hint_id"`
	Level  int    `json:"level"`
	Source string `json:"source"`
	Lang   string `json:"lang"`
	Text   string `json:"text"`
	Line   int    `json:"line,omitempty"`
}

// handleHint produces a hint for one of the caller's own failed attempts.
func (s *Server) handleHint(w http.ResponseWriter, r *http.Request, id identity) error {
	var req hintRequest
	if err := decodeJSON(r, &req, 8<<10); err != nil {
		return err
	}
	if req.AttemptID == "" {
		return errInvalid("attempt_id is required")
	}
	if len(req.Question) > 500 {
		return errInvalid("question is too long")
	}
	att, err := s.Store.GetAttempt(r.Context(), req.AttemptID)
	if errors.Is(err, store.ErrNotFound) {
		return errNotFound("attempt not found")
	}
	if err != nil {
		return err
	}
	// Only the attempt's own device or account may ask about it.
	if att.DeviceID != id.Device.ID && (id.Student == nil || att.StudentID != id.Student.ID) {
		return errNotFound("attempt not found")
	}
	if att.Passed && req.Question == "" {
		return errInvalid("this attempt passed; nothing to hint")
	}
	ex := s.Library.Exercise(att.ExerciseID)
	if ex == nil {
		return errNotFound("exercise not found")
	}

	limit := s.Limits.HintsPerDayAnon
	if id.Student != nil {
		limit = s.Limits.HintsPerDaySigned
	}
	n, err := s.Store.IncrUsage(r.Context(), s.day(), "hints", id.Owner().ID, 1)
	if err != nil {
		return err
	}
	if n > limit {
		s.metrics.rateLimited.WithLabelValues("hints_per_day").Inc()
		return errRateLimited(secondsToMidnight(s.Now()))
	}

	h, err := s.Tutor.Hint(r.Context(), tutor.Request{Attempt: att, Exercise: ex, Owner: id.Owner(), Lang: id.Lang(), Question: req.Question})
	if err != nil {
		return err
	}
	s.metrics.hints.WithLabelValues(h.Source, h.Lang).Inc()
	s.metrics.modelTokens.Add(float64(h.Tokens))
	writeJSON(w, http.StatusOK, hintResponse{HintID: h.ID, Level: h.Level, Source: h.Source, Lang: h.Lang, Text: h.Text, Line: h.Line})
	return nil
}
