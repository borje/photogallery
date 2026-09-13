package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/bege/smugbox/backend/internal/auth"
	"github.com/bege/smugbox/backend/internal/db"
)

// Unlock attempts are limited per (IP, slug). There is deliberately no
// bucket per IP across all albums: behind a proxy without
// TRUSTED_PROXY_CIDR every visitor shares the proxy's address, so such a
// bucket is one bucket for the whole site and a single client emptying it
// locks every visitor out of every album. unlockMaxKeys is what bounds
// limiter memory against one client spraying slugs.
const (
	unlockBurst         = 5
	unlockRefillPerMin  = 5.0
	unlockMaxKeys       = 10000        // per limiter
	sessionTTL          = 24 * 60 * 60 // seconds
	maxUnlockPasswordLn = 256
)

// POST /api/albums/{slug}/unlock {"password": "..."}
//
// Wrong password and unknown album produce the same status, body and
// (bcrypt-dominated) timing, so the endpoint does not reveal which slugs
// exist. Success sets the session cookie and answers 204.
func (s *Server) unlockAlbum(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	w.Header().Set("Cache-Control", "no-store")

	// A malformed slug can never name an album; refuse it before it costs a
	// limiter key or a bcrypt round. Same status as an unknown album.
	if !validSlug(slug) {
		writeError(w, http.StatusUnauthorized, "invalid_password", "")
		return
	}
	ip := s.clientIP(r)
	if ok, wait := s.limiter.Allow(ip + "|" + slug); !ok {
		rateLimited(w, wait)
		return
	}

	var in struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Password) > maxUnlockPasswordLn {
		in.Password = in.Password[:maxUnlockPasswordLn]
	}

	album, err := s.db.GetAlbumBySlug(r.Context(), slug)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}
	if album != nil && !album.Protected() {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	hash := auth.DummyHash()
	if album != nil {
		hash = album.PasswordHash
	}
	if !auth.CheckPassword(hash, in.Password) || album == nil {
		writeError(w, http.StatusUnauthorized, "invalid_password", "")
		return
	}

	sess := s.sessions.New()
	if c, err := r.Cookie(auth.SessionCookieName); err == nil {
		if existing, ok := s.sessions.Decode(c.Value); ok {
			sess.Grants = existing.Grants
		}
	}
	sess.Add(album.ID, album.PasswordVersion)
	value, err := s.sessions.Encode(sess)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(s.sessions.TTL().Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func rateLimited(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
}
