package api

import (
	"net/http"
	"regexp"

	"github.com/bege/photogallery/backend/internal/auth"
	"github.com/bege/photogallery/backend/internal/db"
)

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validSlug reports whether s could have been produced by slugify.
func validSlug(s string) bool {
	return len(s) <= maxSlugLen+5 && slugRe.MatchString(s)
}

// albumAccess reports whether the request may see the album's content.
// Public albums are always visible; protected albums require a session
// cookie granting this album at its current password version.
func (s *Server) albumAccess(r *http.Request, a *db.Album) bool {
	if !a.Protected() {
		return true
	}
	c, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		return false
	}
	sess, ok := s.sessions.Decode(c.Value)
	if !ok {
		return false
	}
	return sess.Granted(a.ID, a.PasswordVersion)
}

// passwordRequired answers 401 with just enough information for the
// visitor to see what they are unlocking.
func passwordRequired(w http.ResponseWriter, sum *db.AlbumSummary, crumbs []crumb) {
	body := map[string]any{"error": "password_required"}
	if sum != nil {
		body["slug"] = sum.Slug
		body["name"] = sum.Name
		body["photo_count"] = sum.PhotoCount
	}
	if len(crumbs) > 0 {
		body["breadcrumb"] = crumbs
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusUnauthorized, body)
}
