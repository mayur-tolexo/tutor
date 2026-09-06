package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Postgres is the production Store.
type Postgres struct {
	pool *pgxpool.Pool
}

// OpenPostgres connects, applies pending migrations, and returns the store.
func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	p := &Postgres{pool: pool}
	if err := p.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return p, nil
}

// Close releases the connection pool.
func (p *Postgres) Close() { p.pool.Close() }

// migrate applies each embedded migration file once, in name order, recording
// applied names in schema_migrations. Files run inside a transaction so a
// failure leaves the schema at the previous version.
func (p *Postgres) migrate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var applied bool
		if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) TouchDevice(ctx context.Context, id string) (Device, error) {
	var d Device
	var studentID *string
	err := p.pool.QueryRow(ctx, `
		INSERT INTO devices (id) VALUES ($1)
		ON CONFLICT (id) DO UPDATE SET last_seen_at = now()
		RETURNING id, student_id, lang_pref, created_at, last_seen_at`, id).
		Scan(&d.ID, &studentID, &d.LangPref, &d.CreatedAt, &d.LastSeenAt)
	if studentID != nil {
		d.StudentID = *studentID
	}
	return d, err
}

func (p *Postgres) SetDeviceLang(ctx context.Context, id, lang string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE devices SET lang_pref = $2 WHERE id = $1`, id, lang)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) UpsertStudent(ctx context.Context, s Student) (Student, error) {
	if s.LangPref == "" {
		s.LangPref = "hinglish"
	}
	var out Student
	// Profile fields refresh on every sign-in; lang_pref is the student's own
	// setting and must survive re-login.
	err := p.pool.QueryRow(ctx, `
		INSERT INTO students (id, google_sub, email, display_name, lang_pref)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (google_sub) DO UPDATE SET email = EXCLUDED.email, display_name = EXCLUDED.display_name
		RETURNING id, google_sub, email, display_name, lang_pref, created_at`,
		NewID(), s.GoogleSub, s.Email, s.DisplayName, s.LangPref).
		Scan(&out.ID, &out.GoogleSub, &out.Email, &out.DisplayName, &out.LangPref, &out.CreatedAt)
	return out, err
}

func (p *Postgres) GetStudent(ctx context.Context, id string) (Student, error) {
	var s Student
	err := p.pool.QueryRow(ctx, `SELECT id, google_sub, email, display_name, lang_pref, created_at FROM students WHERE id = $1`, id).
		Scan(&s.ID, &s.GoogleSub, &s.Email, &s.DisplayName, &s.LangPref, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Student{}, ErrNotFound
	}
	return s, err
}

func (p *Postgres) SetStudentLang(ctx context.Context, id, lang string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE students SET lang_pref = $2 WHERE id = $1`, id, lang)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) LinkDevice(ctx context.Context, deviceID, studentID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE devices SET student_id = $2 WHERE id = $1`, deviceID, studentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	// Merge device progress into the student's: a pass wins over attempted, and
	// the earlier pass time is kept.
	_, err = tx.Exec(ctx, `
		INSERT INTO progress (owner_kind, owner_id, exercise_id, status, best_attempt_id, passed_at)
		SELECT 'student', $2, exercise_id, status, best_attempt_id, passed_at
		FROM progress WHERE owner_kind = 'device' AND owner_id = $1
		ON CONFLICT (owner_kind, owner_id, exercise_id) DO UPDATE SET
			status = CASE WHEN progress.status = 'passed' THEN 'passed' ELSE EXCLUDED.status END,
			best_attempt_id = CASE WHEN progress.status = 'passed' AND EXCLUDED.status <> 'passed' THEN progress.best_attempt_id ELSE EXCLUDED.best_attempt_id END,
			passed_at = LEAST(progress.passed_at, EXCLUDED.passed_at)`, deviceID, studentID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) CreateAttempt(ctx context.Context, a *Attempt) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.Flags == nil {
		a.Flags = []string{}
	}
	if len(a.Result) == 0 {
		a.Result = []byte("{}")
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO attempts (id, device_id, student_id, exercise_id, mode, idempotency_key, code, outcome, passed,
			error_type, error_message, error_line, failing_test, flags, result, duration_ms, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		a.ID, a.DeviceID, nullable(a.StudentID), a.ExerciseID, a.Mode, a.IdempotencyKey, a.Code, a.Outcome, a.Passed,
		a.ErrorType, a.ErrorMessage, a.ErrorLine, a.FailingTest, a.Flags, a.Result, a.DurationMS, a.CreatedAt)
	return err
}

const attemptColumns = `id, device_id, COALESCE(student_id, ''), exercise_id, mode, idempotency_key, code, outcome, passed,
	error_type, error_message, error_line, failing_test, flags, result, duration_ms, created_at`

// scanAttempt reads one attempt row in attemptColumns order.
func scanAttempt(row pgx.Row) (Attempt, error) {
	var a Attempt
	err := row.Scan(&a.ID, &a.DeviceID, &a.StudentID, &a.ExerciseID, &a.Mode, &a.IdempotencyKey, &a.Code, &a.Outcome, &a.Passed,
		&a.ErrorType, &a.ErrorMessage, &a.ErrorLine, &a.FailingTest, &a.Flags, &a.Result, &a.DurationMS, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attempt{}, ErrNotFound
	}
	return a, err
}

func (p *Postgres) GetAttempt(ctx context.Context, id string) (Attempt, error) {
	return scanAttempt(p.pool.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE id = $1`, id))
}

