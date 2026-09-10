package auth

import (
	"bytes"
	"crypto/rand"
	"regexp"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestAPIKeyLifecycle(t *testing.T) {
	plain, err := GenerateAPIKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 43 || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(plain) {
		t.Fatalf("unexpected key format %q", plain)
	}
	prefix, ok := APIKeyPrefix(plain)
	if !ok || prefix != plain[:8] {
		t.Fatalf("prefix %q ok=%v", prefix, ok)
	}
	if _, ok := APIKeyPrefix("short"); ok {
		t.Fatal("short key accepted")
	}
	hash := HashAPIKey(plain)
	if len(hash) != 32 {
		t.Fatalf("hash len %d", len(hash))
	}
	if !VerifyAPIKey(plain, hash) {
		t.Fatal("valid key rejected")
	}
	if VerifyAPIKey(plain[:42]+"x", hash) || VerifyAPIKey("", hash) {
		t.Fatal("wrong key accepted")
	}
	other, _ := GenerateAPIKey(rand.Reader)
	if bytes.Equal(HashAPIKey(other), hash) {
		t.Fatal("two keys hashed alike")
	}
}

func TestPasswords(t *testing.T) {
	BcryptCost = bcrypt.MinCost
	h, err := HashPassword("hemligt")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "hemligt") || CheckPassword(h, "Hemligt") || CheckPassword(nil, "hemligt") {
		t.Fatal("password check wrong")
	}
	d1, d2 := DummyHash(), DummyHash()
	if !bytes.Equal(d1, d2) || CheckPassword(d1, "") || CheckPassword(d1, "gallery-dummy-password-never-matches") == false {
		// The dummy hash is only for timing; it must be stable across calls.
		if !bytes.Equal(d1, d2) {
			t.Fatal("dummy hash not stable")
		}
	}
}
