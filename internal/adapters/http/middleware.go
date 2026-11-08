package http

import (
	"crypto/subtle"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// requireToken guards a handler with a Bearer token. An empty token means the
// operator chose the open dev default: the handler runs unwrapped and no
// comparison happens. The check is constant-time so a wrong token reveals
// nothing about the right one.
func requireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "a valid bearer token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimiter is a per-client-IP token bucket over the expensive endpoints. It
// exists so one abusive client cannot hog the model behind ask/agent; health,
// stats and the static UI are never limited.
type RateLimiter struct {
	rps   float64
	burst int

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter returns a limiter refilling rps tokens per second up to burst.
// Non-positive values fall back to the documented dev defaults.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	if rps <= 0 {
		rps = 1
	}
	if burst <= 0 {
		burst = 5
	}
	return &RateLimiter{rps: rps, burst: burst, buckets: make(map[string]*bucket)}
}

// Allow reports whether ip may proceed now. When it may not, it also reports
// how long to wait for the next token, for the Retry-After header.
func (l *RateLimiter) Allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{tokens: float64(l.burst), last: now}
		l.buckets[ip] = b
		if len(l.buckets) > 1024 {
			l.sweep(now)
		}
	}

	elapsed := now.Sub(b.last).Seconds()
	b.tokens = math.Min(float64(l.burst), b.tokens+elapsed*l.rps)
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration(math.Ceil((1 - b.tokens) / l.rps * float64(time.Second)))
}

// sweep drops idle full buckets so the map cannot grow with every scanner that
// ever knocked. Callers hold the lock.
func (l *RateLimiter) sweep(now time.Time) {
	for ip, b := range l.buckets {
		if b.tokens >= float64(l.burst) && now.Sub(b.last) > time.Minute {
			delete(l.buckets, ip)
		}
	}
}

// limit applies the bucket before the handler runs. Denials answer 429 with a
// Retry-After because the headers are still unsent here; inside the SSE stream
// that would only be possible as an event.
func (l *RateLimiter) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.Allow(clientIP(r)); !ok {
			secs := int(math.Ceil(wait.Seconds()))
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded, retry later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP prefers the leftmost X-Forwarded-For entry when a trusted proxy sets
// it, and otherwise uses the connection address without the port.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip, _, _ := strings.Cut(fwd, ","); strings.TrimSpace(ip) != "" {
			return strings.TrimSpace(ip)
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
