package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are the Argon2id cost parameters. They are embedded in every hash (PHC format),
// so they can be raised later: old hashes keep verifying and are upgraded on the next login.
type Argon2Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
	SaltLen   uint32
	KeyLen    uint32
}

// DefaultArgon2Params follow ADR 0005 (64 MiB, t=2, p=1).
var DefaultArgon2Params = Argon2Params{MemoryKiB: 64 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// PasswordHasher hashes and verifies passwords with Argon2id.
type PasswordHasher struct {
	params Argon2Params
}

// NewPasswordHasher returns a hasher using p for new hashes.
func NewPasswordHasher(p Argon2Params) *PasswordHasher { return &PasswordHasher{params: p} }

var errMalformedHash = errors.New("malformed password hash")

// Hash returns an encoded hash: $argon2id$v=19$m=…,t=…,p=…$<salt>$<key>.
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, h.params.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		h.params.MemoryKiB, h.params.Time, h.params.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded, and whether encoded should be rehashed
// because it was produced with different parameters.
func (h *PasswordHasher) Verify(password, encoded string) (match, needsRehash bool, err error) {
	p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, p.KeyLen)
	match = subtle.ConstantTimeCompare(got, key) == 1
	needsRehash = p.MemoryKiB != h.params.MemoryKiB || p.Time != h.params.Time ||
		p.Threads != h.params.Threads || p.KeyLen != h.params.KeyLen || len(salt) != int(h.params.SaltLen)
	return match, needsRehash, nil
}

func decodeHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	p.SaltLen, p.KeyLen = uint32(len(salt)), uint32(len(key)) //nolint:gosec // lengths come from a short, validated hash
	return p, salt, key, nil
}
