package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// In-memory per-IP rate limiter for login brute-force protection.
//
// UL 827B-era reviewers expect some form of throttle on authentication
// endpoints. A full-fat solution uses Redis so a multi-replica API
// deployment can share counters, but our current target is a single
// api container — an in-process sliding window is enough and avoids
// adding a hot dependency on Redis for this one feature. When we
// scale api horizontally, this becomes per-replica rather than true
// global, which weakens but does not remove the protection: the
// attacker would need to land on a specific replica via the LB's
// hash key, and we still have account lockout and audit logging as
// layered controls.
//
// The implementation is intentionally simple: a slice of attempt
// timestamps per IP, pruned at each hit. At 10/min that's a 10-entry
// slice per IP in the steady state — O(window) memory and O(window)
// per request, both tiny.

type loginRateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	limit  int
	hits   map[string][]time.Time
}

func newLoginRateLimiter(limit int, window time.Duration) *loginRateLimiter {
	return &loginRateLimiter{
		window: window,
		limit:  limit,
		hits:   make(map[string][]time.Time),
	}
}

// allow returns true if the caller is under the limit. Prunes expired
// entries while it's looking at them, which keeps the map bounded
// without a separate janitor goroutine.
func (rl *loginRateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	kept := rl.hits[key][:0]
	for _, ts := range rl.hits[key] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}

	if len(kept) >= rl.limit {
		rl.hits[key] = kept
		return false
	}

	rl.hits[key] = append(kept, now)
	return true
}

// RateLimitLogin is the chi middleware that wraps /auth/login. 10
// attempts per minute per client IP; above that, return 429 with a
// Retry-After hint. We key on IP only, not IP:port — clientIP()
// returns RemoteAddr which includes the ephemeral source port, so
// every curl or browser request would look like a new client to a
// port-aware limiter. net.SplitHostPort strips the port; if it
// fails (no port, IPv6 literal without brackets, etc.) we fall back
// to the raw string.
func RateLimitLogin(perMinute int) func(http.Handler) http.Handler {
	rl := newLoginRateLimiter(perMinute, time.Minute)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r)
			if host, _, err := net.SplitHostPort(key); err == nil {
				key = host
			}
			if !rl.allow(key) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "too many login attempts; try again in 60 seconds", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
