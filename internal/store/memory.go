package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// Memory is an in-process Store for development and tests. Data is lost on
// restart.
type Memory struct {
	mu       sync.Mutex
	devices  map[string]Device
	students map[string]Student
	bySub    map[string]string // google_sub -> student id
	attempts map[string]Attempt
	progress map[Owner]map[string]Progress
	hints    []Hint
	cache    map[string]CacheEntry
	usage    map[string]int
	now      func() time.Time
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		devices:  map[string]Device{},
		students: map[string]Student{},
		bySub:    map[string]string{},
		attempts: map[string]Attempt{},
		progress: map[Owner]map[string]Progress{},
		cache:    map[string]CacheEntry{},
		usage:    map[string]int{},
		now:      time.Now,
	}
}

// NewID returns a random 128-bit hex identifier.
func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (m *Memory) TouchDevice(_ context.Context, id string) (Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		d = Device{ID: id, LangPref: "hinglish", CreatedAt: m.now()}
	}
	d.LastSeenAt = m.now()
	m.devices[id] = d
	return d, nil
}

func (m *Memory) SetDeviceLang(_ context.Context, id, lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.LangPref = lang
	m.devices[id] = d
	return nil
}

func (m *Memory) UpsertStudent(_ context.Context, s Student) (Student, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.bySub[s.GoogleSub]; ok {
		cur := m.students[id]
		cur.Email, cur.DisplayName = s.Email, s.DisplayName
		m.students[id] = cur
		return cur, nil
	}
	s.ID = NewID()
	s.CreatedAt = m.now()
	if s.LangPref == "" {
		s.LangPref = "hinglish"
	}
	m.students[s.ID] = s
	m.bySub[s.GoogleSub] = s.ID
	return s, nil
}

func (m *Memory) GetStudent(_ context.Context, id string) (Student, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.students[id]
	if !ok {
		return Student{}, ErrNotFound
	}
	return s, nil
}

func (m *Memory) SetStudentLang(_ context.Context, id, lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.students[id]
	if !ok {
		return ErrNotFound
	}
	s.LangPref = lang
	m.students[id] = s
	return nil
}

func (m *Memory) LinkDevice(_ context.Context, deviceID, studentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[deviceID]
	if !ok {
		return ErrNotFound
	}
	if _, ok := m.students[studentID]; !ok {
		return ErrNotFound
	}
	d.StudentID = studentID
	m.devices[deviceID] = d
	from := Owner{OwnerDevice, deviceID}
	to := Owner{OwnerStudent, studentID}
	for ex, p := range m.progress[from] {
		m.mergeProgress(to, ex, p)
	}
	return nil
}

// mergeProgress applies p to owner's record for ex. A pass is never regressed
// to attempted, and the earliest pass time is kept.
func (m *Memory) mergeProgress(owner Owner, ex string, p Progress) {
	if m.progress[owner] == nil {
		m.progress[owner] = map[string]Progress{}
	}
	if cur, ok := m.progress[owner][ex]; ok && cur.Status == "passed" {
		if p.Status != "passed" {
			return
		}
		if cur.PassedAt != nil && p.PassedAt != nil && !p.PassedAt.Before(*cur.PassedAt) {
			return
		}
	}
	m.progress[owner][ex] = p
}

func (m *Memory) CreateAttempt(_ context.Context, a *Attempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = m.now()
	}
	m.attempts[a.ID] = *a
	return nil
}

func (m *Memory) GetAttempt(_ context.Context, id string) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.attempts[id]
	if !ok {
		return Attempt{}, ErrNotFound
	}
	return a, nil
}

func (m *Memory) AttemptByIdempotencyKey(_ context.Context, deviceID, key string) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.attempts {
		if a.DeviceID == deviceID && a.IdempotencyKey == key {
			return a, nil
		}
	}
	return Attempt{}, ErrNotFound
}

func (m *Memory) RecordProgress(_ context.Context, owner Owner, exerciseID, status, attemptID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := Progress{ExerciseID: exerciseID, Status: status, BestAttemptID: attemptID}
	if status == "passed" {
		t := at
		p.PassedAt = &t
	}
	m.mergeProgress(owner, exerciseID, p)
	return nil
}

func (m *Memory) ListProgress(_ context.Context, owner Owner) (map[string]Progress, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]Progress{}
	for k, v := range m.progress[owner] {
		out[k] = v
	}
	return out, nil
}

func (m *Memory) CreateHint(_ context.Context, h *Hint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h.ID == "" {
		h.ID = NewID()
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = m.now()
	}
	m.hints = append(m.hints, *h)
	return nil
}

func (m *Memory) HintsSince(_ context.Context, owner Owner, exerciseID string, since time.Time) ([]Hint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Hint
	for _, h := range m.hints {
		if h.Owner == owner && h.ExerciseID == exerciseID && h.CreatedAt.After(since) {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) GetCache(_ context.Context, key string) (CacheEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.cache[key]
	if !ok {
		return CacheEntry{}, ErrNotFound
	}
	return e, nil
}

func (m *Memory) PutCache(_ context.Context, e CacheEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = m.now()
	}
	m.cache[e.Key] = e
	return nil
}

func (m *Memory) IncrUsage(_ context.Context, day, kind, owner string, n int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := day + "|" + kind + "|" + owner
	m.usage[k] += n
	return m.usage[k], nil
}
