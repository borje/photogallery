package auth

import (
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost is the bcrypt work factor for album passwords. Tests may lower it.
var BcryptCost = 12

// HashPassword hashes an album password.
func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
}

// CheckPassword reports whether password matches hash.
func CheckPassword(hash []byte, password string) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

var (
	dummyOnce sync.Once
	dummyHash []byte
)

// DummyHash returns a fixed bcrypt hash used to spend the same time on
// unlock attempts against albums that do not exist, so response timing
// does not reveal whether a slug is valid.
func DummyHash() []byte {
	dummyOnce.Do(func() {
		h, err := bcrypt.GenerateFromPassword([]byte("smugbox-dummy-password-never-matches"), BcryptCost)
		if err != nil {
			panic(err)
		}
		dummyHash = h
	})
	return dummyHash
}
