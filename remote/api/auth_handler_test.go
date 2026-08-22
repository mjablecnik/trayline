package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLogin_ValidToken_SetsHttpOnlySessionCookieAndReturnsCSRFToken(t *testing.T) {
	h := NewAuthHandler("correct-token", true, NewCSRFStore())

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
	if c.SameSite != http.SameSiteNoneMode {
		t.Errorf("expected SameSite=None (required for the dashboard's cross-origin deployment when Secure=true), got %v", c.SameSite)
	}
	if c.MaxAge <= 0 {
		t.Errorf("expected a positive MaxAge, got %d", c.MaxAge)
	}

	var body2 sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body2); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body2.CSRFToken == "" {
		t.Error("expected a non-empty csrfToken in the login response body")
	}
}

func TestHandleLogin_WrongToken_Rejected(t *testing.T) {
	h := NewAuthHandler("correct-token", true, NewCSRFStore())

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
	h := NewAuthHandler("correct-token", true, NewCSRFStore())

	body, _ := json.Marshal(loginRequest{Token: ""})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty token, got %d", rec.Code)
	}
}

func TestHandleLogin_MalformedBody_Rejected(t *testing.T) {
	h := NewAuthHandler("correct-token", true, NewCSRFStore())

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()

	h.HandleLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestHandleLogin_CookieSecureFalseInDevelopment(t *testing.T) {
	h := NewAuthHandler("correct-token", false, NewCSRFStore())

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
	// SameSite=None without Secure is rejected outright by browsers, so the
	// development fallback must stay Lax.
	if cookies[0].SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax when Secure=false, got %v", cookies[0].SameSite)
	}
}

func TestHandleLogout_ClearsSessionCookie(t *testing.T) {
	h := NewAuthHandler("correct-token", true, NewCSRFStore())

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

func TestHandleSession_ReachableMeansAuthenticatedAndIssuesFreshCSRFToken(t *testing.T) {
	h := NewAuthHandler("correct-token", true, NewCSRFStore())
	req := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	rec := httptest.NewRecorder()

	h.HandleSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.CSRFToken == "" {
		t.Error("expected a non-empty csrfToken so a reloaded page can resume making mutating requests")
	}
}

// Integration-style test: AuthMiddleware must accept the session cookie
// AuthHandler.HandleLogin sets, as an alternative to the Authorization
// header — this is the actual mechanism that lets the dashboard drop the
// token from localStorage while every existing REST call keeps working.
// GET is a safe method, so no CSRF token is required here.
func TestAuthMiddleware_AcceptsSessionCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware("correct-token", NewCSRFStore(), next)

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
	h := AuthMiddleware("correct-token", NewCSRFStore(), next)

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
	h := AuthMiddleware("correct-token", NewCSRFStore(), next)

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
	h := AuthMiddleware("correct-token", NewCSRFStore(), next)

	for _, path := range []string{"/auth/login", "/auth/logout"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected to bypass auth (200), got %d", path, rec.Code)
		}
	}
}

// --- CSRF protection for cookie-authenticated mutating requests ---
//
// These are the core regression tests for the SameSite=None migration: the
// session cookie alone is no longer sufficient proof of intent for a
// mutating request once it rides along on cross-site requests too.

func TestAuthMiddleware_CookieAuthMutatingRequest_RequiresCSRFToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	csrf := NewCSRFStore()
	h := AuthMiddleware("correct-token", csrf, next)

	req := httptest.NewRequest(http.MethodPost, "/projects/x/workflows", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "correct-token"})
	// No X-CSRF-Token header attached.
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a cookie-authenticated POST with no CSRF token, got %d", rec.Code)
	}
}

func TestAuthMiddleware_CookieAuthMutatingRequest_RejectsWrongCSRFToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	csrf := NewCSRFStore()
	h := AuthMiddleware("correct-token", csrf, next)

	req := httptest.NewRequest(http.MethodPost, "/projects/x/workflows", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "correct-token"})
	req.Header.Set(csrfHeaderName, "some-token-we-never-issued")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a forged CSRF token, got %d", rec.Code)
	}
}

