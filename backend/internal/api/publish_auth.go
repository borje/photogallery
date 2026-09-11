package api

import (
	"net/http"
	"strings"

	"github.com/bege/smugbox/backend/internal/auth"
)

// requireAPIKey authenticates Lightroom plugin requests with
// "Authorization: Bearer <key>".
func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const scheme = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, scheme) {
			s.unauthorized(w)
			return
		}
		plain := strings.TrimSpace(header[len(scheme):])
		prefix, ok := auth.APIKeyPrefix(plain)
		if !ok {
			s.unauthorized(w)
			return
		}
		keys, err := s.db.ListAPIKeysByPrefix(r.Context(), prefix)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		for _, k := range keys {
			if k.RevokedAt == nil && auth.VerifyAPIKey(plain, k.Hash) {
				now := s.now()
				if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > touchInterval {
					if err := s.db.TouchAPIKey(r.Context(), k.ID, now); err != nil {
						s.log.Warn("touch api key", "err", err)
					}
				}
				next.ServeHTTP(w, r)
				return
			}
		}
		s.unauthorized(w)
	})
}

func (s *Server) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="smugbox"`)
	writeError(w, http.StatusUnauthorized, "unauthorized", "")
}
