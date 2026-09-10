package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIP returns the visitor's IP. X-Forwarded-For is honoured only when
// the direct peer is a configured trusted proxy, and then only its last
// entry (the one the proxy appended).
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.WithZone("").Unmap()
	if s.trustedPeer(peer) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if fwd, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				return fwd.Unmap().String()
			}
		}
	}
	return peer.String()
}

func (s *Server) trustedPeer(addr netip.Addr) bool {
	for _, p := range s.cfg.TrustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
