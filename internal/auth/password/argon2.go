// Package password hashes and verifies passwords with argon2id, encoding the
// parameters and salt into a self-describing PHC string.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	timeCost    = 1
	memoryCost  = 64 * 1024
	parallelism = 4
	saltLen     = 16
	keyLen      = 32
)

// Bounds on parameters read back from a stored hash. argon2.IDKey panics on
// t=0 or p=0, and m sets how much memory one Verify allocates, so a corrupt
// or hostile hash must not reach it unchecked. The upper bounds leave room to
// raise the cost later without letting one hash allocate unbounded memory.
const (
	maxTime   = 16
	maxMemory = 256 * 1024 // KiB
	maxLanes  = 16
)

var errInvalidParams = errors.New("invalid hash parameters")

// Hash returns an argon2id PHC-encoded hash of plain.
func Hash(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, timeCost, memoryCost, parallelism, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryCost, timeCost, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether plain matches the argon2id PHC-encoded hash.
func Verify(plain, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	if version != argon2.Version || t < 1 || t > maxTime || p < 1 || p > maxLanes || m < 8*uint32(p) || m > maxMemory {
		return false, errInvalidParams
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	// Bound-check the key length before casting to uint32: a hostile PHC
	// string could otherwise encode a key larger than argon2 accepts, and
	// rejecting zero-length keys also fails fast on malformed hashes.
	wantLen := uint64(len(want))
	if wantLen == 0 || wantLen > math.MaxUint32 {
		return false, errors.New("invalid key length")
	}
	got := argon2.IDKey([]byte(plain), salt, t, m, p, uint32(wantLen))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
