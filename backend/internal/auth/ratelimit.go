package auth

import (
	"math"
	"sync"
	"time"
)

// RateLimiter is an in-memory token bucket per key (for example "ip|slug").
// The number of tracked keys is capped so attacker-chosen keys cannot grow
// the map without bound.
type RateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	capacity  float64
	perSecond float64
	maxKeys   int
	now       func() time.Time
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

const sweepInterval = 10 * time.Minute

// NewRateLimiter allows capacity attempts at once, refilling refillPerMinute
// tokens per minute, and tracks at most maxKeys distinct keys. now is
// injectable for tests.
func NewRateLimiter(capacity int, refillPerMinute float64, maxKeys int, now func() time.Time) *RateLimiter {
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{
		buckets:   map[string]*bucket{},
		capacity:  float64(capacity),
		perSecond: refillPerMinute / 60,
		maxKeys:   maxKeys,
		now:       now,
		lastSweep: now(),
	}
}

// Allow consumes one token for key. When denied it returns how long until
// the next token is available. A new key is denied outright while the map
// is full and nothing can be swept.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now, false) // before touching the current key so it is never swept mid-use
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			l.sweep(now, true)
		}
		if len(l.buckets) >= l.maxKeys {
			return false, time.Duration(1 / l.perSecond * float64(time.Second))
		}
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

// Len reports how many keys are currently tracked.
func (l *RateLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// sweep forgets buckets that have refilled completely. It runs at most every
// sweepInterval unless forced.
func (l *RateLimiter) sweep(now time.Time, force bool) {
	if !force && now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if math.Min(l.capacity, b.tokens+now.Sub(b.last).Seconds()*l.perSecond) >= l.capacity {
			delete(l.buckets, k)
		}
	}
}
