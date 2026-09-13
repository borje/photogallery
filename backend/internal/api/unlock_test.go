package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/bege/smugbox/backend/internal/auth"
)

func (e *env) unlock(slug, password string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/albums/"+slug+"/unlock", strings.NewReader(`{"password":`+quote(password)+`}`))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func mustPrefix(s string) netip.Prefix { return netip.MustParsePrefix(s) }

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

func (e *env) getWithCookie(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			return c
		}
	}
	t.Fatalf("no session cookie in response (status %d)", rec.Code)
	return nil
}

func TestUnlockFlow(t *testing.T) {
	e := newEnv(t)
	locked := e.createLockedAlbum("Locked Album", "hemligt")
	jpg := testJPEG(t, 300, 200, 4)
	pid, _ := e.uploadPhoto(locked.ID, "lr-1", "s.jpg", jpg, nil)
	photoPath := "/api/albums/" + locked.Slug + "/photos/" + pid + "/thumb"
	zipPath := "/api/albums/" + locked.Slug + "/download"
	albumPath := "/api/albums/" + locked.Slug

	// Locked without a cookie.
	for _, p := range []string{albumPath, photoPath, zipPath} {
		if rec := e.get(p); rec.Code != 401 {
			t.Fatalf("%s without cookie: %d", p, rec.Code)
		}
	}

	rec := e.unlock(locked.Slug, "hemligt", nil)
	if rec.Code != 204 {
		t.Fatalf("unlock: %d %s", rec.Code, rec.Body.String())
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 86400 {
		t.Fatalf("cookie flags: %+v", c)
	}
	for _, p := range []string{albumPath, photoPath, zipPath} {
		if rec := e.getWithCookie(p, c); rec.Code != 200 {
			t.Fatalf("%s with cookie: %d %s", p, rec.Code, rec.Body.String())
		}
	}
	rec = e.getWithCookie(albumPath, c)
	var detail albumDetail
	decode(t, rec, &detail)
	if !detail.Locked || len(detail.Photos) != 1 {
		t.Fatalf("unlocked detail: %+v", detail)
	}
	if cc := e.getWithCookie(photoPath, c).Header().Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Fatalf("protected photo cache-control %q", cc)
	}
	// The cover stays blurred even for an unlocked visitor.
	rec = e.getWithCookie("/api/albums/"+locked.Slug+"/cover", c)
	if rec.Code != 200 || rec.Body.Len() >= 2048 {
		t.Fatalf("cover with cookie: %d, %d bytes", rec.Code, rec.Body.Len())
	}

	// A second album merges into the same cookie.
	other := e.createLockedAlbum("Other", "annat")
	rec = e.unlock(other.Slug, "annat", c)
	c2 := sessionCookie(t, rec)
	if e.getWithCookie(albumPath, c2).Code != 200 || e.getWithCookie("/api/albums/"+other.Slug, c2).Code != 200 {
		t.Fatal("merged cookie should unlock both albums")
	}
	if e.getWithCookie("/api/albums/"+other.Slug, c).Code != 401 {
		t.Fatal("old cookie must not unlock the second album")
	}

	// Password change invalidates the grant; re-saving the same password does not.
	if rec := e.json(http.MethodPut, "/api/publish/albums/"+locked.ID, map[string]any{"password": "hemligt", "description": "same pw"}); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if e.getWithCookie(albumPath, c2).Code != 200 {
		t.Fatal("unchanged password must keep sessions valid")
	}
	if rec := e.json(http.MethodPut, "/api/publish/albums/"+locked.ID, map[string]any{"password": "nytt"}); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if e.getWithCookie(albumPath, c2).Code != 401 {
		t.Fatal("password change must invalidate sessions")
	}
	if e.getWithCookie("/api/albums/"+other.Slug, c2).Code != 200 {
		t.Fatal("other album grant must survive")
	}

	// Expiry.
	rec = e.unlock(locked.Slug, "nytt", nil)
	c3 := sessionCookie(t, rec)
	e.now = e.now.Add(24*time.Hour + time.Minute)
	if e.getWithCookie(albumPath, c3).Code != 401 {
		t.Fatal("expired cookie accepted")
	}
	e.now = testStart

	// Tampered cookie.
	bad := *c3
	flipped := byte('A')
	if bad.Value[0] == 'A' {
		flipped = 'B'
	}
	bad.Value = string(flipped) + bad.Value[1:]
	if e.getWithCookie(albumPath, &bad).Code != 401 {
		t.Fatal("tampered cookie accepted")
	}

	// Public album: nothing to unlock, no cookie.
	pub := e.createAlbum("Public")
	rec = e.unlock(pub.Slug, "anything", nil)
	if rec.Code != 204 || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("public unlock: %d cookies=%d", rec.Code, len(rec.Result().Cookies()))
	}
	req := httptest.NewRequest(http.MethodPost, "/api/albums/"+locked.Slug+"/unlock", strings.NewReader("not json"))
	rr := httptest.NewRecorder()
	e.srv.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatalf("invalid json: %d", rr.Code)
	}
}

