package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"remote/core"
)

// AuthMiddleware validates the Bearer token for all paths except /health.
// Uses constant-time comparison to prevent timing attacks.
// WebSocket upgrade requests (Upgrade: websocket header) are passed through
// without token validation here — WebSocket handlers authenticate via the
// first message after the connection is established.
func AuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for WebSocket upgrades — they authenticate post-connect.
		if isWebSocketUpgrade(r) {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, core.ErrorResponse{
				Error:   "UNAUTHORIZED",
				Message: "missing or invalid Authorization header",
			})
			return
		}

		provided := strings.TrimPrefix(authHeader, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, core.ErrorResponse{
				Error:   "UNAUTHORIZED",
				Message: "invalid token",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// ValidateWSToken checks a token against the expected value using constant-time comparison.
// Used by WebSocket handlers to authenticate the first message.
func ValidateWSToken(provided, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
