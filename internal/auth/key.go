// Package auth implements API key generation and verification.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"strings"
)

const (
	tokenPrefix  = "emk_"
	prefixBytes  = 8
	secretBytes  = 32
	prefixLength = 13 // base32 of 8 bytes, no padding
	secretLength = 52 // base32 of 32 bytes, no padding
	alphabet     = "abcdefghijklmnopqrstuvwxyz234567"
)

var encoding = base32.NewEncoding(alphabet).WithPadding(base32.NoPadding)

// ErrMalformedToken is returned when a token does not match emk_<prefix>_<secret>.
var ErrMalformedToken = errors.New("malformed api key")

// GeneratedKey is a newly issued key. Token is shown to the operator once and
// never stored; only SecretHash is persisted.
type GeneratedKey struct {
	Prefix     string
	Token      string
	SecretHash []byte
}

// GenerateKey creates a key with a random public prefix and secret.
func GenerateKey() (GeneratedKey, error) {
	buf := make([]byte, prefixBytes+secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return GeneratedKey{}, err
	}
	prefix := encoding.EncodeToString(buf[:prefixBytes])
	secret := encoding.EncodeToString(buf[prefixBytes:])
	return GeneratedKey{
		Prefix:     prefix,
		Token:      tokenPrefix + prefix + "_" + secret,
		SecretHash: HashSecret(secret),
	}, nil
}

// ParseToken splits a token into its prefix and secret.
func ParseToken(token string) (prefix, secret string, err error) {
	rest, ok := strings.CutPrefix(token, tokenPrefix)
	if !ok {
		return "", "", ErrMalformedToken
	}
	prefix, secret, ok = strings.Cut(rest, "_")
	if !ok || len(prefix) != prefixLength || len(secret) != secretLength ||
		!inAlphabet(prefix) || !inAlphabet(secret) {
		return "", "", ErrMalformedToken
	}
	return prefix, secret, nil
}

// HashSecret returns the SHA-256 of the secret. The secret has 256 bits of
// entropy, so a fast hash is sufficient.
func HashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// VerifySecret compares the secret with the stored hash in constant time.
func VerifySecret(secret string, hash []byte) bool {
	return subtle.ConstantTimeCompare(HashSecret(secret), hash) == 1
}

func inAlphabet(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune(alphabet, r) {
			return false
		}
	}
	return true
}
