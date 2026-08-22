package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"remote/core"
)

// sessionCookieName is the HttpOnly cookie that carries the API token for
// browser clients, set by AuthHandler.HandleLogin. Unlike the Authorization
// header (which the browser WebSocket API cannot set on the upgrade
// request), this cookie rides along automatically on every request to the
// API's origin — including WS upgrades — without any JS ever reading it.
const sessionCookieName = "trayline_session"

// sessionCookieMaxAge matches this deployment's single static API token,
// which has no separate expiry/rotation of its own — the cookie simply
// mirrors that lifetime.
const sessionCookieMaxAge = 30 * 24 * time.Hour

// setSessionCookie sets the HttpOnly session cookie to token. secure should
// be true in production (HTTPS) and may be false for local http://localhost
// development, per this project's Core Principle on environment-driven
// relaxation (see code-security.md) — never driven by client-controlled input.
func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionCookieMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie deletes the session cookie (used on logout).
func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// providedToken extracts the caller-supplied credential from the
// Authorization header ("Bearer <token>", used by the CLI) if present,
// otherwise from the session cookie (used by the dashboard). ok is false
// only when NEITHER was supplied at all — it says nothing about validity.
func providedToken(r *http.Request) (token string, ok bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer "), true
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return c.Value, true
	}
	return "", false
}

// AuthMiddleware validates the caller's token (header or cookie, see
// providedToken) for all paths except /health, /auth/login and /auth/logout.
// Uses constant-time comparison to prevent timing attacks.
// WebSocket upgrade requests (Upgrade: websocket header) are passed through
// without token validation here — WebSocket handlers authenticate via
// providedToken pre-upgrade (when a header or cookie is present) or the
// first message after the connection is established (browser clients with
// neither, e.g. a cross-origin dashboard deployment without credentials).
func AuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health", "/auth/login", "/auth/logout":
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for WebSocket upgrades — they authenticate post-connect.
		if isWebSocketUpgrade(r) {
			next.ServeHTTP(w, r)
			return
		}

		isOpenAIPath := strings.HasPrefix(r.URL.Path, "/v1/")

		provided, hasCred := providedToken(r)
		if !hasCred {
			if isOpenAIPath {
				writeOpenAIError(w, http.StatusUnauthorized, "invalid_request_error", "missing or invalid Authorization header", nil, nil)
				return
			}
			writeJSON(w, http.StatusUnauthorized, core.ErrorResponse{
				Error:   "UNAUTHORIZED",
				Message: "missing or invalid Authorization header",
			})
			return
		}

		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			if isOpenAIPath {
				code := "invalid_api_key"
				writeOpenAIError(w, http.StatusUnauthorized, "invalid_request_error", "Invalid API key provided", nil, &code)
				return
			}
			writeJSON(w, http.StatusUnauthorized, core.ErrorResponse{
				Error:   "UNAUTHORIZED",
				Message: "invalid token",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// wsTokenInvalid reports whether a pre-upgrade credential (as returned by
// providedToken) is present but wrong. Absence (hasCred == false) is not
// "invalid" here — the caller falls back to the post-upgrade wsAuth
// handshake instead (see providedToken and AuthMiddleware's doc comment).
// Every WS handler MUST call this before upgrading — never treat merely
// having *some* credential as proof it's the right one.
func wsTokenInvalid(provided string, hasCred bool, expected string) bool {
	return hasCred && !ValidateWSToken(provided, expected)
}

// ValidateWSToken checks a token against the expected value using constant-time comparison.
// Used by WebSocket handlers to authenticate the first message.
func ValidateWSToken(provided, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
