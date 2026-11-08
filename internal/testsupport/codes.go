package testsupport

import (
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// StoredCodeHash is the door code hash the store holds for the user with
// email, empty when they have no code.
func (s *Store) StoredCodeHash(email string) string {
	u := s.userByEmail(email)
	if u.CodeHash == nil {
		return ""
	}
	return *u.CodeHash
}

// HashCode is the hash of code under the site's door code hashing
// parameters the store holds: argon2id with the parameters and salt they
// name, as a PHC string. It is worked out here independently of the
// application, so a test can check what the application stored.
func (s *Store) HashCode(code string) string {
	var version int
	var memory, iterations uint32
	var parallelism uint8
	var salt string
	if _, err := fmt.Sscanf(s.codeParams, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s", &version, &memory, &iterations, &parallelism, &salt); err != nil {
		panic("stub: no door code hashing parameters: " + err.Error())
	}
	raw, err := base64.RawStdEncoding.DecodeString(salt)
	if err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(code), raw, iterations, memory, parallelism, 32)
	return s.codeParams + "$" + base64.RawStdEncoding.EncodeToString(key)
}
