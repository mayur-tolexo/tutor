package httpapi

import (
	"sync"
	"time"
)

// bucketLimiter is a fixed-window counter per key, good enough for per-minute
// abuse limits on a single process. Windows are pruned lazily.
type bucketLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	start time.Time
	count int
}

// newBucketLimiter returns a limiter with the given window length.
func newBucketLimiter(window time.Duration) *bucketLimiter {
	return &bucketLimiter{window: window, buckets: map[string]*bucket{}, now: time.Now}
}

// Allow records one event for key and reports whether it is within limit. On
// refusal it also returns the seconds until the window resets.
func (l *bucketLimiter) Allow(key string, limit int) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok || now.Sub(b.start) >= l.window {
		b = &bucket{start: now}
		l.buckets[key] = b
		// Prune occasionally so abandoned keys do not accumulate forever.
		if len(l.buckets) > 10000 {
			l.prune(now)
		}
	}
	if b.count >= limit {
		retry := int((l.window - now.Sub(b.start)).Seconds()) + 1
		return false, retry
	}
	b.count++
	return true, 0
}

// prune drops every bucket whose window has passed.
func (l *bucketLimiter) prune(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.start) >= l.window {
			delete(l.buckets, k)
		}
	}
}
