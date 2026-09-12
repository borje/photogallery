package auth

import (
	"testing"
	"time"
)

func TestRateLimiterCapsKeys(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	l := NewRateLimiter(5, 60, 3, func() time.Time { return now })
	for _, k := range []string{"a", "b", "c"} {
		if ok, _ := l.Allow(k); !ok {
			t.Fatalf("key %s denied", k)
		}
	}
	// Nothing has refilled, so the forced sweep frees nothing and a fresh
	// key is denied without growing the map.
	if ok, wait := l.Allow("d"); ok || wait <= 0 {
		t.Fatalf("fourth key: ok=%v wait=%v", ok, wait)
	}
	if l.Len() != 3 {
		t.Fatalf("len = %d, want 3", l.Len())
	}
	// Known keys keep working while the map is full.
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("existing key denied while full")
	}
	// Once the old buckets have refilled the forced sweep makes room.
	now = now.Add(2 * time.Second)
	if ok, _ := l.Allow("d"); !ok {
		t.Fatal("fresh key should be admitted after sweep")
	}
	if l.Len() != 1 {
		t.Fatalf("len after sweep = %d, want 1", l.Len())
	}
}

func TestRateLimiterPeriodicSweep(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	l := NewRateLimiter(2, 60, 100, func() time.Time { return now })
	l.Allow("a")
	l.Allow("b")
	now = now.Add(sweepInterval - time.Second)
	l.Allow("c")
	if l.Len() != 3 {
		t.Fatalf("swept too early: len = %d", l.Len())
	}
	now = now.Add(2 * time.Second)
	l.Allow("c") // c has not refilled fully (used twice), a and b have
	if l.Len() != 1 {
		t.Fatalf("len after periodic sweep = %d, want 1", l.Len())
	}
}
