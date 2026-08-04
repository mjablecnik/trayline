package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddleware_NoOpWhenOriginEmpty(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := CORSMiddleware("")(next)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next handler to be called when origin is empty")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no CORS headers when origin is empty, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddleware_AddsHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := CORSMiddleware("http://localhost:5173")(next)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("expected Access-Control-Allow-Origin header, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, PUT, DELETE, OPTIONS" {
		t.Errorf("expected Access-Control-Allow-Methods header, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
		t.Errorf("expected Access-Control-Allow-Headers header, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "3600" {
		t.Errorf("expected Access-Control-Max-Age header, got %q", got)
	}
}

func TestCORSMiddleware_PreflightReturns204WithoutCallingNext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := CORSMiddleware("http://localhost:5173")(next)

	req := httptest.NewRequest(http.MethodOptions, "/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("expected next handler NOT to be called on OPTIONS preflight")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 No Content, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("expected CORS headers on preflight response, got %q", got)
	}
}
