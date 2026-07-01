package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"pgregory.net/rapid"
)

// Property 13: Rate limiting enforcement
// Feature: agent-api-server, Property 13: Rate limiting enforcement
func TestRateLimitingEnforcement(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("exceeding rate limit returns 429 with Retry-After", func(t *testing.T) {
		// Use a small limit so we can exhaust it quickly in tests.
		rpm := 3
		rl := NewRateLimiter(rpm)
		handler := rl.Middleware(inner)

		ip := "10.0.0.1:12345"

		// Exhaust the burst tokens.
		for i := 0; i < rpm; i++ {
			req := httptest.NewRequest("GET", "/run", nil)
			req.RemoteAddr = ip
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
			}
		}

		// Next request should be rate limited.
		req := httptest.NewRequest("GET", "/run", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 after exceeding limit, got %d", rec.Code)
		}
		retryAfter := rec.Header().Get("Retry-After")
		if retryAfter == "" {
			t.Fatal("expected Retry-After header on 429 response")
		}
		secs, err := strconv.Atoi(retryAfter)
		if err != nil || secs < 1 {
			t.Fatalf("Retry-After must be a positive integer, got %q", retryAfter)
		}
	})

	t.Run("different IPs have independent buckets", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			ip1 := rapid.StringMatching(`10\.0\.0\.[0-9]+`).Draw(t, "ip1")
			ip2 := rapid.StringMatching(`10\.0\.1\.[0-9]+`).Draw(t, "ip2")
			if ip1 == ip2 {
				t.Skip("generated IPs match, skipping")
			}

			rpm := 2
			rl := NewRateLimiter(rpm)
			handler := rl.Middleware(inner)

			// Exhaust ip1's bucket.
			for i := 0; i < rpm; i++ {
				req := httptest.NewRequest("GET", "/run", nil)
				req.RemoteAddr = ip1 + ":9999"
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
			}

			// ip2 should still be under limit.
			req := httptest.NewRequest("GET", "/run", nil)
			req.RemoteAddr = ip2 + ":9999"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("ip2 should not be rate limited, got %d", rec.Code)
			}
		})
	})

	t.Run("health is always exempt from rate limiting", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			rpm := rapid.IntRange(1, 5).Draw(t, "rpm")
			rl := NewRateLimiter(rpm)
			handler := rl.Middleware(inner)

			ip := "192.168.1.1:1234"

			// Make far more requests than the limit to /health.
			for i := 0; i < rpm*3; i++ {
				req := httptest.NewRequest("GET", "/health", nil)
				req.RemoteAddr = ip
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("/health request %d: expected 200, got %d", i+1, rec.Code)
				}
			}
		})
	})
}
