package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/bege/smugbox/backend/internal/auth"
	"github.com/bege/smugbox/backend/internal/db"
)

// Unlock attempts allowed per (client IP, slug): a burst of 5, refilling 5/min.
const (
	unlockBurst         = 5
	unlockRefillPerMin  = 5.0
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

	if ok, wait := s.limiter.Allow(s.clientIP(r) + "|" + slug); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
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

	var album *db.Album
	if validSlug(slug) {
		a, err := s.db.GetAlbumBySlug(r.Context(), slug)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			s.internalError(w, r, err)
			return
		}
		album = a
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
