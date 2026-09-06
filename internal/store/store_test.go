package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// runConformance exercises every Store method against s so both
// implementations are held to the same behaviour.
func runConformance(t *testing.T, s Store) {
	ctx := context.Background()
	dev := NewID()

	t.Run("device", func(t *testing.T) {
		d, err := s.TouchDevice(ctx, dev)
		if err != nil || d.ID != dev || d.LangPref != "hinglish" || d.StudentID != "" {
			t.Fatalf("TouchDevice = %+v, %v", d, err)
		}
		if err := s.SetDeviceLang(ctx, dev, "en"); err != nil {
			t.Fatal(err)
		}
		d, _ = s.TouchDevice(ctx, dev)
		if d.LangPref != "en" {
			t.Errorf("lang = %q", d.LangPref)
		}
		if err := s.SetDeviceLang(ctx, "nope", "en"); err != ErrNotFound {
			t.Errorf("SetDeviceLang unknown = %v", err)
		}
	})

	owner := Owner{OwnerDevice, dev}
	var attemptID string
	t.Run("attempts", func(t *testing.T) {
		a := &Attempt{DeviceID: dev, ExerciseID: "t/e1", Mode: "submit", IdempotencyKey: "k1", Code: "x",
			Outcome: "failed", Flags: []string{"no_print_call"}, Result: json.RawMessage(`{"ok":1}`), FailingTest: "c1", ErrorType: "NameError"}
		if err := s.CreateAttempt(ctx, a); err != nil {
			t.Fatal(err)
		}
		attemptID = a.ID
		got, err := s.GetAttempt(ctx, a.ID)
		var res map[string]int
		json.Unmarshal(got.Result, &res)
		if err != nil || got.FailingTest != "c1" || len(got.Flags) != 1 || res["ok"] != 1 || got.StudentID != "" {
			t.Fatalf("GetAttempt = %+v, %v", got, err)
		}
		if _, err := s.GetAttempt(ctx, "missing"); err != ErrNotFound {
			t.Errorf("GetAttempt missing = %v", err)
		}
		dup, err := s.AttemptByIdempotencyKey(ctx, dev, "k1")
		if err != nil || dup.ID != a.ID {
			t.Errorf("idempotency lookup = %+v, %v", dup, err)
		}
		if _, err := s.AttemptByIdempotencyKey(ctx, dev, "other"); err != ErrNotFound {
			t.Errorf("idempotency miss = %v", err)
		}
	})

	t.Run("progress", func(t *testing.T) {
		t0 := time.Now().Truncate(time.Microsecond)
		if err := s.RecordProgress(ctx, owner, "t/e1", "attempted", "a1", t0); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordProgress(ctx, owner, "t/e1", "passed", "a2", t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		// A later attempted must not regress a pass; a later pass keeps the first time.
		if err := s.RecordProgress(ctx, owner, "t/e1", "attempted", "a3", t0.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordProgress(ctx, owner, "t/e1", "passed", "a4", t0.Add(3*time.Minute)); err != nil {
			t.Fatal(err)
		}
		pr, err := s.ListProgress(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		p := pr["t/e1"]
		if p.Status != "passed" || p.PassedAt == nil || !p.PassedAt.Equal(t0.Add(time.Minute)) {
			t.Errorf("progress = %+v", p)
		}
	})

	t.Run("hints and cache", func(t *testing.T) {
		since := time.Now().Add(-time.Hour)
		h := &Hint{AttemptID: attemptID, Owner: owner, ExerciseID: "t/e1", Level: 1, Source: "canned", Lang: "hinglish", Text: "soch", Line: 3}
		if err := s.CreateHint(ctx, h); err != nil {
			t.Fatal(err)
		}
		hs, err := s.HintsSince(ctx, owner, "t/e1", since)
		if err != nil || len(hs) != 1 || hs[0].Text != "soch" || hs[0].Line != 3 {
			t.Errorf("HintsSince = %+v, %v", hs, err)
		}
		hs, _ = s.HintsSince(ctx, owner, "t/e1", time.Now().Add(time.Hour))
		if len(hs) != 0 {
			t.Errorf("future since should return none, got %d", len(hs))
		}
		// Unique per run: the Postgres variant reuses one database.
		key := "k-" + dev
		if _, err := s.GetCache(ctx, key); err != ErrNotFound {
			t.Errorf("GetCache miss = %v", err)
		}
		if err := s.PutCache(ctx, CacheEntry{Key: key, Text: "t", Lang: "en", Model: "m"}); err != nil {
			t.Fatal(err)
		}
		e, err := s.GetCache(ctx, key)
		if err != nil || e.Text != "t" || e.Model != "m" {
			t.Errorf("GetCache = %+v, %v", e, err)
		}
	})

	t.Run("usage", func(t *testing.T) {
		if n, _ := s.IncrUsage(ctx, "2026-09-06", "runs", dev, 1); n != 1 {
			t.Errorf("first incr = %d", n)
		}
		if n, _ := s.IncrUsage(ctx, "2026-09-06", "runs", dev, 2); n != 3 {
			t.Errorf("second incr = %d", n)
		}
		if n, _ := s.IncrUsage(ctx, "2026-09-07", "runs", dev, 1); n != 1 {
			t.Errorf("new day = %d", n)
		}
	})

	t.Run("students and linking", func(t *testing.T) {
		st, err := s.UpsertStudent(ctx, Student{GoogleSub: "sub-" + dev, Email: "a@x", DisplayName: "A"})
		if err != nil || st.ID == "" || st.LangPref != "hinglish" {
			t.Fatalf("UpsertStudent = %+v, %v", st, err)
		}
		again, _ := s.UpsertStudent(ctx, Student{GoogleSub: "sub-" + dev, Email: "b@x", DisplayName: "B"})
		if again.ID != st.ID || again.Email != "b@x" {
			t.Errorf("upsert did not update in place: %+v", again)
		}
		if err := s.SetStudentLang(ctx, st.ID, "en"); err != nil {
			t.Fatal(err)
		}
		got, _ := s.GetStudent(ctx, st.ID)
		if got.LangPref != "en" {
			t.Errorf("student lang = %q", got.LangPref)
		}
		// Pre-existing student progress: attempted on e2; device has passed e1 and attempted e2.
		sowner := Owner{OwnerStudent, st.ID}
		s.RecordProgress(ctx, sowner, "t/e2", "attempted", "s1", time.Now())
		s.RecordProgress(ctx, owner, "t/e2", "passed", "d2", time.Now())
		if err := s.LinkDevice(ctx, dev, st.ID); err != nil {
			t.Fatal(err)
		}
		d, _ := s.TouchDevice(ctx, dev)
		if d.StudentID != st.ID {
			t.Errorf("device not linked: %+v", d)
		}
		pr, _ := s.ListProgress(ctx, sowner)
		if pr["t/e1"].Status != "passed" || pr["t/e2"].Status != "passed" {
			t.Errorf("merged progress = %+v", pr)
		}
		if err := s.LinkDevice(ctx, "nope", st.ID); err != ErrNotFound {
			t.Errorf("LinkDevice unknown device = %v", err)
		}
	})
}

func TestMemoryConformance(t *testing.T) {
	runConformance(t, NewMemory())
}

func TestPostgresConformance(t *testing.T) {
	dsn := os.Getenv("TUTOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TUTOR_TEST_DATABASE_URL not set")
	}
	p, err := OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	runConformance(t, p)
}
