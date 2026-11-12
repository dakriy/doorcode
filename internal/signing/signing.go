// Package signing holds the key the server signs firmware images with: an
// ECDSA P-256 key the doors check every image against.
package signing

import (
	"cmp"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
)

// SeedBytes is how many random bytes New makes a key from: 64 bits more
// than the curve's order takes, so reducing them leaves no bias worth the
// name (FIPS 186-5, appendix A.2.1).
const SeedBytes = 40

// scalarBytes is how long a private key is, and each coordinate of a
// public one.
const scalarBytes = 32

// fingerprintLength is how many hex characters of the public key's
// SHA-256 name it.
const fingerprintLength = 16

// ErrNotAKey reports a stored key that is not a P-256 private key.
var ErrNotAKey = errors.New("the stored firmware signing key is not a P-256 private key")

// Key is a firmware signing key.
type Key struct {
	private *ecdsa.PrivateKey
}

// New is the key made from seed, SeedBytes random bytes: their number
// reduced to a scalar from 1 to the curve's order less one.
func New(seed []byte) Key {
	below := new(big.Int).Sub(elliptic.P256().Params().N, big.NewInt(1))
	scalar := new(big.Int).Mod(new(big.Int).SetBytes(seed), below)
	scalar.Add(scalar, big.NewInt(1))
	// It cannot fail: the scalar is in the range the curve takes.
	private, _ := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar.FillBytes(make([]byte, scalarBytes)))
	return Key{private: private}
}

// Parse reads the key Text wrote.
func Parse(text string) (Key, error) {
	raw, hexErr := hex.DecodeString(text)
	private, keyErr := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if cmp.Or(hexErr, keyErr) != nil {
		return Key{}, ErrNotAKey
	}
	return Key{private: private}, nil
}

// Text is the private key as it is stored: its scalar, 32 bytes big-endian,
// in hex.
func (k Key) Text() string {
	// It cannot fail for a key New or Parse made.
	raw, _ := k.private.Bytes()
	return hex.EncodeToString(raw)
}

// Public is the public key's point x || y, each coordinate 32 bytes
// big-endian, without the leading byte that says it is uncompressed.
func (k Key) Public() []byte {
	// It cannot fail for a key on P-256.
	point, _ := k.private.PublicKey.Bytes()
	return point[1:]
}

// Fingerprint names the key: the first 16 hex characters of the SHA-256 of
// its public key.
func (k Key) Fingerprint() string {
	sum := sha256.Sum256(k.Public())
	return hex.EncodeToString(sum[:])[:fingerprintLength]
}
