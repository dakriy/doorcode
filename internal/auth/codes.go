package auth

import (
	"context"
	"fmt"
)

// CodeHash is the hash a door code is stored and looked up by. Every code
// is hashed with argon2id under the same site-wide parameters and salt, so
// equal codes have equal hashes: that keeps codes unique and lets a keypad
// entry be found by its hash. The parameters are fixed the first time a
// code is hashed, at the password hashing memory cost then in effect, so
// changing that setting later does not change any code's hash.
func (s Service) CodeHash(ctx context.Context, code string) (string, error) {
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return "", err
	}
	candidate := hashParams{
		memory:      current.PasswordHashMemoryKiB,
		iterations:  hashIterations,
		parallelism: hashParallelism,
		salt:        s.Entropy.Bytes(saltBytes),
	}
	stored, err := s.Store.CodeHashing(ctx, candidate.prefix())
	if err != nil {
		return "", err
	}
	p, err := parseHash(stored)
	if err != nil {
		return "", fmt.Errorf("door code hashing parameters %q: %w", stored, err)
	}
	return p.encode(code), nil
}
