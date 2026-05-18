// Package auth contains M6 authentication primitives: password hashing and
// the first-boot bootstrap that seeds the instance admin. Middleware and
// session handling land in M6.1.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonSaltLen = 16
	argonKeyLen  = 32
)

const argonPrefix = "$argon2id$v=19$m=65536,t=3,p=4$"

// ErrInvalidEncoding indicates a malformed PHC-style encoded hash.
var ErrInvalidEncoding = errors.New("auth: invalid encoded password hash")

// HashPassword derives an argon2id hash of pw with a fresh random salt and
// returns a PHC-style encoded string:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt-b64>$<hash-b64>
//
// Salt and hash are encoded with base64.RawStdEncoding (no padding), matching
// the de-facto PHC layout.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read random salt: %w", err)
	}
	hash := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := argonPrefix +
		base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(hash)
	return enc, nil
}

// VerifyPassword reports whether pw matches the PHC-encoded hash. It uses a
// constant-time compare on the derived key. Returns (false, ErrInvalidEncoding)
// when encoded is not a parseable argon2id PHC string.
func VerifyPassword(pw, encoded string) (bool, error) {
	if !strings.HasPrefix(encoded, argonPrefix) {
		return false, ErrInvalidEncoding
	}
	rest := encoded[len(argonPrefix):]
	parts := strings.Split(rest, "$")
	if len(parts) != 2 {
		return false, ErrInvalidEncoding
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil || len(salt) == 0 {
		return false, ErrInvalidEncoding
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil || len(want) == 0 {
		return false, ErrInvalidEncoding
	}
	got := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
