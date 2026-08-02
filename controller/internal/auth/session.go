package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	sessionKeyLength = 32
	nonceLength      = 16
)

// ErrInvalidSession covers every rejection reason. Callers must not distinguish
// between a forged signature and an expired token, so an attacker learns
// nothing from the response.
var ErrInvalidSession = errors.New("session is invalid")

// SessionSigner issues and verifies opaque session tokens. The key lives only
// in memory, so restarting the controller invalidates every session.
type SessionSigner struct {
	key []byte
	ttl time.Duration
}

// NewSessionSigner derives a signer from a random key. Supply a key to keep
// sessions valid across restarts; leave it empty to rotate on every start.
func NewSessionSigner(key []byte, ttl time.Duration) (*SessionSigner, error) {
	if ttl <= 0 {
		return nil, errors.New("session ttl must be positive")
	}
	if len(key) == 0 {
		key = make([]byte, sessionKeyLength)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate session key: %w", err)
		}
	}
	if len(key) < sessionKeyLength {
		return nil, fmt.Errorf("session key must be at least %d bytes", sessionKeyLength)
	}
	return &SessionSigner{key: key, ttl: ttl}, nil
}

// TTL reports how long an issued token stays valid.
func (signer *SessionSigner) TTL() time.Duration {
	return signer.ttl
}

// Issue mints a token for the supplied subject, valid until now+TTL.
func (signer *SessionSigner) Issue(subject string, now time.Time) (string, error) {
	if subject == "" {
		return "", errors.New("subject must not be empty")
	}
	nonce := make([]byte, nonceLength)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	payload := strings.Join([]string{
		base64.RawURLEncoding.EncodeToString([]byte(subject)),
		strconv.FormatInt(now.Add(signer.ttl).Unix(), 10),
		base64.RawURLEncoding.EncodeToString(nonce),
	}, "|")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + signer.sign(encoded), nil
}

// Verify returns the subject carried by a token that is both correctly signed
// and unexpired.
func (signer *SessionSigner) Verify(token string, now time.Time) (string, error) {
	encoded, signature, found := strings.Cut(token, ".")
	if !found {
		return "", ErrInvalidSession
	}
	// Compare before decoding so a forged token never reaches the parser.
	if !hmac.Equal([]byte(signature), []byte(signer.sign(encoded))) {
		return "", ErrInvalidSession
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrInvalidSession
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return "", ErrInvalidSession
	}
	subject, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(subject) == 0 {
		return "", ErrInvalidSession
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", ErrInvalidSession
	}
	if now.Unix() >= expiry {
		return "", ErrInvalidSession
	}
	return string(subject), nil
}

func (signer *SessionSigner) sign(encoded string) string {
	mac := hmac.New(sha256.New, signer.key)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
