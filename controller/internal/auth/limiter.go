package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// A human operator needs a handful of tries; a script needs thousands.
	defaultBurst      = 8
	defaultRefill     = 30 * time.Second
	limiterSweepAfter = 30 * time.Minute
	// Bounds the map so a spoofed-source flood cannot exhaust memory.
	maxTrackedClients = 4096
)

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// LoginLimiter throttles login attempts per client address using a token
// bucket. Successful logins refund their token so a working operator is never
// throttled.
type LoginLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	burst   float64
	refill  time.Duration
	now     func() time.Time
}

// NewLoginLimiter builds a limiter with the default budget.
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		buckets: make(map[string]*bucket),
		burst:   defaultBurst,
		refill:  defaultRefill,
		now:     time.Now,
	}
}

// Allow consumes one token for the client and reports whether the attempt may
// proceed.
func (limiter *LoginLimiter) Allow(client string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	limiter.sweepLocked(now)

	entry, ok := limiter.buckets[client]
	if !ok {
		if len(limiter.buckets) >= maxTrackedClients {
			// Table is saturated by unknown sources; fail closed rather than grow.
			return false
		}
		entry = &bucket{tokens: limiter.burst, lastSeen: now}
		limiter.buckets[client] = entry
	}

	replenished := now.Sub(entry.lastSeen).Seconds() / limiter.refill.Seconds()
	entry.tokens = min(entry.tokens+replenished, limiter.burst)
	entry.lastSeen = now
	if entry.tokens < 1 {
		return false
	}
	entry.tokens--
	return true
}

// Refund returns the token spent by a successful login.
func (limiter *LoginLimiter) Refund(client string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if entry, ok := limiter.buckets[client]; ok {
		entry.tokens = min(entry.tokens+1, limiter.burst)
	}
}

// RetryAfter reports how long a throttled client should wait before retrying.
func (limiter *LoginLimiter) RetryAfter() time.Duration {
	return limiter.refill
}

func (limiter *LoginLimiter) sweepLocked(now time.Time) {
	for key, entry := range limiter.buckets {
		if now.Sub(entry.lastSeen) > limiterSweepAfter {
			delete(limiter.buckets, key)
		}
	}
}

// clientAddress identifies the caller for throttling. It deliberately ignores
// X-Forwarded-For: that header is caller-controlled, so trusting it would let
// an attacker mint a fresh budget per request.
func clientAddress(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}
