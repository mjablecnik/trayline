package api

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"remote/core"
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter enforces per-IP request rate limits using a token bucket.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	rpm      int
}

// NewRateLimiter creates a RateLimiter allowing requestsPerMinute per IP.
// A background goroutine periodically removes stale entries.
func NewRateLimiter(requestsPerMinute int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*ipLimiter),
		rpm:      requestsPerMinute,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	ipl, ok := rl.limiters[ip]
	if !ok {
		r := rate.Limit(float64(rl.rpm) / 60.0)
		ipl = &ipLimiter{
			limiter: rate.NewLimiter(r, rl.rpm),
		}
		rl.limiters[ip] = ipl
	}
	ipl.lastSeen = time.Now()
	return ipl.limiter
}

func (rl *RateLimiter) cleanup() {
	for {
		time.Sleep(10 * time.Minute)
		rl.mu.Lock()
		for ip, ipl := range rl.limiters {
			if time.Since(ipl.lastSeen) > 10*time.Minute {
				delete(rl.limiters, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns an http.Handler that enforces the rate limit for all paths
// except /health.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		ip := clientIP(r)
		limiter := rl.getLimiter(ip)

		res := limiter.Reserve()
		delay := res.Delay()
		if delay > 0 {
			res.Cancel()
			retryAfter := int(math.Ceil(delay.Seconds()))
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			writeJSON(w, http.StatusTooManyRequests, core.ErrorResponse{
				Error:   "RATE_LIMITED",
				Message: "too many requests, retry after " + strconv.Itoa(retryAfter) + " seconds",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the client IP from the request, preferring X-Forwarded-For.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
