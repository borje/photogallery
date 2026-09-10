package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// SessionCookieName is the visitor session cookie.
const SessionCookieName = "gallery_session"

// MaxGrants caps the number of unlocked albums remembered in one cookie so
// it stays well under browser size limits.
const MaxGrants = 20

// Grant records that the visitor unlocked an album at a given password
// version. Changing the password bumps the version and voids the grant.
type Grant struct {
	AlbumID string `json:"id"`
	Version int    `json:"v"`
}

// Session is the cookie payload.
type Session struct {
	Expires int64   `json:"exp"` // unix seconds
	Grants  []Grant `json:"a"`
}

// Granted reports whether the session unlocks the album at this version.
func (s *Session) Granted(albumID string, version int) bool {
	for _, g := range s.Grants {
		if g.AlbumID == albumID && g.Version == version {
			return true
		}
	}
	return false
}

// Add records a grant, replacing any earlier grant for the same album and
// dropping the oldest grants beyond MaxGrants.
func (s *Session) Add(albumID string, version int) {
	kept := make([]Grant, 0, len(s.Grants)+1)
	for _, g := range s.Grants {
		if g.AlbumID != albumID {
			kept = append(kept, g)
		}
	}
	kept = append(kept, Grant{AlbumID: albumID, Version: version})
	if len(kept) > MaxGrants {
		kept = kept[len(kept)-MaxGrants:]
	}
	s.Grants = kept
}

// Sessions signs and verifies session cookies.
type Sessions struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewSessions creates a signer. now is injectable for tests.
func NewSessions(secret []byte, ttl time.Duration, now func() time.Time) *Sessions {
	if now == nil {
		now = time.Now
	}
	return &Sessions{secret: secret, ttl: ttl, now: now}
}

// TTL is the session lifetime.
func (s *Sessions) TTL() time.Duration { return s.ttl }

// New returns an empty session expiring after the TTL.
func (s *Sessions) New() Session {
	return Session{Expires: s.now().Add(s.ttl).Unix()}
}

func (s *Sessions) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)
	return mac.Sum(nil)
}

// Encode serialises and signs the session: base64url(json) "." base64url(hmac).
func (s *Sessions) Encode(sess Session) (string, error) {
	payload, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(s.sign(payload)), nil
}

// Decode verifies the signature and expiry before parsing the payload.
// ok is false for anything that is not a currently valid session.
func (s *Sessions) Decode(value string) (Session, bool) {
	dot := strings.IndexByte(value, '.')
	if dot < 0 {
		return Session{}, false
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(value[:dot])
	if err != nil {
		return Session{}, false
	}
	sig, err := enc.DecodeString(value[dot+1:])
	if err != nil {
		return Session{}, false
	}
	if subtle.ConstantTimeCompare(sig, s.sign(payload)) != 1 {
		return Session{}, false
	}
	var sess Session
	if err := json.Unmarshal(payload, &sess); err != nil {
		return Session{}, false
	}
	if sess.Expires <= s.now().Unix() {
		return Session{}, false
	}
	return sess, true
}
