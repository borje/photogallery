// Package auth implements API keys, album passwords and the visitor
// session cookie.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
)

// APIKeyPrefixLen is the number of leading key characters stored in clear
// text for lookup.
const APIKeyPrefixLen = 8

const apiKeyBytes = 32

// GenerateAPIKey returns a new random key: 32 bytes, base64url without padding.
func GenerateAPIKey(rand io.Reader) (string, error) {
	b := make([]byte, apiKeyBytes)
	if _, err := io.ReadFull(rand, b); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// APIKeyPrefix returns the lookup prefix of a presented key. ok is false
// when the key is too short to be valid.
func APIKeyPrefix(plain string) (prefix string, ok bool) {
	if len(plain) < 2*APIKeyPrefixLen {
		return "", false
	}
	return plain[:APIKeyPrefixLen], true
}

// HashAPIKey returns sha256(plain). Keys are high-entropy random strings,
// so a fast hash is appropriate (bcrypt would add nothing).
func HashAPIKey(plain string) []byte {
	h := sha256.Sum256([]byte(plain))
	return h[:]
}

// VerifyAPIKey compares plain against a stored hash in constant time.
func VerifyAPIKey(plain string, hash []byte) bool {
	return subtle.ConstantTimeCompare(HashAPIKey(plain), hash) == 1
}
