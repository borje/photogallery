package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSessionCookieRoundtrip(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	s := NewSessions([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour, clock)

	sess := s.New()
	sess.Add("album-1", 1)
	sess.Add("album-2", 3)
	value, err := s.Encode(sess)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(value, "=+/ ;") {
		t.Fatalf("cookie value not cookie-safe: %q", value)
	}
	got, ok := s.Decode(value)
	if !ok || !got.Granted("album-1", 1) || !got.Granted("album-2", 3) {
		t.Fatalf("roundtrip: ok=%v %+v", ok, got)
	}
	if got.Granted("album-1", 2) || got.Granted("album-3", 1) {
		t.Fatal("grant matched wrong version or album")
	}

	// Expired.
	now = now.Add(24*time.Hour + time.Second)
	if _, ok := s.Decode(value); ok {
		t.Fatal("expired session accepted")
	}
	now = now.Add(-24 * time.Hour)

	// Tampered payload keeps the signature but changes content.
	dot := strings.IndexByte(value, '.')
	tampered := strings.Replace(value[:dot], "a", "b", 1) + value[dot:]
	if tampered == value {
		tampered = value[:dot-1] + "A" + value[dot:]
	}
	if _, ok := s.Decode(tampered); ok {
		t.Fatal("tampered payload accepted")
	}
	if _, ok := s.Decode(value[:len(value)-2] + "xx"); ok {
		t.Fatal("tampered signature accepted")
	}
	for _, junk := range []string{"", ".", "abc", "abc.def", value[:dot]} {
		if _, ok := s.Decode(junk); ok {
			t.Fatalf("junk %q accepted", junk)
		}
	}
	other := NewSessions([]byte("another-secret-another-secret-12"), 24*time.Hour, clock)
	if _, ok := other.Decode(value); ok {
		t.Fatal("cookie signed with a different secret accepted")
	}
}

func TestSessionGrantsReplaceAndCap(t *testing.T) {
	var sess Session
	sess.Add("a", 1)
	sess.Add("a", 2)
	if len(sess.Grants) != 1 || !sess.Granted("a", 2) || sess.Granted("a", 1) {
		t.Fatalf("replace: %+v", sess.Grants)
	}
	for i := 0; i < MaxGrants+5; i++ {
		sess.Add(string(rune('A'+i)), 1)
	}
	if len(sess.Grants) != MaxGrants {
		t.Fatalf("cap: %d", len(sess.Grants))
	}
	if sess.Granted("a", 2) || sess.Granted("A", 1) {
		t.Fatal("oldest grants should have been dropped")
	}
	if !sess.Granted(string(rune('A'+MaxGrants+4)), 1) {
		t.Fatal("newest grant missing")
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	l := NewRateLimiter(5, 5, 100, func() time.Time { return now })
	for i := 0; i < 5; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("attempt %d denied", i+1)
		}
	}
	ok, wait := l.Allow("k")
	if ok {
		t.Fatal("6th attempt allowed")
	}
	if wait < 11*time.Second || wait > 13*time.Second {
		t.Fatalf("retry after %v, want ~12s", wait)
	}
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("independent key denied")
	}
	now = now.Add(12 * time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("token not refilled after 12s")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("second token available too early")
	}
	now = now.Add(time.Hour)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("bucket should be full again")
	}
	// A sweep after the interval drops idle buckets without breaking limits.
	now = now.Add(sweepInterval + time.Minute)
	l.Allow("fresh")
	if len(l.buckets) != 1 {
		t.Fatalf("buckets after sweep: %d", len(l.buckets))
	}
}
