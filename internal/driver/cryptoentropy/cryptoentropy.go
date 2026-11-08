// Package cryptoentropy implements connector.Entropy over crypto/rand.
package cryptoentropy

import "crypto/rand"

// Source reads the operating system's secure random source.
type Source struct{}

// Bytes returns n random bytes. crypto/rand.Read never fails; on a broken
// random source the runtime aborts the process instead.
func (Source) Bytes(n int) []byte {
	out := make([]byte, n)
	_, _ = rand.Read(out)
	return out
}
