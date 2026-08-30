// Package auth implements the access gate: password hashing, persistent
// sessions and login throttling. The whole service sits behind it — one
// shared viewer login plus a separate admin login (see docs/design.md §5).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"

	"streamingestarr/utils"
)

const (
	// OWASP's floor for argon2id. This runs on a small VPS beside the HLS
	// packager, and the login throttle bounds how many hashes can run at
	// once, so the memory-heavy profiles buy nothing here.
	argonMemoryKiB = 19456
	argonPasses    = 2
	argonThreads   = 1
	argonTagBytes  = 32
	argonSaltBytes = 16

	// MinPasswordLength is enforced wherever a password is set.
	MinPasswordLength = 8
)

var argonParamsPattern = regexp.MustCompile(`^m=(\d+),t=(\d+),p=(\d+)$`)

// HashPassword hashes a password for storage. The stored string carries its
// own parameters — "argon2id$m=19456,t=2,p=1$<salt>$<tag>" — so raising them
// later does not invalidate existing passwords.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	salt := make([]byte, argonSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	tag := argon2.IDKey([]byte(password), salt, argonPasses, argonMemoryKiB, argonThreads, argonTagBytes)
	return fmt.Sprintf("argon2id$m=%d,t=%d,p=%d$%s$%s",
		argonMemoryKiB, argonPasses, argonThreads,
		hex.EncodeToString(salt), hex.EncodeToString(tag)), nil
}

// VerifyPassword checks a password against a stored hash in constant time.
// It accepts our argon2id format and, for the admin password set through the
// pre-existing flows, bcrypt (the format Owncast used).
func VerifyPassword(password, stored string) bool {
	if stored == "" || password == "" {
		return false
	}
	if strings.HasPrefix(stored, "argon2id$") {
		return verifyArgon2id(password, stored)
	}
	// bcrypt ($2a$ / $2b$ …) via the inherited helper.
	return utils.CompareHash(stored, password) == nil
}

func verifyArgon2id(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 {
		return false
	}
	m := argonParamsPattern.FindStringSubmatch(parts[1])
	if m == nil {
		return false
	}
	memory, err1 := strconv.ParseUint(m[1], 10, 32)
	passes, err2 := strconv.ParseUint(m[2], 10, 8)
	threads, err3 := strconv.ParseUint(m[3], 10, 8)
	salt, err4 := hex.DecodeString(parts[2])
	tag, err5 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || len(tag) == 0 {
		return false
	}
	derived := argon2.IDKey([]byte(password), salt, uint32(passes), uint32(memory), uint8(threads), uint32(len(tag)))
	return subtle.ConstantTimeCompare(derived, tag) == 1
}

// ErrWeakPassword is returned by validators when a password is too short.
var ErrWeakPassword = errors.New("password too short")
