// Package store persists students, devices, attempts, progress, hints, the
// hint cache, and daily usage counters. Two implementations exist: Postgres for
// production and an in-memory one for development and tests.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("store: not found")

// OwnerKind distinguishes anonymous device-scoped data from account data.
type OwnerKind string

const (
	OwnerDevice  OwnerKind = "device"
	OwnerStudent OwnerKind = "student"
)

// Owner identifies who progress and hints belong to: a signed-in student, or
// an anonymous device until it is linked to one.
type Owner struct {
	Kind OwnerKind
	ID   string
}

// Student is a Google-linked account.
type Student struct {
	ID          string
	GoogleSub   string
	Email       string
	DisplayName string
	LangPref    string
	CreatedAt   time.Time
}

// Device is a browser install identified by a client-generated uuid.
type Device struct {
	ID         string
	StudentID  string // empty until linked
	LangPref   string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Attempt is one run or submit of a student's code and what happened.
type Attempt struct {
	ID             string
	DeviceID       string
	StudentID      string
	ExerciseID     string
	Mode           string
	IdempotencyKey string
	Code           string
	Outcome        string
	Passed         bool
	ErrorType      string
	ErrorMessage   string
	ErrorLine      int
	FailingTest    string
	Flags          []string
	Result         json.RawMessage
	DurationMS     int
	CreatedAt      time.Time
}

// Progress is a student's standing on one exercise.
type Progress struct {
	ExerciseID    string
	Status        string // attempted | passed
	BestAttemptID string
	PassedAt      *time.Time
}

// Hint is one nudge delivered to a student.
type Hint struct {
	ID         string
	AttemptID  string
	Owner      Owner
	ExerciseID string
	Level      int
	Source     string
	Lang       string
	Text       string
	Line       int
	Model      string
	Tokens     int
	CreatedAt  time.Time
}

// CacheEntry is a model-generated hint reusable for the same mistake.
type CacheEntry struct {
	Key       string
	Text      string
	Lang      string
	Model     string
	CreatedAt time.Time
}

// Store is the persistence contract. Implementations are safe for concurrent use.
type Store interface {
	// TouchDevice upserts the device and bumps last_seen, returning its record.
	TouchDevice(ctx context.Context, id string) (Device, error)
	SetDeviceLang(ctx context.Context, id, lang string) error

	UpsertStudent(ctx context.Context, s Student) (Student, error)
	GetStudent(ctx context.Context, id string) (Student, error)
	SetStudentLang(ctx context.Context, id, lang string) error
	// LinkDevice attaches a device to a student and merges the device's
	// progress into the student's, keeping the better status per exercise.
	LinkDevice(ctx context.Context, deviceID, studentID string) error

	CreateAttempt(ctx context.Context, a *Attempt) error
	GetAttempt(ctx context.Context, id string) (Attempt, error)
	// AttemptByIdempotencyKey returns the attempt a device already made with
	// key, so a retried request gets the original result.
	AttemptByIdempotencyKey(ctx context.Context, deviceID, key string) (Attempt, error)

	// RecordProgress updates the owner's standing; status never regresses from
	// passed to attempted.
	RecordProgress(ctx context.Context, owner Owner, exerciseID, status, attemptID string, at time.Time) error
	ListProgress(ctx context.Context, owner Owner) (map[string]Progress, error)

	CreateHint(ctx context.Context, h *Hint) error
	// HintsSince returns the owner's hints on an exercise created after since,
	// newest first; used to compute the hint ladder level.
	HintsSince(ctx context.Context, owner Owner, exerciseID string, since time.Time) ([]Hint, error)

	GetCache(ctx context.Context, key string) (CacheEntry, error)
	PutCache(ctx context.Context, e CacheEntry) error

	// IncrUsage adds n to the (day, kind, owner) counter and returns the new
	// total; it backs daily rate limits and the model budget.
	IncrUsage(ctx context.Context, day, kind, owner string, n int) (int, error)
}
