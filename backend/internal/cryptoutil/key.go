package cryptoutil

import (
	"crypto/hkdf"
	"crypto/sha256"
)

const keySize = 32

// DeriveKey creates a purpose-bound AES-256 key from the application secret.
func DeriveKey(secret, purpose string) ([]byte, error) {
	return hkdf.Key(sha256.New, []byte(secret), nil, purpose, keySize)
}

// LegacySHA256Key reproduces the unversioned v1 encryption key for read-only
// compatibility. New ciphertext must use DeriveKey.
func LegacySHA256Key(secret string) []byte {
	// This fast hash matches the v1 ciphertext format and is never used to
	// create new ciphertext or verify credentials.
	// lgtm[go/weak-sensitive-data-hashing]
	// codeql[go/weak-sensitive-data-hashing]
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
