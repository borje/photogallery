package config

import (
	"net/netip"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := FromEnv(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":8080" || c.DataDir != "./data" || c.MaxUploadBytes != 100<<20 || c.LogLevel != "info" || c.LogFormat != "text" || c.PublicBaseURL != "http://localhost:8080" || len(c.TrustedProxies) != 0 {
		t.Fatalf("defaults: %+v", c)
	}
	if err := c.ValidateForServe(); err == nil {
		t.Fatal("missing SESSION_SECRET must fail serve validation")
	}
}

func TestParsing(t *testing.T) {
	c, err := FromEnv(envOf(map[string]string{
		"PUBLIC_BASE_URL":    "https://photos.example/",
		"MAX_UPLOAD_MB":      "5",
		"TRUSTED_PROXY_CIDR": "10.0.0.0/8, 172.18.0.5",
		"SESSION_SECRET":     "0123456789abcdef0123456789abcdef",
		"LOG_FORMAT":         "JSON",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicBaseURL != "https://photos.example" || c.MaxUploadBytes != 5<<20 || c.LogFormat != "json" {
		t.Fatalf("parsed: %+v", c)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[0] != netip.MustParsePrefix("10.0.0.0/8") || c.TrustedProxies[1] != netip.MustParsePrefix("172.18.0.5/32") {
		t.Fatalf("proxies: %v", c.TrustedProxies)
	}
	if err := c.ValidateForServe(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]string{
		{"MAX_UPLOAD_MB": "0"},
		{"MAX_UPLOAD_MB": "lots"},
		{"TRUSTED_PROXY_CIDR": "not-an-ip"},
		{"LOG_FORMAT": "xml"},
	} {
		if _, err := FromEnv(envOf(bad)); err == nil {
			t.Errorf("%v should fail", bad)
		}
	}
}
