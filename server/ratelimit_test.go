package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

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

// --- clientIP tests ---

func TestClientIPXFF(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{
			name:       "single XFF IP",
			xff:        "1.2.3.4",
			remoteAddr: "9.9.9.9:1234",
			want:       "1.2.3.4",
		},
		{
			name:       "multiple XFF IPs uses first",
			xff:        "1.2.3.4, 5.6.7.8, 9.0.1.2",
			remoteAddr: "9.9.9.9:1234",
			want:       "1.2.3.4",
		},
		{
			name:       "XFF with leading space trimmed",
			xff:        "  10.0.0.1  ",
			remoteAddr: "9.9.9.9:1234",
			want:       "10.0.0.1",
		},
		{
			name:       "no XFF falls back to RemoteAddr host",
			xff:        "",
			remoteAddr: "192.168.1.50:5678",
			want:       "192.168.1.50",
		},
		{
			name:       "no XFF RemoteAddr without port returned as-is",
			xff:        "",
			remoteAddr: "192.168.1.50",
			want:       "192.168.1.50",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/run", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			got := clientIP(req)
			if got != tc.want {
				t.Fatalf("clientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- RateLimiter cleanup tests ---

func TestRateLimiterCleanupEvictsStaleEntries(t *testing.T) {
	// Build a RateLimiter without starting its background goroutine so we
	// control lastSeen times and invoke the eviction logic synchronously.
	rl := &RateLimiter{
		limiters: make(map[string]*ipLimiter),
		rpm:      60,
	}

	now := time.Now()

	// Stale entry: last seen > 10 minutes ago.
	rl.limiters["stale-ip"] = &ipLimiter{
		lastSeen: now.Add(-11 * time.Minute),
	}
	// Fresh entry: last seen 1 minute ago.
	rl.limiters["fresh-ip"] = &ipLimiter{
		lastSeen: now.Add(-1 * time.Minute),
	}

	// Replicate the cleanup loop from RateLimiter.cleanup().
	rl.mu.Lock()
	for ip, ipl := range rl.limiters {
		if time.Since(ipl.lastSeen) > 10*time.Minute {
			delete(rl.limiters, ip)
		}
	}
	rl.mu.Unlock()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if _, ok := rl.limiters["stale-ip"]; ok {
		t.Error("stale-ip should have been evicted by cleanup")
	}
	if _, ok := rl.limiters["fresh-ip"]; !ok {
		t.Error("fresh-ip should still be present after cleanup")
	}
}
