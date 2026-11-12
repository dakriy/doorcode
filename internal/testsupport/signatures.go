package testsupport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
)

// signatureBlockBytes is how long a signed image's signature block is: a
// version word and the signature r || s.
const signatureBlockBytes = 68

// ErrBadSignature reports a signed image whose block is missing, of
// another version, or does not verify.
var ErrBadSignature = errors.New("bad signature")

// SignedImage is the image signed carries, checked as a door checks it:
// signed ends in a 68-byte block, the version word 0 little-endian and the
// P-256 signature r || s of the SHA-256 of the image, which must verify
// with publicKey, the hex of the point x || y. It reports ErrBadSignature
// when it does not.
func SignedImage(publicKey, signed string) (string, error) {
	point, err := hex.DecodeString("04" + publicKey)
	if err != nil {
		return "", err
	}
	key, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
	if err != nil {
		return "", err
	}
	if len(signed) < signatureBlockBytes {
		return "", ErrBadSignature
	}
	image, block := signed[:len(signed)-signatureBlockBytes], []byte(signed[len(signed)-signatureBlockBytes:])
	sum := sha256.Sum256([]byte(image))
	r, s := new(big.Int).SetBytes(block[4:36]), new(big.Int).SetBytes(block[36:])
	if string(block[:4]) != "\x00\x00\x00\x00" || !ecdsa.Verify(key, sum[:], r, s) {
		return "", ErrBadSignature
	}
	return image, nil
}

// SHA256 is the SHA-256 of body in hex.
func SHA256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
