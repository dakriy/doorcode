package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Password hashing is argon2id in the PHC string format. The memory cost
// comes from settings; the other parameters are fixed at the values OWASP
// recommends for a 19 MiB memory cost. A stored hash carries its own
// parameters, so changing the setting affects only passwords set afterwards.
const (
	hashIterations  = 2
	hashParallelism = 1
	hashKeyBytes    = 32
	saltBytes       = 16
	hashFormat      = "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s"
	hashParamFormat = "$argon2id$v=%d$m=%d,t=%d,p=%d$%s"
)

var hashEncoding = base64.RawStdEncoding

type hashParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
}

func (p hashParams) encode(password string) string {
	key := argon2.IDKey([]byte(password), p.salt, p.iterations, p.memory, p.parallelism, hashKeyBytes)
	return fmt.Sprintf(hashFormat, argon2.Version, p.memory, p.iterations, p.parallelism,
		hashEncoding.EncodeToString(p.salt), hashEncoding.EncodeToString(key))
}

// hashPassword hashes password with salt at the given memory cost.
func hashPassword(password string, salt []byte, memoryKiB uint32) string {
	return hashParams{memory: memoryKiB, iterations: hashIterations, parallelism: hashParallelism, salt: salt}.encode(password)
}

// passwordMatches reports whether password hashes to encoded. It rehashes
// password with the parameters and salt encoded carries and compares the
// whole strings, so a malformed salt or key, or another version, can never
// match.
func passwordMatches(encoded, password string) bool {
	var version int
	var p hashParams
	var rest string
	_, err := fmt.Sscanf(encoded, hashParamFormat, &version, &p.memory, &p.iterations, &p.parallelism, &rest)
	if err != nil {
		return false
	}
	salt, _, _ := strings.Cut(rest, "$")
	p.salt, _ = hashEncoding.DecodeString(salt)
	return subtle.ConstantTimeCompare([]byte(p.encode(password)), []byte(encoded)) == 1
}
