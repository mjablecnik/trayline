package api

import (
	"net/http"
)

// CORSMiddleware adds CORS headers for the dashboard frontend and handles
// preflight OPTIONS requests. If allowedOrigin is empty, it is a no-op.
func CORSMiddleware(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if allowedOrigin == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			// Required for the dashboard's session cookie (see auth.go) to be
			// sent/received on cross-origin requests. Safe here because
			// allowedOrigin is always a single, explicitly configured origin
			// (see core.Config.DashboardOrigin) — never a wildcard or a
			// reflected request Origin header.
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "3600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