func TestUnlockDoesNotRevealAlbums(t *testing.T) {
	e := newEnv(t)
	locked := e.createLockedAlbum("Locked", "right")
	wrong := e.unlock(locked.Slug, "wrong", nil)
	missing := e.unlock("does-not-exist", "wrong", nil)
	invalid := e.unlock("Not_A_Slug", "wrong", nil)
	if wrong.Code != 401 || missing.Code != 401 || invalid.Code != 401 {
		t.Fatalf("codes: %d %d %d", wrong.Code, missing.Code, invalid.Code)
	}
	if wrong.Body.String() != missing.Body.String() || wrong.Body.String() != invalid.Body.String() {
		t.Fatalf("bodies differ: %q vs %q vs %q", wrong.Body.String(), missing.Body.String(), invalid.Body.String())
	}
	for _, rec := range []*httptest.ResponseRecorder{wrong, missing} {
		if len(rec.Result().Cookies()) != 0 {
			t.Fatal("cookie set on failure")
		}
	}
	if wrong.Header().Get("Content-Type") != missing.Header().Get("Content-Type") {
		t.Fatal("headers differ")
	}
}

func TestUnlockRateLimitAndTrustedProxy(t *testing.T) {
	e := newEnv(t)
	locked := e.createLockedAlbum("Limited", "pw")
	attempt := func(xff string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/albums/"+locked.Slug+"/unlock", strings.NewReader(`{"password":"nope"}`))
		req.Header.Set("Content-Type", "application/json")
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rec := httptest.NewRecorder()
		e.srv.ServeHTTP(rec, req)
		return rec
	}
	// Untrusted peer (httptest uses 192.0.2.1): X-Forwarded-For is ignored,
	// so different forwarded IPs share one bucket.
	for i := 0; i < unlockBurst; i++ {
		if rec := attempt("10.0.0." + string(rune('1'+i))); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
	rec := attempt("10.0.0.9")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over limit: %d retry-after %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	// The correct password is also refused while limited.
	if rec := e.unlock(locked.Slug, "pw", nil); rec.Code != 429 {
		t.Fatalf("correct password while limited: %d", rec.Code)
	}
	// Other slugs are unaffected.
	other := e.createLockedAlbum("Other", "pw")
	if rec := e.unlock(other.Slug, "pw", nil); rec.Code != 204 {
		t.Fatalf("other slug limited: %d", rec.Code)
	}
	// Tokens refill with the injected clock; no sleeping.
	e.now = e.now.Add(13 * time.Second)
	if rec := attempt(""); rec.Code != 401 {
		t.Fatalf("after refill: %d", rec.Code)
	}
	e.now = e.now.Add(time.Hour)

	// Trusted proxy: forwarded IPs get their own buckets.
	cfg := e.srv.cfg
	cfg.TrustedProxies = nil
	e.srv.cfg = cfg
	e.srv.cfg.TrustedProxies = append(e.srv.cfg.TrustedProxies, mustPrefix("192.0.2.0/24"))
	for i := 0; i < unlockBurst; i++ {
		attempt("203.0.113.5")
	}
	if rec := attempt("203.0.113.5"); rec.Code != 429 {
		t.Fatalf("forwarded ip should be limited: %d", rec.Code)
	}
	if rec := attempt("198.51.100.7, 203.0.113.6"); rec.Code != 401 {
		t.Fatalf("different forwarded ip should have its own bucket: %d", rec.Code)
	}
	if rec := attempt("203.0.113.6, 198.51.100.7"); rec.Code != 401 {
		t.Fatalf("only the last hop counts: %d", rec.Code)
	}
	if ip := e.srv.clientIP(httptest.NewRequest(http.MethodGet, "/", nil)); ip != "192.0.2.1" {
		t.Fatalf("clientIP without header: %q", ip)
	}
}

func TestUnlockLimitIsPerSlug(t *testing.T) {
	e := newEnv(t)
	post := func(slug string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/albums/"+slug+"/unlock", strings.NewReader(`{"password":"nope"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.srv.ServeHTTP(rec, req)
		return rec
	}
	// Each album has its own bucket, so traffic against one slug can never
	// rate-limit another: without that, one client behind the same proxy
	// address as every visitor would lock the whole site out.
	for i := 0; i < 50; i++ {
		if rec := post(fmt.Sprintf("guess-%d", i)); rec.Code != 401 {
			t.Fatalf("slug %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if n := e.srv.limiter.Len(); n != 50 {
		t.Fatalf("per-slug buckets = %d, want 50", n)
	}
	// One slug still runs out after its own burst.
	for i := 0; i < unlockBurst; i++ {
		post("guess-0")
	}
	if rec := post("guess-0"); rec.Code != 429 {
		t.Fatalf("repeat attempts on one slug: %d", rec.Code)
	}
	if rec := post("guess-1"); rec.Code != 401 {
		t.Fatalf("another slug must be unaffected: %d", rec.Code)
	}

	// Malformed slugs are refused before the limiter or bcrypt runs.
	e.now = e.now.Add(time.Hour)
	for _, slug := range []string{"..%2Fx", "UPPER", strings.Repeat("a", 200), "trailing-"} {
		if rec := post(slug); rec.Code != 401 {
			t.Fatalf("slug %q: %d", slug, rec.Code)
		}
	}
	if n := e.srv.limiter.Len(); n != 50 {
		t.Fatalf("malformed slugs created buckets: %d", n)
	}
}
