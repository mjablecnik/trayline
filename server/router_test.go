package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestRouter creates a fully assembled router backed by stub handlers.
// authToken is used for the Authorization header; any non-empty value works.
func newTestRouter(t *testing.T, authToken string) http.Handler {
	t.Helper()

	cfg := &Config{
		MaxConcurrentTasks: 2,
		TaskTimeout:        5 * time.Second,
		SessionTimeout:     24 * time.Hour,
		RateLimit:          300,
		WorkspaceHostDir:   t.TempDir(),
	}

	mock := newMockContainerClient()
	mock.createErr = context.DeadlineExceeded // tasks fail immediately → no blocking
	logger := NewLogger("")

	taskStore := NewTaskStore()
	sessionStore := NewSessionStore()
	cm := NewContainerManager(mock, cfg, logger)

	taskH := NewTaskHandler(taskStore, cm, logger)
	sessionH := NewSessionHandler(sessionStore, cm, logger, cfg)
	health := &HealthHandler{}
	rl := NewRateLimiter(cfg.RateLimit)

	return NewRouter(health, taskH, sessionH, authToken, rl, logger)
}

func TestRouterHealthNoAuth(t *testing.T) {
	router := newTestRouter(t, "secret-token")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// Deliberately omit Authorization header — health must bypass auth.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status=ok, got %q", body["status"])
	}
}

func TestRouterProtectedRouteNoToken(t *testing.T) {
	router := newTestRouter(t, "secret-token")

	req := httptest.NewRequest(http.MethodGet, "/runs", nil)
	// No Authorization header.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Error != "UNAUTHORIZED" {
		t.Fatalf("expected UNAUTHORIZED error, got %q", body.Error)
	}
}

func TestRouterProtectedRouteWithValidToken(t *testing.T) {
	token := "my-test-token"
	router := newTestRouter(t, token)

	req := httptest.NewRequest(http.MethodGet, "/runs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// 200 is expected — /runs returns an empty list.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", rec.Code)
	}
}

func TestRouterPanicHandlerReturns500(t *testing.T) {
	logger := NewLogger("")
	// Wrap a panicking handler directly with recoveryMiddleware.
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	rl := NewRateLimiter(300)
	handler := recoveryMiddleware(logger, rl.Middleware(panicking))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	// Must not crash the test process.
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on panic, got %d", rec.Code)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Error != "INTERNAL_ERROR" {
		t.Fatalf("expected INTERNAL_ERROR, got %q", body.Error)
	}
}

func TestRequestIDMiddlewareAttachesID(t *testing.T) {
	var capturedID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := requestIDMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID == "" {
		t.Fatal("expected non-empty request ID in context")
	}
	// UUID should contain hyphens.
	if !strings.Contains(capturedID, "-") {
		t.Fatalf("request ID %q does not look like a UUID", capturedID)
	}
}
