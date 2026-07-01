package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pgregory.net/rapid"
)

// Property 12: Authentication enforcement
// Feature: agent-api-server, Property 12: Authentication enforcement
func TestAuthenticationEnforcement(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("non-matching token returns 401", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			validToken := rapid.StringN(1, 64, -1).Draw(t, "validToken")
			// Generate a token that is definitely not equal to validToken.
			wrongToken := rapid.StringN(1, 64, -1).Draw(t, "wrongToken")
			if wrongToken == validToken {
				t.Skip("generated tokens match, skipping")
			}

			handler := AuthMiddleware(validToken, inner)

			req := httptest.NewRequest("GET", "/run", nil)
			req.Header.Set("Authorization", "Bearer "+wrongToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 for wrong token, got %d", rec.Code)
			}
			var errResp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
				t.Fatalf("response body is not valid JSON: %v", err)
			}
			if errResp.Error == "" {
				t.Fatal("error field must not be empty")
			}
		})
	})

	t.Run("missing Authorization header returns 401", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			validToken := rapid.StringN(1, 64, -1).Draw(t, "validToken")
			path := rapid.SampledFrom([]string{"/run", "/runs", "/sessions", "/chat"}).Draw(t, "path")

			handler := AuthMiddleware(validToken, inner)

			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 with no auth header, got %d", rec.Code)
			}
		})
	})

	t.Run("wrong scheme returns 401", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			validToken := rapid.StringN(1, 64, -1).Draw(t, "validToken")

			handler := AuthMiddleware(validToken, inner)

			req := httptest.NewRequest("GET", "/run", nil)
			req.Header.Set("Authorization", "Basic "+validToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 for non-Bearer scheme, got %d", rec.Code)
			}
		})
	})

	t.Run("matching token passes through", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			validToken := rapid.StringN(1, 64, -1).Draw(t, "validToken")

			handler := AuthMiddleware(validToken, inner)

			req := httptest.NewRequest("GET", "/run", nil)
			req.Header.Set("Authorization", "Bearer "+validToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for valid token, got %d", rec.Code)
			}
		})
	})

	t.Run("health never requires auth", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			validToken := rapid.StringN(1, 64, -1).Draw(t, "validToken")
			// Provide any random or no token.
			anyToken := rapid.StringN(0, 64, -1).Draw(t, "anyToken")

			handler := AuthMiddleware(validToken, inner)

			req := httptest.NewRequest("GET", "/health", nil)
			if anyToken != "" {
				req.Header.Set("Authorization", "Bearer "+anyToken)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// /health must pass regardless of token.
			if rec.Code != http.StatusOK {
				t.Fatalf("/health: expected 200 regardless of token, got %d", rec.Code)
			}
		})
	})
}
