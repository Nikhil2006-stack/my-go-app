package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a thread-safe per-IP sliding window rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	maxReqs int
	hits    map[string][]time.Time
}

// NewRateLimiter creates a RateLimiter allowing maxReqs requests per window per IP address.
func NewRateLimiter(maxReqs int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		window:  window,
		maxReqs: maxReqs,
		hits:    make(map[string][]time.Time),
	}
}

// Allow checks whether the given key (e.g. client IP) is within the allowed rate limit.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	timestamps := rl.hits[key]
	valid := timestamps[:0]
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= rl.maxReqs {
		rl.hits[key] = valid
		return false
	}

	rl.hits[key] = append(valid, now)
	return true
}

// ClientIP extracts the client IP address from X-Forwarded-For (used by Google Cloud Run)
// or falls back to RemoteAddr.
func ClientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

// SecurityHeaders adds standard HTTP security headers to all responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		next.ServeHTTP(w, r)
	})
}
