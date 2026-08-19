package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a per-client sliding-window rate limiter kept in memory.
// It is intentionally dependency-free (no Redis) so it protects the API even
// when Redis is unavailable. For multi-replica deployments the limit applies
// per replica, which still bounds abuse traffic.
type rateLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
	limit   int           // max requests per window
	window  time.Duration // window size

	lastSweep time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		windows:   make(map[string][]time.Time),
		limit:     limit,
		window:    window,
		lastSweep: time.Now(),
	}
}

// allow reports whether the client identified by key may proceed.
func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Periodically sweep stale entries to bound memory.
	if now.Sub(rl.lastSweep) > rl.window {
		for k, ts := range rl.windows {
			if len(ts) == 0 || ts[len(ts)-1].Before(cutoff) {
				delete(rl.windows, k)
			}
		}
		rl.lastSweep = now
	}

	ts := rl.windows[key]
	// Drop timestamps outside the window.
	i := 0
	for ; i < len(ts); i++ {
		if ts[i].After(cutoff) {
			break
		}
	}
	ts = ts[i:]

	if len(ts) >= rl.limit {
		rl.windows[key] = ts
		return false
	}

	rl.windows[key] = append(ts, now)
	return true
}

// clientIP extracts the client IP, preferring proxy headers set by the ingress.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First address is the original client.
		if idx := strings.IndexByte(xff, ','); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if rip := r.Header.Get("X-Real-Ip"); rip != "" {
		return strings.TrimSpace(rip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func tooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error":"rate limit exceeded, try again later"}`))
}

// Global limiter: generous ceiling per IP across the whole API.
var globalLimiter = newRateLimiter(300, time.Minute)

// Auth limiter: strict limit per IP for authentication/OTP endpoints to
// prevent OTP brute force, SMS-cost abuse, and credential guessing.
var authLimiter = newRateLimiter(10, time.Minute)

// RateLimit is the global per-IP rate limiting middleware (300 req/min/IP).
func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !globalLimiter.allow(clientIP(r)) {
			tooManyRequests(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AuthRateLimit is a strict per-IP rate limiting middleware (10 req/min/IP)
// for OTP and login endpoints.
func AuthRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authLimiter.allow("auth:" + clientIP(r)) {
			tooManyRequests(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
