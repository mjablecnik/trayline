package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddleware_NoTokenConfigured_Passthrough(t *testing.T) {
	h := authMiddleware("", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no token configured, got %d", rec.Code)
	}
}

func TestAuthMiddleware_HealthAlwaysExempt(t *testing.T) {
	h := authMiddleware("secret", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected /health to bypass auth, got %d", rec.Code)
	}
}

func TestAuthMiddleware_RejectsMissingAuthHeader(t *testing.T) {
	h := authMiddleware("secret", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no Authorization header, got %d", rec.Code)
	}
}

func TestAuthMiddleware_RejectsWrongToken(t *testing.T) {
	h := authMiddleware("secret", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}
}

func TestAuthMiddleware_RejectsMalformedHeader(t *testing.T) {
	h := authMiddleware("secret", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.Header.Set("Authorization", "secret") // missing "Bearer " prefix
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for header without Bearer prefix, got %d", rec.Code)
	}
}

func TestAuthMiddleware_AcceptsCorrectToken(t *testing.T) {
	h := authMiddleware("secret", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct token, got %d", rec.Code)
	}
}

// Regression test for the finding this closes: unauthenticated arbitrary
// shell command execution via POST /projects/{project}/tasks.
func TestAuthMiddleware_BlocksUnauthenticatedTaskCreation(t *testing.T) {
	h := authMiddleware("secret", okHandler())

	req := httptest.NewRequest(http.MethodPost, "/projects/x/tasks", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected task creation to require auth, got %d", rec.Code)
	}
}
