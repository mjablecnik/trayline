package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// authMiddleware requires a valid Bearer token on every request except
// /health, when token is non-empty. When token is empty (server bound to
// loopback only, per Config's validation), it is a no-op passthrough — this
// tool's whole purpose is running arbitrary shell commands, so a token is
// mandatory the moment the server is reachable off-host (see isLoopbackAddr).
// Uses constant-time comparison to avoid leaking the token via timing.
func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		provided, ok := strings.CutPrefix(authHeader, "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid Authorization header")
			return
		}

		next.ServeHTTP(w, r)
	})
}
