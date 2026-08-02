// Package auth guards the controller console and API with a single operator
// credential, an HMAC-signed session cookie, and per-client login throttling.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	hashScheme = "pbkdf2-sha256"
	// OWASP 2023 guidance for PBKDF2-HMAC-SHA256.
	defaultIterations = 600_000
	minIterations     = 100_000
	saltLength        = 16
	keyLength         = 32
)

// ErrMalformedHash reports a password hash the controller cannot interpret.
// The message never echoes the hash so it stays out of logs.
var ErrMalformedHash = errors.New("password hash is malformed")

// HashPassword derives a storable hash from a plaintext password. The result is
// safe to place in an environment variable or secret store.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, defaultIterations, keyLength)
	if err != nil {
		return "", fmt.Errorf("derive key: %w", err)
	}
	return strings.Join([]string{
		hashScheme,
		strconv.Itoa(defaultIterations),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	}, "$"), nil
}

type parsedHash struct {
	iterations int
	salt       []byte
	key        []byte
}

func parseHash(encoded string) (parsedHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return parsedHash{}, ErrMalformedHash
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < minIterations {
		return parsedHash{}, ErrMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return parsedHash{}, ErrMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(key) == 0 {
		return parsedHash{}, ErrMalformedHash
	}
	return parsedHash{iterations: iterations, salt: salt, key: key}, nil
}

// Credential is the single operator account the console accepts.
type Credential struct {
	Username string
	hash     parsedHash
}

// NewCredential validates a username and encoded hash pair up front so a
// malformed secret fails at startup instead of on the first login attempt.
func NewCredential(username, encodedHash string) (Credential, error) {
	if strings.TrimSpace(username) == "" {
		return Credential{}, errors.New("username must not be empty")
	}
	parsed, err := parseHash(encodedHash)
	if err != nil {
		return Credential{}, err
	}
	return Credential{Username: username, hash: parsed}, nil
}

// Verify reports whether the supplied pair matches. It derives the key even
// when the username is wrong so the response time does not reveal which half
// failed.
func (credential Credential) Verify(username, password string) bool {
	derived, err := pbkdf2.Key(sha256.New, password, credential.hash.salt, credential.hash.iterations, len(credential.hash.key))
	if err != nil {
		return false
	}
	passwordMatch := subtle.ConstantTimeCompare(derived, credential.hash.key)
	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(credential.Username))
	return passwordMatch&usernameMatch == 1
}
