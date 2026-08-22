package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLogin_ValidToken_SetsHttpOnlySessionCookie(t *testing.T) {
	h := NewAuthHandler("correct-token", true)

	body, _ := json.Marshal(loginRequest{Token: "correct-token"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := rec.Result()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected exactly one cookie set, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != sessionCookieName {
		t.Errorf("expected cookie name %q, got %q", sessionCookieName, c.Name)
	}
	if c.Value != "correct-token" {
		t.Errorf("expected cookie value %q, got %q", "correct-token", c.Value)
	}
	if !c.HttpOnly {
		t.Error("expected HttpOnly=true — this is the whole point of the fix, JS must never be able to read this cookie")
	}
	if !c.Secure {
		t.Error("expected Secure=true (cookieSecure=true was passed to NewAuthHandler)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", c.SameSite)
	}
	if c.MaxAge <= 0 {
		t.Errorf("expected a positive MaxAge, got %d", c.MaxAge)
	}
}

func TestHandleLogin_WrongToken_Rejected(t *testing.T) {
	h := NewAuthHandler("correct-token", true)

	body, _ := json.Marshal(loginRequest{Token: "wrong-token"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("expected no cookie set for a wrong token")
	}
}

func TestHandleLogin_EmptyToken_Rejected(t *testing.T) {
	h := NewAuthHandler("correct-token", true)

	body, _ := json.Marshal(loginRequest{Token: ""})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty token, got %d", rec.Code)
	}
}

func TestHandleLogin_MalformedBody_Rejected(t *testing.T) {
	h := NewAuthHandler("correct-token", true)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()

	h.HandleLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestHandleLogin_CookieSecureFalseInDevelopment(t *testing.T) {
	h := NewAuthHandler("correct-token", false)

	body, _ := json.Marshal(loginRequest{Token: "correct-token"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleLogin(rec, req)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	if cookies[0].Secure {
		t.Error("expected Secure=false when cookieSecure=false was configured")
	}
}

func TestHandleLogout_ClearsSessionCookie(t *testing.T) {
	h := NewAuthHandler("correct-token", true)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rec := httptest.NewRecorder()

	h.HandleLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie in the clearing response, got %d", len(cookies))
	}
	if cookies[0].MaxAge >= 0 {
		t.Errorf("expected a negative MaxAge to delete the cookie, got %d", cookies[0].MaxAge)
	}
}

func TestHandleSession_ReachableMeansAuthenticated(t *testing.T) {
	h := NewAuthHandler("correct-token", true)
	req := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	rec := httptest.NewRecorder()

	h.HandleSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// Integration-style test: AuthMiddleware must accept the session cookie
// AuthHandler.HandleLogin sets, as an alternative to the Authorization
// header — this is the actual mechanism that lets the dashboard drop the
// token from localStorage while every existing REST call keeps working.
func TestAuthMiddleware_AcceptsSessionCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware("correct-token", next)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "correct-token"})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with a valid session cookie, got %d", rec.Code)
	}
}

func TestAuthMiddleware_RejectsWrongSessionCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware("correct-token", next)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "wrong-token"})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with an invalid session cookie, got %d", rec.Code)
	}
}

// The Authorization header must still work unchanged (the CLI never adopts
// cookies), and take precedence if somehow both are present.
func TestAuthMiddleware_HeaderStillWorksAndTakesPrecedenceOverCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware("correct-token", next)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.Header.Set("Authorization", "Bearer correct-token")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "garbage-that-would-fail-if-used"})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 using the valid header even with a bogus cookie present, got %d", rec.Code)
	}
}

func TestAuthMiddleware_LoginAndLogoutExemptFromAuth(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware("correct-token", next)

	for _, path := range []string{"/auth/login", "/auth/logout"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected to bypass auth (200), got %d", path, rec.Code)
		}
	}
}
