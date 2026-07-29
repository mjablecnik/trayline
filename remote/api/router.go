package api

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/google/uuid"

	"remote/core"
)

// NewRouter builds and returns the HTTP ServeMux with all routes and middleware applied.
// Middleware chain: rate limiter → auth → handler (health bypasses both).
func NewRouter(
	health *HealthHandler,
	taskH *TaskHandler,
	sessionH *SessionHandler,
	authToken string,
	rl *RateLimiter,
	logger *core.Logger,
) http.Handler {
	mux := http.NewServeMux()

	// Health endpoint — no auth, no rate limiting.
	mux.Handle("GET /health", health)

	// Task endpoints.
	mux.HandleFunc("POST /run", taskH.HandlePostRun)
	mux.HandleFunc("GET /run/{id}", taskH.HandleGetRun)
	mux.HandleFunc("GET /runs", taskH.HandleGetRuns)
	mux.HandleFunc("POST /run/{id}/cancel", taskH.HandleCancelRun)

	// Session endpoints.
	mux.HandleFunc("GET /chat", sessionH.HandleChat)
	mux.HandleFunc("GET /chat/{id}", sessionH.HandleChatReconnect)
	mux.HandleFunc("GET /sessions", sessionH.HandleGetSessions)
	mux.HandleFunc("POST /sessions/{id}/terminate", sessionH.HandleTerminateSession)

	// Apply middleware: recovery → rate limiter → auth → mux.
	return recoveryMiddleware(logger, rl.Middleware(AuthMiddleware(authToken, requestIDMiddleware(mux))))
}

// requestIDMiddleware attaches a unique request ID to every request context.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := core.WithRequestID(r.Context(), uuid.NewString())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// recoveryMiddleware catches panics in handlers and returns HTTP 500.
func recoveryMiddleware(logger *core.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				logger.Error(r.Context(), fmt.Sprintf("panic recovered: %v\n%s", rec, stack))
				writeJSON(w, http.StatusInternalServerError, core.ErrorResponse{
					Error:   "INTERNAL_ERROR",
					Message: "an unexpected error occurred",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
