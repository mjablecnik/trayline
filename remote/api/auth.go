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

// sessionCookieSameSite picks the cookie's SameSite mode. None is required
// for the dashboard's cross-origin deployment (dashboard and the remote API
// run on different origins in production) — browsers refuse to store a
// SameSite=None cookie without Secure, so this only applies when secure is
// true; local (non-HTTPS) development falls back to Lax, which is enough
// for same-origin/co-located dev setups.
func sessionCookieSameSite(secure bool) http.SameSite {
	if secure {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

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
		SameSite: sessionCookieSameSite(secure),
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
		SameSite: sessionCookieSameSite(secure),
	})
}

// csrfHeaderName is the header the dashboard must attach the CSRF token
// (issued at login, see AuthHandler) on every cookie-authenticated
// state-changing request.
const csrfHeaderName = "X-CSRF-Token"

// providedToken extracts the caller-supplied credential from the
// Authorization header ("Bearer <token>", used by the CLI) if present,
// otherwise from the session cookie (used by the dashboard). hasCred is
// false only when NEITHER was supplied at all — it says nothing about
// validity. viaCookie tells the caller whether CSRF protection applies:
// only cookie-sourced credentials ride along automatically with a browser
// request, so only they need it (see AuthMiddleware).
func providedToken(r *http.Request) (token string, hasCred bool, viaCookie bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer "), true, false
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return c.Value, true, true
	}
	return "", false, false
}

// isMutatingMethod reports whether m can change server state — the set of
// methods CSRF protection actually needs to cover (GET/HEAD/OPTIONS must
// never change state in this API, so they're exempt, same as the CSRF
// standard's baseline).
func isMutatingMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// AuthMiddleware validates the caller's token (header or cookie, see
// providedToken) for all paths except /health, /auth/login and /auth/logout.
// Uses constant-time comparison to prevent timing attacks.
//
// Cookie-authenticated state-changing requests additionally require a valid
// X-CSRF-Token header matching a token csrfStore issued at login. This is
// required because the session cookie is SameSite=None in production (see
// sessionCookieSameSite) so it rides along on cross-site requests too —
// unlike SameSite=Lax, which alone would block that for anything but a
// top-level navigation. Header-authenticated callers (the CLI) never need
// this: a browser can't be tricked into attaching a custom Authorization
// header, so there's no ambient-credential attack to defend against there.
//
// WebSocket upgrade requests (Upgrade: websocket header) are passed through
// without token validation here — WebSocket handlers authenticate via
// providedToken pre-upgrade (when a header or cookie is present) or the
// first message after the connection is established (browser clients with
// neither, e.g. a cross-origin dashboard deployment without credentials).
// WS upgrades are always GET, so CSRF (which only guards mutating methods)
// doesn't apply to them — they're protected instead by validating the
// Origin header at the upgrade (see CheckOrigin in session_handler.go),
// which defends against cross-site WebSocket hijacking.
func AuthMiddleware(token string, csrf *csrfStore, next http.Handler) http.Handler {
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

		provided, hasCred, viaCookie := providedToken(r)
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

		if viaCookie && isMutatingMethod(r.Method) && !csrf.valid(r.Header.Get(csrfHeaderName)) {
			writeJSON(w, http.StatusForbidden, core.ErrorResponse{
				Error:   "CSRF_TOKEN_INVALID",
				Message: "missing or invalid " + csrfHeaderName + " header",
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

// allowedWSOrigin is the dashboard origin WebSocket upgrades are allowed
// from, set once at startup via SetAllowedWSOrigin (see cmd/server/main.go).
// A package-level var is acceptable here: this process serves exactly one
// configured DashboardOrigin for its whole lifetime, same as core.Config.
var allowedWSOrigin string

// SetAllowedWSOrigin configures the origin checkWSOrigin enforces. Call once
// at startup with core.Config.DashboardOrigin.
func SetAllowedWSOrigin(origin string) {
	allowedWSOrigin = origin
}

// checkWSOrigin is the websocket.Upgrader.CheckOrigin implementation used by
// every WS handler in this package (see the shared `upgrader` in
// session_handler.go). Without this, moving the session cookie to
// SameSite=None (required for the dashboard's cross-origin deployment, see
// sessionCookieSameSite) would let ANY origin the victim's browser is
// pointed at open a fully-authenticated WS connection using the ambient
// session cookie — Cross-Site WebSocket Hijacking. CSRF tokens don't cover
// this: the WS handshake is always a GET, and CSRF only guards mutating
// methods (see AuthMiddleware) — Origin validation at the handshake is the
// actual defense here.
//
// An empty Origin header (no browser involved — e.g. the CLI's gorilla/
// websocket dialer, which doesn't set one by default) is allowed through;
// those callers authenticate via the Authorization header instead, which a
// browser can never be tricked into attaching, so they carry no CSWSH risk.
// If allowedWSOrigin itself is unset (DashboardOrigin not configured, e.g.
// local dev without CORS), Origin is not enforced — same convention as
// CORSMiddleware's "empty disables" behavior.
func checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || allowedWSOrigin == "" {
		return true
	}
	return origin == allowedWSOrigin
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
