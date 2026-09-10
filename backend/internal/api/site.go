package api

import "net/http"

// siteConfig exposes server-wide settings the frontend needs, such as the
// configurable site title.
func (s *Server) siteConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"title": s.cfg.SiteTitle})
}
