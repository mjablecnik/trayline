package main

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

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
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{
				Error:   "UNAUTHORIZED",
				Message: "missing or invalid Authorization header",
			})
			return
		}

		provided := strings.TrimPrefix(authHeader, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{
				Error:   "UNAUTHORIZED",
				Message: "invalid token",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}