func (p *Postgres) AttemptByIdempotencyKey(ctx context.Context, deviceID, key string) (Attempt, error) {
	if key == "" {
		return Attempt{}, ErrNotFound
	}
	return scanAttempt(p.pool.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE device_id = $1 AND idempotency_key = $2`, deviceID, key))
}

func (p *Postgres) RecordProgress(ctx context.Context, owner Owner, exerciseID, status, attemptID string, at time.Time) error {
	var passedAt *time.Time
	if status == "passed" {
		passedAt = &at
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO progress (owner_kind, owner_id, exercise_id, status, best_attempt_id, passed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (owner_kind, owner_id, exercise_id) DO UPDATE SET
			status = CASE WHEN progress.status = 'passed' THEN 'passed' ELSE EXCLUDED.status END,
			best_attempt_id = CASE WHEN progress.status = 'passed' AND EXCLUDED.status <> 'passed' THEN progress.best_attempt_id ELSE EXCLUDED.best_attempt_id END,
			passed_at = LEAST(progress.passed_at, EXCLUDED.passed_at)`,
		string(owner.Kind), owner.ID, exerciseID, status, attemptID, passedAt)
	return err
}

func (p *Postgres) ListProgress(ctx context.Context, owner Owner) (map[string]Progress, error) {
	rows, err := p.pool.Query(ctx, `SELECT exercise_id, status, best_attempt_id, passed_at FROM progress WHERE owner_kind = $1 AND owner_id = $2`, string(owner.Kind), owner.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Progress{}
	for rows.Next() {
		var pr Progress
		if err := rows.Scan(&pr.ExerciseID, &pr.Status, &pr.BestAttemptID, &pr.PassedAt); err != nil {
			return nil, err
		}
		out[pr.ExerciseID] = pr
	}
	return out, rows.Err()
}

func (p *Postgres) CreateHint(ctx context.Context, h *Hint) error {
	if h.ID == "" {
		h.ID = NewID()
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now()
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO hints (id, attempt_id, owner_kind, owner_id, exercise_id, level, source, lang, text, line, model, tokens, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		h.ID, h.AttemptID, string(h.Owner.Kind), h.Owner.ID, h.ExerciseID, h.Level, h.Source, h.Lang, h.Text, h.Line, h.Model, h.Tokens, h.CreatedAt)
	return err
}

func (p *Postgres) HintsSince(ctx context.Context, owner Owner, exerciseID string, since time.Time) ([]Hint, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, attempt_id, level, source, lang, text, line, model, tokens, created_at
		FROM hints WHERE owner_kind = $1 AND owner_id = $2 AND exercise_id = $3 AND created_at > $4
		ORDER BY created_at DESC`, string(owner.Kind), owner.ID, exerciseID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hint
	for rows.Next() {
		h := Hint{Owner: owner, ExerciseID: exerciseID}
		if err := rows.Scan(&h.ID, &h.AttemptID, &h.Level, &h.Source, &h.Lang, &h.Text, &h.Line, &h.Model, &h.Tokens, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (p *Postgres) GetCache(ctx context.Context, key string) (CacheEntry, error) {
	var e CacheEntry
	err := p.pool.QueryRow(ctx, `SELECT key, text, lang, model, created_at FROM hint_cache WHERE key = $1`, key).
		Scan(&e.Key, &e.Text, &e.Lang, &e.Model, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CacheEntry{}, ErrNotFound
	}
	return e, err
}

func (p *Postgres) PutCache(ctx context.Context, e CacheEntry) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO hint_cache (key, text, lang, model) VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) DO UPDATE SET text = EXCLUDED.text, model = EXCLUDED.model, created_at = now()`,
		e.Key, e.Text, e.Lang, e.Model)
	return err
}

func (p *Postgres) IncrUsage(ctx context.Context, day, kind, owner string, n int) (int, error) {
	var total int
	err := p.pool.QueryRow(ctx, `
		INSERT INTO usage_daily (day, kind, owner, count) VALUES ($1, $2, $3, $4)
		ON CONFLICT (day, kind, owner) DO UPDATE SET count = usage_daily.count + EXCLUDED.count
		RETURNING count`, day, kind, owner, n).Scan(&total)
	return total, err
}

// nullable maps an empty string to SQL NULL for optional foreign keys.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
