package auth

import (
	"math"
	"sync"
	"time"
)

// RateLimiter is an in-memory token bucket per key (for example "ip|slug").
type RateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	capacity  float64
	perSecond float64
	now       func() time.Time
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

const sweepInterval = 10 * time.Minute

// NewRateLimiter allows capacity attempts at once, refilling refillPerMinute
// tokens per minute. now is injectable for tests.
func NewRateLimiter(capacity int, refillPerMinute float64, now func() time.Time) *RateLimiter {
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{
		buckets:   map[string]*bucket{},
		capacity:  float64(capacity),
		perSecond: refillPerMinute / 60,
		now:       now,
		lastSweep: now(),
	}
}

// Allow consumes one token for key. When denied it returns how long until
// the next token is available.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now) // before touching the current key so it is never swept mid-use
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.capacity, last: now}
		l.buckets[key] = b
	} else {
		b.tokens = math.Min(l.capacity, b.tokens+now.Sub(b.last).Seconds()*l.perSecond)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.perSecond * float64(time.Second))
	return false, wait
}

// sweep forgets buckets that have refilled completely.
func (l *RateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if math.Min(l.capacity, b.tokens+now.Sub(b.last).Seconds()*l.perSecond) >= l.capacity {
			delete(l.buckets, k)
		}
	}
}
