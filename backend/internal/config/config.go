// Package config reads server configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Config holds all runtime settings. See deploy/.env.example for the
// environment variables that populate it.
type Config struct {
	ListenAddr     string
	DataDir        string
	FrontendDir    string
	PublicBaseURL  string
	TrustedProxies []netip.Prefix
	MaxUploadBytes int64
	LogLevel       string
	LogFormat      string // "text" or "json"
}

// FromEnv builds a Config from the given environment lookup (normally os.Getenv).
func FromEnv(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	c := Config{
		ListenAddr:    get("LISTEN_ADDR", ":8080"),
		DataDir:       get("DATA_DIR", "./data"),
		FrontendDir:   get("FRONTEND_DIR", ""),
		PublicBaseURL: strings.TrimRight(get("PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		LogLevel:      strings.ToLower(get("LOG_LEVEL", "info")),
		LogFormat:     strings.ToLower(get("LOG_FORMAT", "text")),
	}

	mb, err := strconv.ParseInt(get("MAX_UPLOAD_MB", "100"), 10, 64)
	if err != nil || mb <= 0 {
		return c, errors.New("MAX_UPLOAD_MB must be a positive integer")
	}
	c.MaxUploadBytes = mb << 20

	for _, raw := range strings.Split(getenv("TRUSTED_PROXY_CIDR"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			addr, err2 := netip.ParseAddr(raw)
			if err2 != nil {
				return c, fmt.Errorf("TRUSTED_PROXY_CIDR: %q is not a CIDR or IP address", raw)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix)
	}

	if c.LogFormat != "text" && c.LogFormat != "json" {
		return c, errors.New("LOG_FORMAT must be \"text\" or \"json\"")
	}
	return c, nil
}