func TestAuthMiddleware_CookieAuthMutatingRequest_AcceptsValidCSRFToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	csrf := NewCSRFStore()
	h := AuthMiddleware("correct-token", csrf, next)

	token, err := csrf.issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/projects/x/workflows", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "correct-token"})
	req.Header.Set(csrfHeaderName, token)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with a valid CSRF token, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Header-authenticated (CLI) mutating requests must NOT need a CSRF token —
// a browser can never be tricked into attaching a custom Authorization
// header, so there is no ambient-credential attack to defend against, and
// requiring one would break the CLI for no security benefit.
func TestAuthMiddleware_HeaderAuthMutatingRequest_NoCSRFTokenNeeded(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware("correct-token", NewCSRFStore(), next)

	req := httptest.NewRequest(http.MethodPost, "/projects/x/workflows", nil)
	req.Header.Set("Authorization", "Bearer correct-token")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a header-authenticated POST with no CSRF token, got %d", rec.Code)
	}
}

// The CSRF token issued at login must actually be the one AuthMiddleware
// accepts later — an end-to-end wiring check, not just unit-level csrfStore
// behavior.
func TestLoginThenMutatingRequest_CSRFTokenFromLoginResponseWorks(t *testing.T) {
	csrf := NewCSRFStore()
	authH := NewAuthHandler("correct-token", true, csrf)

	body, _ := json.Marshal(loginRequest{Token: "correct-token"})
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	loginRec := httptest.NewRecorder()
	authH.HandleLogin(loginRec, loginReq)

	var loginResp sessionResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := AuthMiddleware("correct-token", csrf, next)

	req := httptest.NewRequest(http.MethodPost, "/projects/x/workflows", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "correct-token"})
	req.Header.Set(csrfHeaderName, loginResp.CSRFToken)
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 using the CSRF token from the login response, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- WebSocket origin validation (Cross-Site WebSocket Hijacking defense) ---

func TestCheckWSOrigin_AllowsMatchingOrigin(t *testing.T) {
	SetAllowedWSOrigin("https://dashboard.example.com")
	t.Cleanup(func() { SetAllowedWSOrigin("") })

	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	req.Header.Set("Origin", "https://dashboard.example.com")

	if !checkWSOrigin(req) {
		t.Error("expected the configured dashboard origin to be allowed")
	}
}

func TestCheckWSOrigin_RejectsMismatchedOrigin(t *testing.T) {
	SetAllowedWSOrigin("https://dashboard.example.com")
	t.Cleanup(func() { SetAllowedWSOrigin("") })

	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	req.Header.Set("Origin", "https://evil.example.com")

	if checkWSOrigin(req) {
		t.Error("expected a mismatched Origin to be rejected — this is the actual CSWSH defense once cookies are SameSite=None")
	}
}

func TestCheckWSOrigin_AllowsAbsentOriginForNonBrowserClients(t *testing.T) {
	SetAllowedWSOrigin("https://dashboard.example.com")
	t.Cleanup(func() { SetAllowedWSOrigin("") })

	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	// No Origin header set — e.g. the CLI's gorilla/websocket dialer.

	if !checkWSOrigin(req) {
		t.Error("expected an absent Origin header to be allowed (non-browser client, authenticates via Authorization header instead)")
	}
}

func TestCheckWSOrigin_AllowsAnyOriginWhenUnconfigured(t *testing.T) {
	SetAllowedWSOrigin("")

	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	req.Header.Set("Origin", "https://anything.example.com")

	if !checkWSOrigin(req) {
		t.Error("expected Origin to be unenforced when DashboardOrigin/allowedWSOrigin is unset, matching CORSMiddleware's empty-disables convention")
	}
}
