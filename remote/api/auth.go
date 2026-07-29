package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"remote/core"
)

// AuthMiddleware validates the Bearer token for all paths except /health.
// Uses constant-time comparison to prevent timing attacks.
func AuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
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
