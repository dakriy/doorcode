package auth

import (
	"context"
	"fmt"
)

// Door codes are hashed with argon2id at fixed parameters of their own,
// far cheaper than a password's, since every code check at a keypad costs
// one hash. They do not follow the password hashing setting.
const (
	codeHashMemoryKiB   = 2048
	codeHashIterations  = 1
	codeHashParallelism = 1
)

// CodeHash is the hash a door code is stored and looked up by. Every code
// is hashed with argon2id under the same site-wide parameters and salt, so
// equal codes have equal hashes: that keeps codes unique and lets a keypad
// entry be found by its hash. The parameters are fixed the first time a
// code is hashed, so a site that hashed codes under other parameters keeps
// them.
func (s Service) CodeHash(ctx context.Context, code string) (string, error) {
	candidate := hashParams{
		memory:      codeHashMemoryKiB,
		iterations:  codeHashIterations,
		parallelism: codeHashParallelism,
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
