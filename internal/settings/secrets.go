package settings

import (
	"cmp"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/dakriy/doorcode/internal/connector"
)

// SecretKeyVar names the environment variable holding the key that
// encrypts the secret settings the store holds.
const SecretKeyVar = "DOORCODE_SECRET_KEY"

// secretKeyBytes is how long the key is: an AES-256 key.
const secretKeyBytes = 32

// Mask is what a secret setting shows in place of a value it has.
const Mask = "********"

// sealedEncoding writes the parts of an encrypted value.
var sealedEncoding = base64.RawURLEncoding

// Secrets encrypts the values of secret settings before the store holds
// them and decrypts them when the application needs them, with AES-256-GCM
// under the key the environment gives. Each value is bound to its
// setting's key, so a value stored under one setting cannot be read as
// another's.
type Secrets struct {
	aead    cipher.AEAD
	entropy connector.Entropy
}

// OpenSecrets reads the key from env and returns the Secrets it makes,
// drawing each value's nonce from entropy. It refuses a key that is
// missing or is not 32 bytes in standard base64.
func OpenSecrets(env connector.Environment, entropy connector.Entropy) (Secrets, error) {
	value, _ := env.Lookup(SecretKeyVar)
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != secretKeyBytes {
		return Secrets{}, fmt.Errorf("%s must be %d random bytes in base64, such as the output of openssl rand -base64 %d; "+
			"it encrypts the secret settings the database holds", SecretKeyVar, secretKeyBytes, secretKeyBytes)
	}
	// Neither can fail: the key is the length AES-256 takes, and GCM takes
	// any AES block.
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	return Secrets{aead: aead, entropy: entropy}, nil
}

// seal is value encrypted for the setting named key: its nonce and its
// ciphertext, each in base64url, joined by a dot.
func (s Secrets) seal(key, value string) string {
	nonce := s.entropy.Bytes(s.aead.NonceSize())
	sealed := s.aead.Seal(nil, nonce, []byte(value), []byte(key))
	return sealedEncoding.EncodeToString(nonce) + "." + sealedEncoding.EncodeToString(sealed)
}

// open decrypts stored, the value seal made for the setting named key.
func (s Secrets) open(key, stored string) (string, error) {
	nonceText, sealedText, _ := strings.Cut(stored, ".")
	nonce, nonceErr := sealedEncoding.DecodeString(nonceText)
	sealed, sealedErr := sealedEncoding.DecodeString(sealedText)
	if cmp.Or(nonceErr, sealedErr) != nil || len(nonce) != s.aead.NonceSize() {
		return "", unreadable(key)
	}
	value, err := s.aead.Open(nil, nonce, sealed, []byte(key))
	if err != nil {
		return "", unreadable(key)
	}
	return string(value), nil
}

// secret returns the value of the secret setting named key: as the
// environment gives it, decrypted as stored holds it, or empty, which
// every secret defaults to.
func (s Service) secret(key string, stored map[string]connector.StoredSetting) (string, error) {
	if value, set := s.Env.Lookup(envVarFor(key)); set {
		return value, nil
	}
	row, found := stored[key]
	if !found || row.Value == "" {
		return "", nil
	}
	return s.Secrets.open(key, row.Value)
}

// storable is value as the store holds it for the setting def: encrypted
// when def is secret, unless it is empty, which a cleared secret is.
func (s Service) storable(def definition, value string) string {
	if def.secret && value != "" {
		return s.Secrets.seal(def.key, value)
	}
	return value
}

// keeps reports whether storing value for the setting def leaves it as it
// is: an empty value for a secret, which forms and the API offer blank.
func keeps(def definition, value string) bool {
	return def.secret && value == ""
}

// shown is s as the setting def lets it be seen, in its section with its
// label and help: a secret's value masked.
func shown(def definition, s Setting) Setting {
	s.Secret, s.Section, s.Label, s.Help = def.secret, def.section, def.label, def.help
	if def.secret && s.Value != "" {
		s.Value = Mask
	}
	return s
}

// unreadable reports a stored secret that cannot be decrypted.
func unreadable(key string) error {
	return fmt.Errorf("the stored %s cannot be decrypted with the key in %s; store it again", key, SecretKeyVar)
}
