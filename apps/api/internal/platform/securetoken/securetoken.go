// Package securetoken creates opaque bearer tokens (sessions, invites). Clients hold the
// token; the database stores only its SHA-256, so a database leak does not expose credentials.
package securetoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// MaxLen bounds tokens accepted from clients (a generated token has 43 characters).
const MaxLen = 128

// New returns a random 256-bit token (base64url) and its hash.
func New() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, Hash(token), nil
}

// Hash returns the SHA-256 of token, as stored in the database.
func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
