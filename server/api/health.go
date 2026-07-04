package api

import (
	"net/http"
	"sync/atomic"
)

// HealthHandler serves GET /health. Returns 200 while accepting traffic and
// 503 during graceful shutdown.
type HealthHandler struct {
	shuttingDown atomic.Bool
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.shuttingDown.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "shutting_down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SetShuttingDown signals the health handler to return 503.
func (h *HealthHandler) SetShuttingDown() {
	h.shuttingDown.Store(true)
}
